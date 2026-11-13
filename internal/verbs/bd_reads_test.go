package verbs

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// handlerRunner builds a bd client whose every call is answered by fn, and
// returns the argv (minus the leading -C <home>) of each call in order.
func handlerRunner(t *testing.T, fn func(args []string) (string, error)) (*bd.Client, *[][]string) {
	t.Helper()
	var calls [][]string
	c := bd.NewClientWithExec(t.TempDir(), func(_ string, args ...string) ([]byte, []byte, error) {
		if len(args) >= 2 && args[0] == "-C" {
			args = args[2:]
		}
		calls = append(calls, args)
		out, err := fn(args)
		if err != nil {
			return nil, []byte(err.Error()), err
		}
		return []byte(out), nil, nil
	})
	return c, &calls
}

func isWholeStoreRead(args []string) bool {
	return reflect.DeepEqual(args, []string{"--readonly", "memories", "--json"}) ||
		reflect.DeepEqual(args, []string{"memories", "--json"})
}

func isSQLCall(args []string) bool {
	return len(args) == 4 && args[0] == "--readonly" && args[1] == "sql" && args[2] == "--json"
}

const wholeStoreJSON = `{"dri:hot:a":"A","user:x":"X","schema_version":1}`

func TestReadMemoriesByPrefix_ArgvAndRows(t *testing.T) {
	c, calls := handlerRunner(t, func(args []string) (string, error) {
		return `[{"k":"dri:hot:a","v":"A"},{"k":"dri:fresh:b","v":"B"}]`, nil
	})
	got, err := readMemoriesByPrefix(c, "dri:hot:", "dri:fresh:")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"dri:hot:a": "A", "dri:fresh:b": "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v, want exactly one", *calls)
	}
	wantQ := "SELECT SUBSTRING(`key`, 11) AS k, `value` AS v FROM config WHERE `key` LIKE 'kv.memory.dri:hot:%' OR `key` LIKE 'kv.memory.dri:fresh:%'"
	if !reflect.DeepEqual((*calls)[0], []string{"--readonly", "sql", "--json", wantQ}) {
		t.Fatalf("argv = %q", (*calls)[0])
	}
}

func TestReadMemoriesByPrefix_SinglePrefixQuery(t *testing.T) {
	c, calls := handlerRunner(t, func([]string) (string, error) { return `[{"k":"user:a","v":"A"}]`, nil })
	if _, err := readMemoriesByPrefix(c, "user:"); err != nil {
		t.Fatal(err)
	}
	q := (*calls)[0][3]
	if !strings.HasSuffix(q, "WHERE `key` LIKE 'kv.memory.user:%'") || strings.Contains(q, " OR ") {
		t.Fatalf("query = %q", q)
	}
}

func TestReadMemoriesByPrefix_LikeEscaping(t *testing.T) {
	c, calls := handlerRunner(t, func([]string) (string, error) { return `[{"k":"a_b:x","v":"1"}]`, nil })
	if _, err := readMemoriesByPrefix(c, "a_b:"); err != nil {
		t.Fatal(err)
	}
	if q := (*calls)[0][3]; !strings.Contains(q, `'kv.memory.a\_b:%'`) {
		t.Fatalf("underscore not escaped: %q", q)
	}
	if got := likeEscaper.Replace(`a\b%c_d`); got != `a\\b\%c\_d` {
		t.Fatalf("likeEscaper = %q", got)
	}
}

func TestReadMemoriesByPrefix_DisallowedPrefixFallsBack(t *testing.T) {
	for _, bad := range []string{"it's:", "a b:", "x`y", "a%", ""} {
		c, calls := handlerRunner(t, func(args []string) (string, error) {
			if isWholeStoreRead(args) {
				return wholeStoreJSON, nil
			}
			t.Errorf("unexpected call %v", args)
			return "", nil
		})
		got, err := readMemoriesByPrefix(c, "dri:", bad)
		if err != nil || len(got) != 3 {
			t.Fatalf("prefix %q: got %v err %v", bad, got, err)
		}
		if len(*calls) != 1 || !isWholeStoreRead((*calls)[0]) {
			t.Fatalf("prefix %q: calls = %v", bad, *calls)
		}
	}
}

func TestReadMemoriesByPrefix_NoPrefixesFallsBack(t *testing.T) {
	c, calls := handlerRunner(t, func([]string) (string, error) { return wholeStoreJSON, nil })
	got, err := readMemoriesByPrefix(c)
	if err != nil || len(got) != 3 || len(*calls) != 1 || !isWholeStoreRead((*calls)[0]) {
		t.Fatalf("got %v err %v calls %v", got, err, *calls)
	}
}

func TestReadMemoriesByPrefix_NonUnreachableErrorFallsBack(t *testing.T) {
	for _, msg := range []string{"Error: 'bd sql' is not yet supported in embedded mode", "unknown command \"sql\""} {
		c, calls := handlerRunner(t, func(args []string) (string, error) {
			if isSQLCall(args) {
				return "", errors.New(msg)
			}
			return wholeStoreJSON, nil
		})
		got, err := readMemoriesByPrefix(c, "dri:")
		if err != nil || len(got) != 3 {
			t.Fatalf("%q: got %v err %v", msg, got, err)
		}
		if len(*calls) != 2 || !isWholeStoreRead((*calls)[1]) {
			t.Fatalf("%q: calls = %v", msg, *calls)
		}
	}
}

func TestReadMemoriesByPrefix_UnreachableReturnedAfterOneCall(t *testing.T) {
	c, calls := handlerRunner(t, func([]string) (string, error) {
		return "", errors.New("Dolt server unreachable")
	})
	_, err := readMemoriesByPrefix(c, "dri:")
	if err == nil || !isStoreUnreachable(err) {
		t.Fatalf("err = %v, want unreachable", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v, want exactly one", *calls)
	}
}

func TestReadMemoriesByPrefix_ZeroRowsDriftGuard(t *testing.T) {
	t.Run("count positive returns empty", func(t *testing.T) {
		c, calls := handlerRunner(t, func(args []string) (string, error) {
			if isSQLCall(args) && strings.Contains(args[3], "COUNT(*)") {
				return `[{"n":42}]`, nil
			}
			return `[]`, nil
		})
		got, err := readMemoriesByPrefix(c, "nobody:")
		if err != nil || len(got) != 0 || got == nil {
			t.Fatalf("got %v err %v", got, err)
		}
		if len(*calls) != 2 || (*calls)[1][3] != memoryRowCountQuery {
			t.Fatalf("calls = %v", *calls)
		}
	})
	t.Run("count zero falls back", func(t *testing.T) {
		c, calls := handlerRunner(t, func(args []string) (string, error) {
			if isWholeStoreRead(args) {
				return wholeStoreJSON, nil
			}
			if strings.Contains(args[3], "COUNT(*)") {
				return `[{"n":0}]`, nil
			}
			return `[]`, nil
		})
		got, err := readMemoriesByPrefix(c, "dri:")
		if err != nil || len(got) != 3 || len(*calls) != 3 || !isWholeStoreRead((*calls)[2]) {
			t.Fatalf("got %v err %v calls %v", got, err, *calls)
		}
	})
}
