// Package verbs contains per-track verb registration functions.
// This file is owned by Track A (read/query verbs).
package verbs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterQueryKong registers all query verbs onto p as native kong structs.
func RegisterQueryKong(p *cli.Parser) {
	p.AddVerb("ws", "Print the workspace home path.", &wsKong{})
	p.AddVerb("list", "List open initiatives.", &listKong{})
	p.AddVerb("list-json", "List initiatives as JSON, each with its parsed routing fields.", &listJSONKong{})
	p.AddVerb("human-list", "List gated beads awaiting human input.", &humanListKong{})
	p.AddVerb("show", "Show details for an initiative.", &showKong{})
	p.AddVerb("learnings", "Print role memories (hot+fresh, or all).", &learningsKong{})
	p.AddVerb("instructions", "Print a role's machine-local human-authored instructions file, if any.", &instructionsKong{})
	p.AddVerb("recall", "Search role memories by tokenized query, ranked by matched terms.", &recallKong{})
	p.AddVerb("prime", "Print cross-project user preferences.", &primeKong{})
	p.AddVerb("roles", "List role namespaces present in workspace memories.", &rolesKong{})
	p.AddVerb("memories-json", "List all role memories as JSON with tier + applied signal.", &memoriesJsonKong{})
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

// listJSONKong emits bd list --json with each element's routing fields parsed
// out into a "fields" object, so a consumer never has to re-implement the field
// rule against the raw description text.
//
// Status has no kong enum:"" tag: bd owns the set of valid statuses (open,
// in_progress, blocked, deferred, closed, pinned, hooked, all) and grows it,
// and a copy of that list here would reject a status bd accepts. bd rejects an
// invalid one loudly, which surfaces as this verb's error.
type listJSONKong struct {
	Status string `name:"status" default:"open" help:"Bead status to list, passed through to bd (open, closed, all, ...)."`
}

func (c *listJSONKong) Validate() error {
	if c.Status == "" {
		return cli.Usagef("ateam list-json: --status must not be empty")
	}
	return nil
}

func (c *listJSONKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam list-json: no context")
	}
	// Direct Run calls in tests bypass kong's default:"open" tag.
	status := c.Status
	if status == "" {
		status = "open"
	}
	out, err := ctx.BD.Run("list", "--status="+status, "--json")
	if err != nil {
		return err
	}
	enriched, err := withRoutingFields([]byte(out))
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, string(enriched))
	return nil
}

// withRoutingFields returns raw — bd's JSON array of issues — with a "fields"
// object, a "prs" array, a "pr_reviews" array, and a "pr_workstreams" array
// added to every element.
// "fields" holds that issue's routing data as parsed by internal/initiative.
// "prs" is the RESOLVED PR list (initiative.ResolvedPRs — docs/multi-pr-
// contract.md §2a; NOT fields.pr, which stays the raw rail-only projection).
// "pr_reviews" is the Go-computed per-PR gate array (§5), one entry per
// resolved PR. Nothing else about any element changes: each existing key is
// re-emitted as the exact bytes bd produced for its value. The only
// difference is insignificant whitespace — the document is re-indented as a
// whole, so a nested value bd printed on one line comes out across several.
//
// Elements are held as map[string]json.RawMessage rather than decoded into a
// struct. bd's element carries keys this CLI does not model (dependency_count,
// comment_count, ...) and will gain more; a struct decode would drop every
// unmodeled key on re-marshal — the same silent-loss failure mode
// internal/initiative exists to prevent, one layer out. Each element is
// decoded TWICE from its original bytes for the same reason: once losslessly
// for re-emission, once into a bd.Issue for initiative.JSONFields, so the
// component keeps receiving a whole issue and this function never hand-picks
// which issue fields the component is allowed to see.
//
// A payload that is not a JSON array is an error, not something to pass
// through: a caller that asked for enriched JSON must never silently receive
// un-enriched output.
func withRoutingFields(raw []byte) ([]byte, error) {
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return nil, fmt.Errorf("ateam list-json: bd did not return a JSON array: %w", err)
	}
	enriched := make([]map[string]json.RawMessage, 0, len(elements))
	for i, element := range elements {
		var keyed map[string]json.RawMessage
		if err := json.Unmarshal(element, &keyed); err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d is not a JSON object: %w", i, err)
		}
		// bd emitting any of these keys itself would make the assignments
		// below a silent overwrite of real data. Refuse instead of guessing
		// which one wins.
		for _, key := range []string{"fields", "prs", "pr_reviews", "pr_workstreams"} {
			if _, exists := keyed[key]; exists {
				return nil, fmt.Errorf("ateam list-json: element %d already carries a %q key; refusing to overwrite it", i, key)
			}
		}
		var issue bd.Issue
		if err := json.Unmarshal(element, &issue); err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d does not decode as an issue: %w", i, err)
		}
		fields, err := json.Marshal(initiative.JSONFields(issue))
		if err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d: encoding routing fields: %w", i, err)
		}
		keyed["fields"] = fields

		prs := initiative.ResolvedPRs(issue)
		if prs == nil {
			prs = []string{}
		}
		prsJSON, err := json.Marshal(prs)
		if err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d: encoding prs: %w", i, err)
		}
		keyed["prs"] = prsJSON

		reviewsJSON, err := json.Marshal(computePRReviews(issue.Labels, prs))
		if err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d: encoding pr_reviews: %w", i, err)
		}
		keyed["pr_reviews"] = reviewsJSON

		prWorkstreams := initiative.PRWorkstreams(issue)
		if prWorkstreams == nil {
			prWorkstreams = []initiative.PRWorkstream{}
		}
		prWorkstreamsJSON, err := json.Marshal(prWorkstreams)
		if err != nil {
			return nil, fmt.Errorf("ateam list-json: element %d: encoding pr_workstreams: %w", i, err)
		}
		keyed["pr_workstreams"] = prWorkstreamsJSON

		enriched = append(enriched, keyed)
	}
	// Indented to match what bd itself prints — this output is read by humans
	// and agents (`ateam list-json` in the resume-dri skill), not only by the
	// dashboard's JSON.parse.
	return json.MarshalIndent(enriched, "", "  ")
}

