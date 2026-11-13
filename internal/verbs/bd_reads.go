// bd_reads.go holds the shared bd read helpers: one open-initiative list, the
// pure filters over it, and read-only memories/config reads.
package verbs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

// listOpenIssues runs bd list --status=open [--include-infra] --limit=0 --json.
// --limit=0 keeps a configured list.limit from truncating the result.
func listOpenIssues(ctx *cli.Context, includeInfra bool) ([]bd.Issue, error) {
	args := []string{"list", "--status=open"}
	if includeInfra {
		args = append(args, "--include-infra")
	}
	args = append(args, "--limit=0", "--json")
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, args...); err != nil {
		return nil, fmt.Errorf("list open issues: %w", err)
	}
	return issues, nil
}

// initiativeRows returns the rows a plain bd list --status=open returns from an
// --include-infra list: drops IssueType message, agent, role, and Ephemeral
// rows.
func initiativeRows(issues []bd.Issue) []bd.Issue {
	rows := make([]bd.Issue, 0, len(issues))
	for _, iss := range issues {
		switch iss.IssueType {
		case "message", "agent", "role":
			continue
		}
		if iss.Ephemeral {
			continue
		}
		rows = append(rows, iss)
	}
	return rows
}

// matchInitiativeBySession is the pure core of resolveInitiativeBySession
// (match.go): empty sessionID, zero matches, or more than one runtime-matching
// match all return (bd.Issue{}, false). Callers pass rows already narrowed by
// initiativeRows when the list came from --include-infra.
func matchInitiativeBySession(issues []bd.Issue, runtime sessionruntime.Kind, sessionID string) (bd.Issue, bool) {
	if sessionID == "" {
		return bd.Issue{}, false
	}
	var match *bd.Issue
	for i := range issues {
		iss := &issues[i]
		if !issueHasSession(*iss, sessionID) {
			continue
		}
		issRuntime, err := sessionruntime.ResolveStored(initiative.Of(*iss).Runtime)
		if err != nil || issRuntime != runtime {
			continue
		}
		if match != nil {
			return bd.Issue{}, false // >1 match: never guess
		}
		match = iss
	}
	if match == nil {
		return bd.Issue{}, false
	}
	return *match, true
}

// readMemoriesJSON runs bd --readonly memories --json into dst; if that
// returns an error it retries once as bd memories --json, so the failure path
// is unchanged (covers a store that needs a write-mode open).
func readMemoriesJSON(r cli.BDRunner, dst any) error {
	if err := r.RunJSON(dst, "--readonly", "memories", "--json"); err == nil {
		return nil
	}
	return r.RunJSON(dst, "memories", "--json")
}

// memoryPrefixAllowed is the allowlist for a prefix interpolated into SQL: bd
// sql has no bind parameters, so a prefix outside it never reaches the query.
var memoryPrefixAllowed = regexp.MustCompile(`^[A-Za-z0-9:._-]+$`)

// likeEscaper escapes the LIKE metacharacters in a prefix.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// memoryRowCountQuery counts every stored memory row. The scoped read runs it
// only after a zero-row result, to tell an empty prefix from a bd that moved
// the kv.memory. row layout.
const memoryRowCountQuery = "SELECT COUNT(*) AS n FROM config WHERE `key` LIKE 'kv.memory.%'"

// readMemoriesByPrefix returns the memories whose key starts with any of
// prefixes, in the same map[string]any shape readMemoriesJSON fills. It reads
// the config rows bd stores memories in (key kv.memory.<memory-key>) with one
// bd sql call, so the transfer is the scoped rows, not the whole store.
//
// It returns the whole-store read when prefixes is empty, when any prefix
// fails memoryPrefixAllowed, when the sql call fails for a reason other than
// an unreachable store (embedded mode, a bd without sql, bad JSON), and when
// the scoped query returns no rows while the store holds no kv.memory. rows at
// all. An unreachable-store error is returned unchanged after one call.
func readMemoriesByPrefix(r cli.BDRunner, prefixes ...string) (map[string]any, error) {
	whole := func() (map[string]any, error) {
		var raw map[string]any
		if err := readMemoriesJSON(r, &raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if len(prefixes) == 0 {
		return whole()
	}
	clauses := make([]string, len(prefixes))
	for i, p := range prefixes {
		if !memoryPrefixAllowed.MatchString(p) {
			return whole()
		}
		clauses[i] = "`key` LIKE 'kv.memory." + likeEscaper.Replace(p) + "%'"
	}
	query := "SELECT SUBSTRING(`key`, 11) AS k, `value` AS v FROM config WHERE " + strings.Join(clauses, " OR ")

	var rows []struct {
		K string `json:"k"`
		V string `json:"v"`
	}
	if err := r.RunJSON(&rows, "--readonly", "sql", "--json", query); err != nil {
		if isStoreUnreachable(err) {
			return nil, err
		}
		return whole()
	}
	if len(rows) == 0 {
		var count []struct {
			N int `json:"n"`
		}
		if err := r.RunJSON(&count, "--readonly", "sql", "--json", memoryRowCountQuery); err != nil {
			if isStoreUnreachable(err) {
				return nil, err
			}
			return whole()
		}
		if len(count) == 0 || count[0].N == 0 {
			return whole()
		}
	}
	out := make(map[string]any, len(rows))
	for _, row := range rows {
		out[row.K] = row.V
	}
	return out, nil
}

// readConfigValue runs bd --readonly config get <key> with the same one-retry
// fallback.
func readConfigValue(ctx *cli.Context, key string) (string, error) {
	if out, err := ctx.BD.Run("--readonly", "config", "get", key); err == nil {
		return out, nil
	}
	return ctx.BD.Run("config", "get", key)
}
