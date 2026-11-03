package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installFakeBD puts a `bd` shell script first on PATH. The script appends
// "<subcommand> tls=<env> journal=<env>" to a log file, prints id for create,
// and prints journalOut for sql. It returns the log path.
func installFakeBD(t *testing.T, id, journalOut string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
# args: -C <dir> <sub> ...
sub="$3"
echo "$sub tls=$BEADS_DOLT_SERVER_TLS journal=$BD_EVENTS_JOURNAL" >> "` + log + `"
case "$sub" in
  create) echo "` + id + `" ;;
  sql) [ -n "$FAKE_BD_SQL_FAIL" ] && { echo "tls handshake failed" >&2; exit 1; }; printf '%s' "` + journalOut + `" ;;
  *) echo '{}' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BEADS_DOLT_SERVER_TLS", "")
	t.Setenv("BD_EVENTS_JOURNAL", "")
	return log
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDefaultFileRunBead_SetsTLSAndJournalAndVerifies(t *testing.T) {
	log := installFakeBD(t, "dsp-1", "issue_id\ndsp-1\n")
	id, err := defaultFileRunBead("/dsp", "me@example.com", "title", "body")
	if err != nil {
		t.Fatalf("defaultFileRunBead: %v", err)
	}
	if id != "dsp-1" {
		t.Errorf("id = %q, want dsp-1", id)
	}
	got := readLog(t, log)
	for _, want := range []string{"create tls=1 journal=1", "sql tls=1 journal=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("calls %q missing %q", got, want)
		}
	}
}

func TestDefaultFileRunBead_MissingJournalRowIsError(t *testing.T) {
	log := installFakeBD(t, "dsp-2", "")
	_, err := defaultFileRunBead("/dsp", "me@example.com", "title", "body")
	if err == nil {
		t.Fatal("want error when the journal has no create row")
	}
	var pce *postCreateError
	if !errors.As(err, &pce) || pce.Kind != postCreateNoRow || pce.ID != "dsp-2" {
		t.Fatalf("err = %#v, want no-row postCreateError for dsp-2", err)
	}
	if !pce.Closed {
		t.Errorf("orphan run bead not closed: %v", pce.CloseErr)
	}
	if got := readLog(t, log); !strings.Contains(got, "close tls=1 journal=1") {
		t.Errorf("calls %q missing close with the run bead env", got)
	}
}

func TestDefaultFileRunBead_SQLFailureIsCheckFailedAndClosesNothing(t *testing.T) {
	log := installFakeBD(t, "dsp-3", "")
	t.Setenv("FAKE_BD_SQL_FAIL", "1")
	_, err := defaultFileRunBead("/dsp", "me@example.com", "title", "body")
	var pce *postCreateError
	if !errors.As(err, &pce) || pce.Kind != postCreateCheckFailed || pce.ID != "dsp-3" {
		t.Fatalf("err = %#v, want check-failed postCreateError for dsp-3", err)
	}
	if strings.Contains(readLog(t, log), "close ") {
		t.Error("a failed check must not close the run bead")
	}
}

func TestDefaultFileRunBead_RejectsUnsafeID(t *testing.T) {
	log := installFakeBD(t, "x'; drop table t; --", "")
	if _, err := defaultFileRunBead("/dsp", "me@example.com", "title", "body"); err == nil {
		t.Fatal("want error for an id that is not a bd id")
	}
	if strings.Contains(readLog(t, log), "sql ") {
		t.Error("an unsafe id must not reach the sql query")
	}
}

func TestDefaultCloudBD_SetsTLSNotJournal(t *testing.T) {
	log := installFakeBD(t, "x", "")
	if _, err := defaultCloudBD("/hosted").Run("list"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readLog(t, log); !strings.Contains(got, "list tls=1 journal=\n") {
		t.Errorf("calls = %q, want TLS set and journal unset", got)
	}
}