// humanListRow is one rendered row: an issue paired with its gate kind and,
// for a per-PR row, the PR it belongs to ("" for the legacy, no-PR shape).
type humanListRow struct {
	issue bd.Issue
	kind  string // "REVIEW" or "QUESTION"
	pr    string // "" when not PR-specific
	// multiPR is true when the issue has 2+ resolved PRs. On those rows the
	// note/ask rendering is scoped to THIS pr's own tagged ask block only
	// (extractAskForPR) — never the initiative's latest block system-wide,
	// which may belong to a DIFFERENT PR (the mis-pairing this field exists
	// to prevent; see the render loop in Run). Single-PR and no-PR-concept
	// rows are unaffected and keep the original rendering.
	multiPR bool
}

// humanListKong renders gated beads with their gate kind and note.
//
// Reshaped per docs/multi-pr-contract.md §6: an initiative with a resolved
// PR list (initiative.ResolvedPRs) prints ONE ROW PER GATED PR, not one row
// per initiative — a bead with two PRs gated independently prints two rows,
// each with its own "pr:" line. A bead with no resolved PR at all (a plain
// gated work item, or an initiative that hasn't recorded a PR yet) keeps the
// original single-row-per-bead rendering, unchanged.
type humanListKong struct{}

func (c *humanListKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam human-list: no context")
	}
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "human", "list", "--json"); err != nil {
		return err
	}

	// The row-selection filter runs BEFORE the empty check, not inside the
	// render loop: every row being handed off is this feature's SUCCESS
	// case, and it must answer "nothing needs you" rather than print
	// nothing at all (agent-teams-p9dm.23 regression).
	var rows []humanListRow
	for _, issue := range issues {
		prs := initiative.ResolvedPRs(issue)
		if len(prs) == 0 {
			// No PR concept at all — legacy single-row rendering. A
			// handed-off initiative (external_review.go §2) still carries
			// human + gate:review by design, so `bd human list` still
			// returns it — but Eric already declared he's done looking, so
			// it is no longer awaiting him. Filter here rather than
			// smearing this condition across hung_scan.go / hung_workproduct.go.
			if hasLabel(issue.Labels, externalReviewLabel) {
				continue
			}
			rows = append(rows, humanListRow{issue: issue, kind: gateKind(issue.Labels)})
			continue
		}
		multi := len(prs) >= 2
		for _, r := range computePRReviews(issue.Labels, prs) {
			switch r.Gate {
			case "question":
				rows = append(rows, humanListRow{issue: issue, kind: "QUESTION", pr: r.PR, multiPR: multi})
			case "review":
				rows = append(rows, humanListRow{issue: issue, kind: "REVIEW", pr: r.PR, multiPR: multi})
				// "external" (handed off) and "" (ungated) produce no row —
				// same "no longer awaiting Eric" / "nothing to report" logic
				// as the legacy branch above, applied per PR.
			}
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(ctx.Stdout, "No human-needed beads found.")
		return nil
	}
	for _, row := range rows {
		fmt.Fprintf(ctx.Stdout, "%s  [%s]  %s\n", row.issue.ID, row.kind, row.issue.Title)
		if row.pr != "" {
			fmt.Fprintf(ctx.Stdout, "    pr: %s\n", row.pr)
		}
		if row.multiPR {
			// 2+ PRs: only render an ask block specifically tagged for THIS
			// pr (gateKong.Run tags it when --pr was used) — never fall back
			// to the initiative's latest/raw notes, which may be about a
			// DIFFERENT PR. No tagged match => honest silence rather than a
			// guess (docs/multi-pr-contract.md follow-up, agent-teams-ssib.8:
			// a confidently-wrong pairing is worse than no pairing at all).
			if ask, ok := extractAskForPR(row.issue.Notes, row.pr); ok {
				fmt.Fprint(ctx.Stdout, renderAsk(ask))
			}
			continue
		}
		renderNoteFallback(ctx, row.issue.Notes)
	}
	return nil
}

