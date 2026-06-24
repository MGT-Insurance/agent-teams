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

// RegisterQueryKong registers all query verbs onto p as native kong structs.
func RegisterQueryKong(p *cli.Parser) {
	p.AddVerb("ws", "Print the workspace home path.", &wsKong{})
	p.AddVerb("list", "List open initiatives.", &listKong{})
	p.AddVerb("list-json", "List open initiatives as JSON.", &listJSONKong{})
	p.AddVerb("human-list", "List gated beads awaiting human input.", &humanListKong{})
	p.AddVerb("show", "Show details for an initiative.", &showKong{})
	p.AddVerb("learnings", "Print role memories (hot+fresh, or all).", &learningsKong{})
	p.AddVerb("recall", "Search role memories by substring query.", &recallKong{})
	p.AddVerb("prime", "Print cross-project user preferences.", &primeKong{})
	p.AddVerb("roles", "List role namespaces present in workspace memories.", &rolesKong{})
}

// ── kong structs (native form) ────────────────────────────────────────────────

// wsKong provides help-listing presence for the ws verb. main.go intercepts
// "ws" before kong dispatch and prints the home path directly; this Run is a
// safety fallback only.
type wsKong struct{}

func (c *wsKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam ws: no context")
	}
	fmt.Fprintln(ctx.Stdout, ctx.Home)
	return nil
}

// listKong passes through: bd list --status=open
type listKong struct{}

func (c *listKong) Run(ctx *cli.Context) error {
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

// listJSONKong passes through: bd list --status=open --json
type listJSONKong struct{}

func (c *listJSONKong) Run(ctx *cli.Context) error {
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

// humanListKong renders gated beads with their gate kind and note.
type humanListKong struct{}

func (c *humanListKong) Run(ctx *cli.Context) error {
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
			if ask, ok := extractLatestAsk(issue.Notes); ok {
				fmt.Fprint(ctx.Stdout, renderAsk(ask))
			} else {
				fmt.Fprintf(ctx.Stdout, "    %s\n", lastNoteBlock(issue.Notes))
			}
		}
	}
	return nil
}

// showKong passes through: bd show <id>
type showKong struct {
	ID string `arg:"" name:"id" help:"Initiative ID to show."`
}

func (c *showKong) Validate() error {
	if c.ID == "" {
		return cli.Usagef("ateam show: id must not be empty")
	}
	return nil
}

func (c *showKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam show: no context")
	}
	out, err := ctx.BD.Run("show", c.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, out)
	return nil
}

// learningsKong prints full bodies of memories for a role.
type learningsKong struct {
	Role string `arg:"" name:"role" help:"Role namespace to fetch memories for." optional:""`
}

func (c *learningsKong) Validate() error {
	if c.Role == "" {
		return cli.Usagef("ateam learnings: <role> is required")
	}
	return nil
}

func (c *learningsKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam learnings: no context")
	}
	return runLearnings(ctx, c.Role)
}

// recallKong performs a substring search over a role's memories.
type recallKong struct {
	Role  string `arg:"" name:"role"  optional:"" help:"Role namespace to search."`
	Query string `arg:"" name:"query" optional:"" help:"Substring to search for."`
}

func (c *recallKong) Validate() error {
	if c.Role == "" {
		return cli.Usagef("ateam recall: <role> is required")
	}
	if c.Query == "" {
		return cli.Usagef("ateam recall: <query> is required")
	}
	return nil
}

func (c *recallKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam recall: no context")
	}
	return runRecall(ctx, c.Role, c.Query)
}

// primeKong prints cross-project user preferences from bd memories.
type primeKong struct{}

func (c *primeKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam prime: no context")
	}
	return runPrime(ctx)
}

// rolesKong lists the distinct role namespaces present in workspace memories.
type rolesKong struct{}

func (c *rolesKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam roles: no context")
	}
	return runRoles(ctx)
}


// lastNoteBlockLines is the maximum number of lines rendered from the fallback
// note block before a truncation indicator is prepended.
const lastNoteBlockLines = 10

// lastNoteBlock returns the last non-empty blank-line-separated block from
// notes, capped to lastNoteBlockLines lines. When the block exceeds the cap,
// a single indicator line is prepended. Leading/trailing whitespace is trimmed
// from the returned block.
func lastNoteBlock(notes string) string {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return ""
	}

	// Split on one or more blank lines (a newline followed by optional
	// whitespace then another newline).
	blocks := splitOnBlankLines(notes)

	// Find the last non-empty block.
	last := ""
	for i := len(blocks) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(blocks[i])
		if trimmed != "" {
			last = trimmed
			break
		}
	}
	if last == "" {
		return notes
	}

	lines := strings.Split(last, "\n")
	if len(lines) <= lastNoteBlockLines {
		return last
	}
	tail := strings.Join(lines[len(lines)-lastNoteBlockLines:], "\n")
	return "(…older lines truncated — see bd show <id>)\n" + tail
}

// splitOnBlankLines splits s into blocks separated by one or more blank lines.
// A blank line is a line that contains only whitespace (including an empty line).
func splitOnBlankLines(s string) []string {
	var blocks []string
	var current strings.Builder
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if current.Len() > 0 {
				blocks = append(blocks, current.String())
				current.Reset()
			}
		} else {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(line)
		}
	}
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}
	return blocks
}

// askBlock holds the parsed fields of a structured ateam-ask sentinel block
// (CONTRACT agent-teams-j9s §2).
type askBlock struct {
	decision       string
	recommendation string
	alternative    string
	context        string
}

