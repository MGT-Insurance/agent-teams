// This file is owned by Track A (Go) of at-ig53 (condense-cost work).
//
// condense-check replaces the model-driven Step 2 measurement block in the
// condense skill (six `ateam learnings <role>` calls + `bd memories --json` +
// byte arithmetic, ~132K cache-read tokens per turn) with a single read-only
// tool call. See contract agent-teams-0yd3.1 SEAM 1 (verb shape) and SEAM 2
// (threshold constants, defined alongside condenseBudgetTokens in
// kong_converted.go).
package verbs

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterCondenseCheck registers the condense-check verb onto p using a
// native kong struct.
func RegisterCondenseCheck(p *cli.Parser) {
	p.AddVerb("condense-check", "Compute the condense fire/skip verdict for one role or all roles (read-only).", &condenseCheckKong{})
}

// condenseCheckRoleResult is one role's measurement + verdict. JSON field
// names are frozen by contract agent-teams-0yd3.1 SEAM 1.
type condenseCheckRoleResult struct {
	Role              string `json:"role"`
	LearningsBytes    int    `json:"learnings_bytes"`
	ApproxTokens      int    `json:"approx_tokens"`
	FreshBytes        int    `json:"fresh_bytes"`
	FreshApproxTokens int    `json:"fresh_approx_tokens"`
	HotApproxTokens   int    `json:"hot_approx_tokens"`
	Verdict           string `json:"verdict"` // "FIRE" or "SKIP"
	Reason            string `json:"reason"`
}

// condenseCheckKong is the kong-converted form of condense-check.
// Role is optional: omitted means "all roles" (per SEAM 1), enumerated the
// same way `ateam roles` does today, skipping the user and applied
// namespaces (neither is a learnings role — see roles_test.go /
// query.go:runRoles for the applied exclusion this mirrors).
type condenseCheckKong struct {
	Role string `arg:"" name:"role" optional:"" help:"Role to check (default: all roles, skipping user/applied)."`
	JSON bool   `name:"json" help:"Output machine-readable JSON instead of one aligned line per role."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
// Read-only: the only bd call is `memories --json`. Zero writes — mirrored by
// TestCondenseCheck_ZeroWritesOccur alongside
// write_test.go:TestCondense_ZeroWritesOccur. Exit code is always 0
// regardless of verdict (SEAM 1) — verdict is data, not a process outcome.
func (c *condenseCheckKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense-check: no context")
	}

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	roles := c.roles(raw)
	results := make([]condenseCheckRoleResult, 0, len(roles))
	for _, role := range roles {
		results = append(results, condenseCheckForRole(raw, role))
	}

	if c.JSON {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	return renderCondenseCheckText(ctx.Stdout, results)
}

// roles returns the roles to check: just c.Role when explicitly given,
// otherwise every distinct role namespace in raw except "user" and
// "applied" (neither is a learnings role; see rolesKong/runRoles in
// query.go, which this mirrors but does not call — query.go is owned by a
// different track and this file must stay file-disjoint from it).
func (c *condenseCheckKong) roles(raw map[string]any) []string {
	if c.Role != "" {
		return []string{c.Role}
	}

	seen := make(map[string]struct{})
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		idx := strings.Index(k, ":")
		if idx < 0 {
			continue
		}
		role := k[:idx]
		if role == "applied" || role == "user" {
			continue
		}
		seen[role] = struct{}{}
	}

	roles := make([]string, 0, len(seen))
	for r := range seen {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	return roles
}

// condenseCheckForRole computes the full measurement + verdict for one role
// out of the already-fetched memories map raw.
func condenseCheckForRole(raw map[string]any, role string) condenseCheckRoleResult {
	hotPrefix := role + ":hot:"
	freshPrefix := role + ":fresh:"
	rolePrefix := role + ":"

	var hotKeys, freshKeys, allRoleKeys []string
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		switch {
		case strings.HasPrefix(k, hotPrefix):
			hotKeys = append(hotKeys, k)
		case strings.HasPrefix(k, freshPrefix):
			freshKeys = append(freshKeys, k)
		}
		if strings.HasPrefix(k, rolePrefix) {
			allRoleKeys = append(allRoleKeys, k)
		}
	}

	// servedKeys mirrors what `ateam learnings <role>` prints: union(hot,
	// fresh), falling back to all role: keys when both are empty (pre-tier
	// backward compat — see query.go:runLearnings, which this deliberately
	// re-derives rather than calls, to stay file-disjoint from query.go).
	var servedKeys []string
	if len(hotKeys) > 0 || len(freshKeys) > 0 {
		seen := make(map[string]struct{}, len(hotKeys)+len(freshKeys))
		for _, k := range hotKeys {
			if _, dup := seen[k]; !dup {
				servedKeys = append(servedKeys, k)
				seen[k] = struct{}{}
			}
		}
		for _, k := range freshKeys {
			if _, dup := seen[k]; !dup {
				servedKeys = append(servedKeys, k)
				seen[k] = struct{}{}
			}
		}
	} else {
		servedKeys = allRoleKeys
	}
	sort.Strings(servedKeys)

	// learnings_bytes mirrors the exact byte shape `ateam learnings <role>`
	// prints: "<key>\n<body>\n" per entry, with a blank-line separator
	// between entries (query.go:runLearnings).
	learningsBytes := 0
	for i, k := range servedKeys {
		body, _ := raw[k].(string)
		learningsBytes += len(k) + 1 + len(body) + 1
		if i < len(servedKeys)-1 {
			learningsBytes++ // blank-line separator
		}
	}

	freshBytes := sumBodyBytes(raw, freshKeys)
	hotBytes := sumBodyBytes(raw, hotKeys)

	approxTokens := learningsBytes / condenseApproxTokensDivisor
	freshApproxTokens := freshBytes / condenseApproxTokensDivisor
	hotApproxTokens := hotBytes / condenseApproxTokensDivisor

	verdict := "SKIP"
	reason := fmt.Sprintf("under threshold (fresh-alone %d <= %d)", freshApproxTokens, condenseFreshThresholdTokens)
	if freshApproxTokens > condenseFreshThresholdTokens {
		verdict = "FIRE"
		reason = fmt.Sprintf("fresh-alone %d > %d", freshApproxTokens, condenseFreshThresholdTokens)
	}

	return condenseCheckRoleResult{
		Role:              role,
		LearningsBytes:    learningsBytes,
		ApproxTokens:      approxTokens,
		FreshBytes:        freshBytes,
		FreshApproxTokens: freshApproxTokens,
		HotApproxTokens:   hotApproxTokens,
		Verdict:           verdict,
		Reason:            reason,
	}
}

// sumBodyBytes sums the byte length of the string bodies for keys in raw.
// Non-string or absent values contribute 0.
func sumBodyBytes(raw map[string]any, keys []string) int {
	total := 0
	for _, k := range keys {
		if body, ok := raw[k].(string); ok {
			total += len(body)
		}
	}
	return total
}

// renderCondenseCheckText prints one aligned line per role (SEAM 1's bare,
// non-JSON output shape) via text/tabwriter.
func renderCondenseCheckText(w io.Writer, results []condenseCheckRoleResult) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ROLE\tVERDICT\tFRESH/T\tHOT~tok\tLEARNINGS~tok(bytes)\tREASON")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%d\t%d (%dB)\t%s\n",
			r.Role, r.Verdict,
			r.FreshApproxTokens, condenseFreshThresholdTokens,
			r.HotApproxTokens,
			r.ApproxTokens, r.LearningsBytes,
			r.Reason,
		)
	}
	return tw.Flush()
}