// renderNoteFallback renders the initiative-wide latest ask block, or the
// raw last-note-block fallback when no structured ask is present — the
// original human-list rendering, unchanged, correct for a bare gate and for
// any issue with at most one resolved PR (no ambiguity to resolve). A
// multi-PR row (humanListRow.multiPR) does NOT use this — see Run.
func renderNoteFallback(ctx *cli.Context, notes string) {
	if notes == "" {
		return
	}
	if ask, ok := extractLatestAsk(notes); ok {
		fmt.Fprint(ctx.Stdout, renderAsk(ask))
	} else {
		fmt.Fprintf(ctx.Stdout, "    %s\n", lastNoteBlock(notes))
	}
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
	Role string `arg:"" name:"role" help:"Role namespace to fetch memories for." required:""`
}

func (c *learningsKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam learnings: no context")
	}
	return runLearnings(ctx, c.Role)
}

// recallKong performs a substring search over a role's memories.
type recallKong struct {
	Role  string `arg:"" name:"role"  required:"" help:"Role namespace to search."`
	Query string `arg:"" name:"query" required:"" help:"Search terms; split on whitespace, matched case-insensitively, ranked by terms matched."`
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

// memoriesJsonKong emits every role memory as a JSON array with tier +
// applied-signal join. JSON-only — no --json flag, no --role filter, no text
// table (frozen contract agent-teams-hvje.1): the sole consumer is the
// dashboard Memories tab, which fetches everything and filters client-side.
type memoriesJsonKong struct{}

func (c *memoriesJsonKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam memories-json: no context")
	}
	return runMemoriesJSON(ctx)
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
	// pr is the PR this ask is about, when the gate that wrote it was
	// scoped with --pr (kong_converted.go's gateKong.Run); "" for a bare
	// gate's block (the single-PR/no-PR case, unaffected by any of this).
	pr string
}

// extractAllAsks scans notes for every valid sentinel-delimited ateam-ask
// block, in document order. Malformed or incomplete blocks (missing closing
// sentinel) are skipped.
//
// The closing sentinel ">>>" must appear at the start of a line to avoid
// matching ">>>" embedded in prose or git conflict markers.
func extractAllAsks(notes string) []askBlock {
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

	var all []askBlock
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
			all = append(all, parsed)
		}
		remaining = after[end+len(">>>"):]
	}
	return all
}

// extractLatestAsk scans notes for the LAST sentinel-delimited ateam-ask block
// and parses it. Returns the parsed block and true when found; false otherwise.
// This is the initiative-wide "latest block regardless of which PR it might
// be tagged with" lookup — correct for a bare gate (no --pr) and for any
// initiative with at most one resolved PR, where there is no ambiguity to
// resolve. A multi-PR initiative's per-PR rows use extractAskForPR instead.
func extractLatestAsk(notes string) (askBlock, bool) {
	all := extractAllAsks(notes)
	if len(all) == 0 {
		return askBlock{}, false
	}
	return all[len(all)-1], true
}