// extractLatestAsk scans notes for the LAST sentinel-delimited ateam-ask block
// and parses it. Returns the parsed block and true when found; false otherwise.
// Malformed or incomplete blocks (missing closing sentinel) are skipped.
//
// The closing sentinel ">>>" must appear at the start of a line to avoid
// matching ">>>" embedded in prose or git conflict markers.
func extractLatestAsk(notes string) (askBlock, bool) {
	const open = "<<<ateam-ask"

	// closeMarker matches ">>>" anchored to the start of a line.
	// The writer (buildAskBlock) always emits ">>>" on its own line, so
	// requiring a leading "\n" is a safe tighter match that round-trips correctly.
	closeLine := func(s string) int {
		// Check for ">>>" at the very start of the string (first block, no
		// preceding newline) or after a newline.
		if strings.HasPrefix(s, ">>>") {
			return 0
		}
		idx := strings.Index(s, "\n>>>")
		if idx == -1 {
			return -1
		}
		return idx + 1 // position of the ">" that starts ">>>"
	}

	var last askBlock
	found := false
	remaining := notes
	for {
		start := strings.Index(remaining, open)
		if start == -1 {
			break
		}
		after := remaining[start+len(open):]
		end := closeLine(after)
		if end == -1 {
			// Unclosed block — skip and keep scanning for later valid blocks.
			// Advance past the open sentinel so we don't loop on the same position.
			remaining = after
			continue
		}
		body := after[:end]
		if parsed, ok := parseAskBody(body); ok {
			last = parsed
			found = true
		}
		remaining = after[end+len(">>>"):]
	}
	return last, found
}

// parseAskBody parses the interior of an ateam-ask block. Returns false when
// the required decision field is absent or empty.
func parseAskBody(body string) (askBlock, bool) {
	var b askBlock
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "decision:"); ok {
			b.decision = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "recommendation:"); ok {
			b.recommendation = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "alternative:"); ok {
			b.alternative = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "context:"); ok {
			b.context = strings.TrimSpace(after)
		}
	}
	if b.decision == "" {
		return askBlock{}, false
	}
	return b, true
}

// renderAsk formats a parsed askBlock for human-list output. Each field is
// indented with four spaces; context is omitted when empty.
func renderAsk(b askBlock) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "    decision: %s\n", b.decision)
	fmt.Fprintf(&sb, "    recommendation: %s\n", b.recommendation)
	fmt.Fprintf(&sb, "    alternative: %s\n", b.alternative)
	if b.context != "" {
		fmt.Fprintf(&sb, "    context: %s\n", b.context)
	}
	return sb.String()
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

// runLearnings prints full bodies of memories for role. Serves the union of
// HOT keys (prefix `role+":hot:"`) and FRESH keys (prefix `role+":fresh:"`).
// Falls back to ALL `role:` keys when both sets are empty.
func runLearnings(ctx *cli.Context, role string) error {
	hotPrefix := role + ":hot:"
	freshPrefix := role + ":fresh:"
	rolePrefix := role + ":"

	// Use map[string]any to tolerate non-string values (e.g. schema_version: 1).
	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	// Collect hot, fresh, and all-role keys in one pass.
	var hotKeys []string
	var freshKeys []string
	var allRoleKeys []string
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		if strings.HasPrefix(k, hotPrefix) {
			hotKeys = append(hotKeys, k)
		}
		if strings.HasPrefix(k, freshPrefix) {
			freshKeys = append(freshKeys, k)
		}
		if strings.HasPrefix(k, rolePrefix) {
			allRoleKeys = append(allRoleKeys, k)
		}
	}

	// Served set = union(hotKeys, freshKeys). Fall back to allRoleKeys when both
	// are empty, preserving zero-tier backward-compat behavior.
	var keys []string
	if len(hotKeys) > 0 || len(freshKeys) > 0 {
		seen := make(map[string]struct{}, len(hotKeys)+len(freshKeys))
		for _, k := range hotKeys {
			if _, dup := seen[k]; !dup {
				keys = append(keys, k)
				seen[k] = struct{}{}
			}
		}
		for _, k := range freshKeys {
			if _, dup := seen[k]; !dup {
				keys = append(keys, k)
				seen[k] = struct{}{}
			}
		}
	} else {
		keys = allRoleKeys
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

// runRecall performs a substring search over a role's memories (both hot and
// cold), printing key + body for each match.
func runRecall(ctx *cli.Context, role, query string) error {
	query = strings.ToLower(query)
	rolePrefix := role + ":"

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	var keys []string
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		if !strings.HasPrefix(k, rolePrefix) {
			continue
		}
		body := v.(string)
		if strings.Contains(strings.ToLower(k), query) || strings.Contains(strings.ToLower(body), query) {
			keys = append(keys, k)
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

// runPrime prints cross-project user preferences from bd memories.
// Filters to keys with the "user:" prefix, caps at 12, and truncates each body to ~300 chars.
func runPrime(ctx *cli.Context) error {
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

// runRoles lists the distinct role namespaces present in workspace memories.
func runRoles(ctx *cli.Context) error {
	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
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
		seen[role] = struct{}{}
	}

	roles := make([]string, 0, len(seen))
	for r := range seen {
		roles = append(roles, r)
	}
	sort.Strings(roles)

	for _, r := range roles {
		fmt.Fprintln(ctx.Stdout, r)
	}
	return nil
}
