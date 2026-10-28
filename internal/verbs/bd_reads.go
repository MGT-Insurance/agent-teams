// bd_reads.go holds the shared bd read helpers: one open-initiative list, the
// pure filters over it, and read-only memories/config reads.
package verbs

import (
	"fmt"

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
func readMemoriesJSON(ctx *cli.Context, dst any) error {
	if err := ctx.BD.RunJSON(dst, "--readonly", "memories", "--json"); err == nil {
		return nil
	}
	return ctx.BD.RunJSON(dst, "memories", "--json")
}

// readConfigValue runs bd --readonly config get <key> with the same one-retry
// fallback.
func readConfigValue(ctx *cli.Context, key string) (string, error) {
	if out, err := ctx.BD.Run("--readonly", "config", "get", key); err == nil {
		return out, nil
	}
	return ctx.BD.Run("config", "get", key)
}
