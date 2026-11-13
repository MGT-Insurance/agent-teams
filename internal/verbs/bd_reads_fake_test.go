package verbs

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Scoped-store fake helpers: answer bd sql the way bd does over the config
// table, so verb tests can serve a memory store through readMemoriesByPrefix.

var sqlLikePrefix = regexp.MustCompile("`key` LIKE 'kv\\.memory\\.(.*?)%'")

// sqlPrefixesOf returns the memory-key prefixes named by a readMemoriesByPrefix
// query, with LIKE escapes undone.
func sqlPrefixesOf(query string) []string {
	var out []string
	for _, m := range sqlLikePrefix.FindAllStringSubmatch(query, -1) {
		out = append(out, strings.ReplaceAll(m[1], `\_`, "_"))
	}
	return out
}

// storeSQLAnswer answers a bd sql query for the memory store (key -> value):
// the scoped rows for a prefix query, or the row count for the drift-guard
// COUNT query. Non-string values have no row-form body, so they get no row.
func storeSQLAnswer(query string, store map[string]any) []byte {
	if strings.Contains(query, "COUNT(*)") {
		n := 0
		for _, v := range store {
			if _, ok := v.(string); ok {
				n++
			}
		}
		b, _ := json.Marshal([]map[string]int{{"n": n}})
		return b
	}
	prefixes := sqlPrefixesOf(query)
	rows := []map[string]string{}
	for k, v := range store {
		s, ok := v.(string)
		if !ok {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				rows = append(rows, map[string]string{"k": k, "v": s})
				break
			}
		}
	}
	b, _ := json.Marshal(rows)
	return b
}

// assertScopedReads fails unless calls holds no whole-store memories read and
// its bd sql calls name exactly want (one prefix list per call, in order).
func assertScopedReads(t *testing.T, calls [][]string, want ...[]string) {
	t.Helper()
	var got [][]string
	for _, c := range calls {
		if isWholeStoreRead(c) {
			t.Errorf("whole-store memories read issued: %v", c)
		}
		if isSQLCall(c) && c[3] != memoryRowCountQuery { // the drift-guard count names no prefixes
			got = append(got, sqlPrefixesOf(c[3]))
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sql prefixes per call = %v, want %v (calls: %v)", got, want, calls)
	}
}
