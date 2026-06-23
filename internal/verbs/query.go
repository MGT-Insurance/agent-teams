// Package verbs contains per-track verb registration functions.
// This file is owned by Track A (read/query verbs).
package verbs

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterQuery registers the read/query verbs:
// ws, list, list-json, human-list, show, learnings, prime.
//
// NOTE: ws is also special-cased in main before workspace initialization is
// checked; it is registered here for completeness and usage listing.
func RegisterQuery(reg cli.Registry) {
	reg.Register(&wsCmd{})
	reg.Register(&listCmd{})
	reg.Register(&listJSONCmd{})
	reg.Register(&humanListCmd{})
	reg.Register(&showCmd{})
	reg.Register(&learningsCmd{})
	reg.Register(&primeCmd{})
}

// wsCmd prints the workspace home path.
type wsCmd struct{}

func (c *wsCmd) Name() string { return "ws" }

func (c *wsCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam ws: no context")
	}
	fmt.Fprintln(ctx.Stdout, ctx.Home)
	return nil
}

// listCmd passes through: bd list --status=open
type listCmd struct{}

func (c *listCmd) Name() string { return "list" }

func (c *listCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam list: no context")
	}
	out, err := ctx.BD.Run("list", "--status=open")
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// listJSONCmd passes through: bd list --status=open --json
type listJSONCmd struct{}

func (c *listJSONCmd) Name() string { return "list-json" }

func (c *listJSONCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam list-json: no context")
	}
	out, err := ctx.BD.Run("list", "--status=open", "--json")
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// humanListCmd renders gated beads with their gate kind and note.
// Calls `bd human list --json`, parses the result, and emits a terse
// scannable display per issue:
//
//	<id>  [REVIEW|QUESTION]  <title>
//	    <notes>  (omitted when empty)
//
// Kind is derived from labels: gate:review => REVIEW; otherwise QUESTION
// (covers gate:question and backward-compat human-only beads).
type humanListCmd struct{}

func (c *humanListCmd) Name() string { return "human-list" }

func (c *humanListCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam human-list: no context")
	}
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "human", "list", "--json"); err != nil {
		return err
	}
	if len(issues) == 0 {
		fmt.Fprintln(ctx.Stdout, "No human-needed beads found.")
		return nil
	}
	for _, issue := range issues {
		kind := gateKind(issue.Labels)
		fmt.Fprintf(ctx.Stdout, "%s  [%s]  %s\n", issue.ID, kind, issue.Title)
		if issue.Notes != "" {
			fmt.Fprintf(ctx.Stdout, "    %s\n", issue.Notes)
		}
	}
	return nil
}

// gateKind derives the gate kind from a bead's labels using the kind-resolution
// rule from contract agent-teams-04c:
//   - contains "gate:review"  => "REVIEW"
//   - else (human present, or gate:question, or backward-compat) => "QUESTION"
func gateKind(labels []string) string {
	for _, l := range labels {
		if l == "gate:review" {
			return "REVIEW"
		}
	}
	return "QUESTION"
}

// showCmd passes through: bd show <id>
type showCmd struct{}

func (c *showCmd) Name() string { return "show" }

func (c *showCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam show: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam show: missing <id>")
	}
	out, err := ctx.BD.Run("show", args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// learningsCmd prints full bodies of memories whose keys are prefixed with
// <role>: (e.g. "implementer:"). It calls `bd memories --json` to get a flat
// {key: body} map with untruncated bodies, then filters to keys with the
// requested role prefix. This avoids both the one-line truncation and the
// cross-role bleed that `bd memories <role>` (substring match over key+body)
// produces.
type learningsCmd struct{}

func (c *learningsCmd) Name() string { return "learnings" }

func (c *learningsCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam learnings: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam learnings: missing <role>")
	}
	role := args[0]
	prefix := role + ":"

	// Use map[string]any to tolerate non-string values (e.g. schema_version: 1).
	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	// Collect keys with the "<role>:" prefix whose values are strings.
	var keys []string
	for k, v := range raw {
		if strings.HasPrefix(k, prefix) {
			if _, ok := v.(string); ok {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}

	sort.Strings(keys)
	for i, k := range keys {
		fmt.Fprintln(ctx.Stdout, k)
		fmt.Fprintln(ctx.Stdout, raw[k].(string))
		if i < len(keys)-1 {
			fmt.Fprintln(ctx.Stdout)
		}
	}
	return nil
}

// primeCmd prints cross-project user preferences from bd memories.
// It filters to keys with the "user:" prefix, caps at 12, and truncates
// each body to ~300 chars. Emits nothing when no user: memories exist.
type primeCmd struct{}

func (c *primeCmd) Name() string { return "prime" }

func (c *primeCmd) Run(ctx *cli.Context, _ []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam prime: no context")
	}
	// Use map[string]any to tolerate non-string values (e.g. schema_version: 1).
	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	// Collect keys with the "user:" prefix whose values are strings.
	var keys []string
	for k, v := range raw {
		if strings.HasPrefix(k, "user:") {
			if _, ok := v.(string); ok {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}

	sort.Strings(keys)
	if len(keys) > 12 {
		keys = keys[:12]
	}

	fmt.Fprintln(ctx.Stdout, "## agent-teams: cross-project user preferences")
	for _, k := range keys {
		slug := strings.TrimPrefix(k, "user:")
		body := formatBody(raw[k].(string))
		fmt.Fprintf(ctx.Stdout, "- **%s**: %s\n", slug, body)
	}
	return nil
}

// formatBody collapses newlines to spaces and truncates to ~300 chars,
// appending an ellipsis when truncated.
func formatBody(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	const limit = 300
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}