// extractAskForPR returns the LAST ask block in notes tagged with exactly
// pr (askBlock.pr == pr), or false if none. Used by human-list's per-PR rows
// on a multi-PR initiative (2+ resolved PRs) so each row renders the block
// that was actually about THAT pr, never the initiative's latest block
// system-wide — showing PR A's row with PR B's decision text is the bug this
// exists to prevent.
func extractAskForPR(notes, pr string) (askBlock, bool) {
	var found askBlock
	ok := false
	for _, b := range extractAllAsks(notes) {
		if b.pr == pr {
			found = b
			ok = true
		}
	}
	return found, ok
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
		} else if after, ok := strings.CutPrefix(line, "pr:"); ok {
			b.pr = strings.TrimSpace(after)
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
//   - contains "gate:review" (bare or any per-PR "gate:review:<url>" suffixed
//     form, per hasGateKind/status.go) => "REVIEW"
//   - else (human present, or gate:question, or backward-compat) => "QUESTION"
func gateKind(labels []string) string {
	if hasGateKind(labels, "gate:review") {
		return "REVIEW"
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

	// Served set = hot block (sorted) followed by fresh block (sorted) — hot
	// leads because it carries the highest-value curated entries
	// (agent-teams-bbsz.23). A flat sort.Strings across the union would
	// alphabetize "fresh:" ahead of "hot:" and undo that ordering, so each
	// tier is sorted independently instead of the merged set as a whole.
	// Falls back to allRoleKeys (sorted) when both tiers are empty,
	// preserving zero-tier backward-compat behavior.
	var keys []string
	if len(hotKeys) > 0 || len(freshKeys) > 0 {
		sort.Strings(hotKeys)
		sort.Strings(freshKeys)
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
		sort.Strings(keys)
	}
	if len(keys) == 0 {
		fmt.Fprintf(ctx.Stdout, "[learnings %s: EMPTY]\n", role)
		return nil
	}

	// Build the payload in a buffer first so the header/trailer can report its
	// exact size (chars = len of this string, i.e. bytes — documented in
	// TestLearnings_TrailerCharsCountsBytesNotRunes) without a second pass over raw.
	var payload strings.Builder
	for i, k := range keys {
		fmt.Fprintln(&payload, k)
		fmt.Fprintln(&payload, raw[k].(string))
		if i < len(keys)-1 {
			fmt.Fprintln(&payload)
		}
	}

	// stats is shared verbatim between the leading header and the trailing
	// trailer (agent-teams-bbsz.33) — a reading session that sees matching
	// stats on both ends can trust it received the whole payload; a mismatch
	// (or a missing trailer entirely, e.g. from piping through `head`) means
	// it was truncated. The trailer's own format is UNCHANGED from before
	// this bead (it is the end-marker other tooling/tests key off of).
	stats := fmt.Sprintf("%s: %d entries, %d chars, hot %d fresh %d",
		role, len(keys), payload.Len(), len(hotKeys), len(freshKeys))
	fmt.Fprintf(ctx.Stdout, "[learnings %s — read in full; do NOT pipe through head/tail or truncate; output ends at the matching trailer line]\n", stats)
	fmt.Fprint(ctx.Stdout, payload.String())
	fmt.Fprintf(ctx.Stdout, "[learnings %s]\n", stats)
	return nil
}

// recallNearestCount caps how many near-miss keys are listed when a recall
// query matches nothing (agent-teams-bbsz.22).
const recallNearestCount = 5

// runRecall performs a tokenized, ranked search over a role's memories (both
// hot and cold). The query is split on whitespace into lowercase tokens; a
// key is a match when at least one token appears (case-insensitively) as a
// substring of its key or body. Matches are ranked by the number of DISTINCT
// tokens matched, most first, tie-broken by key ascending — so a
// multi-word query surfaces its best-covered memories first instead of an
// arbitrary substring order.
//
// A header line is always printed first (agent-teams-bbsz.22, fixing
// bbsz.13's silent zero-byte miss at exit 0): on zero matches it is followed
// by up to recallNearestCount keys "nearest" to the query. This branch only
// runs when every candidate scored 0, so the shared score-then-key sort
// degenerates to a plain alphabetical ordering — "nearest" is really just
// the role's first N keys by key ascending, not a token-overlap ranking. It
// still shows the searcher what content actually exists for the role.
func runRecall(ctx *cli.Context, role, query string) error {
	rolePrefix := role + ":"
	tokens := recallTokenize(query)

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	type scoredKey struct {
		key   string
		score int
	}
	var candidates []scoredKey
	for k, v := range raw {
		body, ok := v.(string)
		if !ok {
			continue
		}
		if !strings.HasPrefix(k, rolePrefix) {
			continue
		}
		candidates = append(candidates, scoredKey{key: k, score: recallMatchedTokens(tokens, k, body)})
	}

	byScoreThenKey := func(s []scoredKey) {
		sort.Slice(s, func(i, j int) bool {
			if s[i].score != s[j].score {
				return s[i].score > s[j].score
			}
			return s[i].key < s[j].key
		})
	}

	var matches []scoredKey
	for _, c := range candidates {
		if c.score > 0 {
			matches = append(matches, c)
		}
	}
	byScoreThenKey(matches)

	fmt.Fprintf(ctx.Stdout, "[recall %s %q: %d matches]\n", role, query, len(matches))

	if len(matches) == 0 {
		nearest := append([]scoredKey(nil), candidates...)
		byScoreThenKey(nearest)
		if len(nearest) > recallNearestCount {
			nearest = nearest[:recallNearestCount]
		}
		if len(nearest) > 0 {
			nearestKeys := make([]string, len(nearest))
			for i, c := range nearest {
				nearestKeys[i] = c.key
			}
			fmt.Fprintf(ctx.Stdout, "nearest: %s\n", strings.Join(nearestKeys, " "))
		}
		return nil
	}

	for i, m := range matches {
		fmt.Fprintln(ctx.Stdout, m.key)
		fmt.Fprintln(ctx.Stdout, raw[m.key].(string))
		if i < len(matches)-1 {
			fmt.Fprintln(ctx.Stdout)
		}
	}
	return nil
}

// recallTokenize splits query on whitespace into lowercase tokens.
func recallTokenize(query string) []string {
	fields := strings.Fields(query)
	tokens := make([]string, len(fields))
	for i, f := range fields {
		tokens[i] = strings.ToLower(f)
	}
	return tokens
}

// recallMatchedTokens returns the count of distinct tokens that appear
// case-insensitively as a substring of key or body.
func recallMatchedTokens(tokens []string, key, body string) int {
	lowerKey := strings.ToLower(key)
	lowerBody := strings.ToLower(body)
	count := 0
	for _, t := range tokens {
		if strings.Contains(lowerKey, t) || strings.Contains(lowerBody, t) {
			count++
		}
	}
	return count
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
		// "applied" is the applied-signal counter namespace (applied:<role>:<slug>),
		// not a role — it must stay invisible to every learnings/condense scan
		// (frozen contract agent-teams-u71p.1). Surfacing it here would let the
		// condense all-roles sweep run `ateam condense applied`, which matches
		// the applied: prefix across every real role and leaks every counter
		// into the condense packet (and can corrupt real counters via a
		// subsequent `ateam forget applied ...`).
		if role == "applied" {
			continue
		}
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

// memoryRecord is the JSON shape emitted by `ateam memories-json`; json tags
// match the dashboard/shared/types.ts MemoryEntry contract exactly (frozen
// agent-teams-hvje.1). LastApplied is *string so an absent applied record
// emits JSON null (never "").
type memoryRecord struct {
	Role         string  `json:"role"`
	Key          string  `json:"key"`
	Slug         string  `json:"slug"`
	Tier         string  `json:"tier"`
	Body         string  `json:"body"`
	AppliedCount int     `json:"appliedCount"`
	LastApplied  *string `json:"lastApplied"`
}

// runMemoriesJSON emits every role memory as a JSON array with tier +
// applied-signal join, sorted by key ascending. Mirrors runRoles's
// key-derivation rules (role = substring before the first ":"; the
// "applied:" namespace is joined in via lookupApplied, not listed as its own
// role) plus condense's slug/applied-join pattern (condenseKong.Run).
func runMemoriesJSON(ctx *cli.Context) error {
	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return err
	}

	var keys []string
	for k, v := range raw {
		if _, ok := v.(string); !ok {
			continue
		}
		idx := strings.Index(k, ":")
		if idx < 0 {
			continue
		}
		if k[:idx] == "applied" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	records := make([]memoryRecord, 0, len(keys))
	for _, k := range keys {
		role := k[:strings.Index(k, ":")]
		rolePrefix := role + ":"

		tier := "cold"
		switch {
		case strings.HasPrefix(k, rolePrefix+"hot:"):
			tier = "hot"
		case strings.HasPrefix(k, rolePrefix+"fresh:"):
			tier = "fresh"
		}

		slug := condenseBareSlug(rolePrefix, k)
		appliedCount, lastApplied := lookupApplied(raw, role, slug)

		records = append(records, memoryRecord{
			Role:         role,
			Key:          k,
			Slug:         slug,
			Tier:         tier,
			Body:         raw[k].(string),
			AppliedCount: appliedCount,
			LastApplied:  strOrNil(lastApplied),
		})
	}

	enc := json.NewEncoder(ctx.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(records)
}
