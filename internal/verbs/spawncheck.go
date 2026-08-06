// Package verbs — spawncheck.go implements `ateam spawn-check`
// (agent-teams-wf7o.6, contract agent-teams-wf7o.9 artifact (7)).
//
// WHY THIS EXISTS: after wf7o.16 moved the role definitions out of the
// plugin's auto-registered agents/ directory into roles/, an UNNAMED spawn of
// a missing subagent_type errors loudly (the registry rejects it). But a
// NAMED teammate spawn of a type that resolves to nothing is silent — Claude
// Code launches a generic agent with full tools and no error
// (anthropics/claude-code#78234, #81746). The deny hook (wf7o.11) blocks the
// one KNOWN-bad call shape (the removed agent-teams:<role> colon key), but a
// stale prompt, an old transcript, or an interactively-launched session with
// no --agents payload at all can still produce a silently-generic teammate.
//
// Nothing else in this design WITNESSES that a role definition actually
// attached — everything else is a model of the harness's behavior. This verb
// reads the harness's own record of what happened: the .meta.json sidecar
// Claude Code writes beside every subagent transcript at
// ~/.claude/projects/<project>/<session>/subagents/agent-*.meta.json.
//
// THE DISCRIMINATOR (measured live, frozen in wf7o.9 artifact (7)):
// customAgentType, NOT agentType. On the teammate path, agentType is ALWAYS
// overwritten with the caller-supplied spawn name — that is normal and is
// not the bug. A sidecar with taskKind == "in_process_teammate" and a
// non-empty customAgentType proves the named role definition attached. The
// same taskKind with no customAgentType key at all means the definition was
// dropped. Any sidecar without that taskKind (an ordinary unnamed subagent,
// spawnDepth >= 1, etc.) never carries customAgentType and must be SKIPPED,
// not flagged — flagging it would make every historical window permanently
// red and the tool useless.
//
// agent-teams-wf7o.19 CORRECTION: an empty customAgentType is NOT itself
// proof of a dropped definition. Built-in types (general-purpose, Explore,
// fork, claude-code-guide, ...) never carry customAgentType even when a
// named spawn of one behaves exactly as intended — only a CLI/project/user
// scope definition (i.e. one of the agent-teams-<role> hyphen keys)
// populates it. So an empty customAgentType is ambiguous between "no
// definition was ever expected" and "a definition was expected and did not
// attach", and the sidecar alone cannot resolve that ambiguity: the
// requested subagent_type is precisely the field the upstream bug discards
// from the sidecar. It is recovered instead by joining back to the PARENT
// transcript's `Agent` tool_use call that produced this sidecar (matching on
// spawn name, disambiguating same-name collisions with description) — see
// spawnCheckParentTranscriptPath / spawnCheckMatchRequestedType. Only once
// the requested type is known do we ask "was an agent-teams role requested?"
// A role request with no customAgentType is DEFINITION-DROPPED. Any other
// requested type is not a finding — it never carried customAgentType to
// begin with. When the requested type cannot be recovered at all (parent
// transcript missing/unreadable, or the join is ambiguous), that is reported
// as its own TYPE-UNKNOWN status — never silently folded into OK or DROPPED.
package verbs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// spawnCheckTeammateTaskKind is the sidecar field value that marks a
// teammate-path spawn (as opposed to an ordinary unnamed subagent). Only
// sidecars carrying this value are subject to the witness predicate.
const spawnCheckTeammateTaskKind = "in_process_teammate"

// Status strings for a spawnCheckFinding. DEFINITION-DROPPED is the exact
// token execution.md tells the DRI to look for (agent-teams-wf7o.6 spawn
// prompt) — do not reword it.
const (
	spawnCheckStatusOK = "OK"
	// spawnCheckStatusDropped is the exact token execution.md tells the DRI
	// to look for (agent-teams-wf7o.6 spawn prompt) — do not reword it.
	spawnCheckStatusDropped = "DEFINITION-DROPPED"
	// spawnCheckStatusUnknown means the requested subagent_type could not be
	// recovered from the parent transcript (missing/unreadable transcript,
	// or an ambiguous join). This is deliberately its own status, distinct
	// from both OK and DEFINITION-DROPPED — folding it into either would be
	// exactly the kind of silent guess this verb exists to eliminate
	// (agent-teams-wf7o.19).
	spawnCheckStatusUnknown = "TYPE-UNKNOWN"
)

// spawnCheckRoleNames are the CLI-scope role definitions
// `ateam dispatch`/`ateam resume` inject via --agents (execution.md's Spawn
// rule). A named spawn requesting one of these — by either the correct
// hyphen key or the removed colon key — is a request for a role definition
// to attach; any other requested type (general-purpose, Explore, fork,
// claude-code-guide, a plugin-scoped name, ...) is not.
//
// This list must stay in step with plugins/agent-teams/roles/*.md. It is a
// literal rather than a directory scan because spawn-check reads harness
// sidecars and must work even where the plugin's roles/ dir cannot be
// resolved — but a role missing here is INVISIBLE: spawn-check simply stops
// classifying that role's spawns as role requests, so the one guard that
// catches a silently-generic teammate stops watching it, with nothing going
// red. TestSpawnCheckRoleNamesMatchRolesDir pins the two together.
var spawnCheckRoleNames = []string{"planner", "implementer", "reviewer", "tester", "investigator"}

// RegisterSpawnCheckKong registers the spawn-check verb onto p.
func RegisterSpawnCheckKong(p *cli.Parser) {
	p.AddVerb("spawn-check", "Witness whether a named teammate spawn actually attached its role definition (reads the harness's own subagent sidecar).", &spawnCheckKong{})
}

// spawnCheckKong is the kong-native form of the spawn-check verb.
type spawnCheckKong struct {
	Session string `name:"session" help:"Only check sidecars under this session id (default: $CLAUDE_CODE_SESSION_ID, the current session)."`
	Since   string `name:"since" help:"Only check sidecars modified at/after this date (RFC3339 or YYYY-MM-DD). Sweeps every session when --session is not also given."`
	JSON    bool   `name:"json" help:"Output machine-readable JSON instead of a table."`
}

// spawnCheckSidecar is the subset of a .meta.json sidecar this verb reads.
// Unrecognized fields are ignored by encoding/json's default behavior, so a
// sidecar shape gaining new fields upstream does not break parsing.
type spawnCheckSidecar struct {
	TaskKind        string `json:"taskKind"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	AgentType       string `json:"agentType"`
	CustomAgentType string `json:"customAgentType"`
	// ParentAgentID, when non-empty, means this spawn happened at
	// spawnDepth >= 1: some OTHER agent (not the top-level session) made the
	// Agent tool_use call that produced this sidecar. That call lives in
	// that parent agent's own subagent transcript
	// (<session>/subagents/agent-<ParentAgentID>.jsonl), a sibling of this
	// sidecar file — not in the top-level <session-id>.jsonl. See
	// spawnCheckParentTranscriptPath.
	ParentAgentID string `json:"parentAgentId"`
}

// spawnCheckFinding is one in_process_teammate sidecar's verdict. JSON field
// names are this verb's own contract — nothing downstream depends on them
// yet, so they are not frozen elsewhere.
type spawnCheckFinding struct {
	File            string `json:"file"`
	Name            string `json:"name"`
	AgentType       string `json:"agent_type"`
	CustomAgentType string `json:"custom_agent_type,omitempty"`
	// RequestedType is the subagent_type recovered by joining back to the
	// parent transcript's Agent tool_use call. Populated only when the join
	// was attempted (CustomAgentType empty) and succeeded; left blank when
	// CustomAgentType already proved the definition attached (no join
	// needed) or when Status is TYPE-UNKNOWN (join failed — see Note).
	RequestedType string `json:"requested_type,omitempty"`
	Status        string `json:"status"`
	// Note explains a TYPE-UNKNOWN status (why the requested type could not
	// be recovered). Empty for OK and DEFINITION-DROPPED findings.
	Note string `json:"note,omitempty"`
}

// spawnCheckResult is the top-level --json payload.
type spawnCheckResult struct {
	Findings     []spawnCheckFinding `json:"findings"`
	DroppedCount int                 `json:"dropped_count"`
	UnknownCount int                 `json:"unknown_count"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *spawnCheckKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam spawn-check: nil context")
	}

	sessionFilter := c.Session
	var since *time.Time
	if c.Since != "" {
		t, err := parseSpawnCheckSince(c.Since)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam spawn-check: invalid --since %q: %v\n", c.Since, err)
			return cli.Silent(1)
		}
		since = &t
	}

	// Default mode (neither flag given): the current session only, resolved
	// from $CLAUDE_CODE_SESSION_ID — "the mode a DRI runs right after its
	// first named spawn" (agent-teams-wf7o.6).
	if sessionFilter == "" && since == nil {
		sessionFilter = os.Getenv(sessionIDEnvVar)
		if sessionFilter == "" {
			fmt.Fprintf(ctx.Stderr, "ateam spawn-check: no session to check — $%s is not set; pass --session <id> or --since <date>\n", sessionIDEnvVar)
			return cli.Silent(1)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("ateam spawn-check: resolve home dir: %w", err)
	}
	root := filepath.Join(home, ".claude", "projects")

	findings, warnings, err := scanSpawnCheck(root, sessionFilter, since)
	if err != nil {
		return fmt.Errorf("ateam spawn-check: %w", err)
	}

	for _, w := range warnings {
		fmt.Fprintf(ctx.Stderr, "ateam spawn-check: WARN: %s\n", w)
	}

	droppedCount, unknownCount := 0, 0
	for _, f := range findings {
		switch f.Status {
		case spawnCheckStatusDropped:
			droppedCount++
		case spawnCheckStatusUnknown:
			unknownCount++
		}
	}

	if c.JSON {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(spawnCheckResult{Findings: findings, DroppedCount: droppedCount, UnknownCount: unknownCount}); err != nil {
			return fmt.Errorf("ateam spawn-check: encode JSON: %w", err)
		}
	} else if err := renderSpawnCheckText(ctx.Stdout, findings, droppedCount, unknownCount); err != nil {
		return fmt.Errorf("ateam spawn-check: %w", err)
	}

	if droppedCount > 0 {
		return cli.Silent(1)
	}
	return nil
}

// scanSpawnCheck walks projectsRoot for subagent sidecars and applies the
// witness predicate frozen in wf7o.9 artifact (7). sessionID, when non-empty,
// restricts the scan to
// <projectsRoot>/*/<sessionID>/subagents/*.meta.json — any project, that one
// session — rather than requiring the caller to know which project directory
// a session lives under. since, when non-nil, additionally drops any sidecar
// file whose mtime predates it (the historical-sweep mode).
//
// A missing or empty projectsRoot/session directory is not an error — it
// yields zero findings, matched by filepath.Glob returning no matches.
// Malformed JSON or an unreadable file is reported as a warning and skipped,
// never treated as a dropped definition: we cannot prove a negative about a
// sidecar we could not parse.
func scanSpawnCheck(projectsRoot, sessionID string, since *time.Time) ([]spawnCheckFinding, []string, error) {
	pattern := filepath.Join(projectsRoot, "*", "*", "subagents", "*.meta.json")
	if sessionID != "" {
		pattern = filepath.Join(projectsRoot, "*", sessionID, "subagents", "*.meta.json")
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	sort.Strings(matches)

	var findings []spawnCheckFinding
	var warnings []string
	for _, path := range matches {
		if since != nil {
			info, statErr := os.Stat(path)
			if statErr != nil {
				warnings = append(warnings, fmt.Sprintf("stat %s: %v", path, statErr))
				continue
			}
			if info.ModTime().Before(*since) {
				continue
			}
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("read %s: %v", path, readErr))
			continue
		}

		var sc spawnCheckSidecar
		if unmarshalErr := json.Unmarshal(data, &sc); unmarshalErr != nil {
			warnings = append(warnings, fmt.Sprintf("malformed JSON in %s: %v", path, unmarshalErr))
			continue
		}

		// Not a teammate-path spawn (ordinary unnamed subagent, no taskKind
		// at all, or some other taskKind) — SKIPPED, not flagged. This is
		// the branch that keeps a historical sweep from being permanently
		// red: most sidecars on any real machine are not teammate spawns.
		if sc.TaskKind != spawnCheckTeammateTaskKind {
			continue
		}

		name := sc.Name
		if name == "" {
			name = sc.AgentType
		}

		var status, requestedType, note string
		if sc.CustomAgentType != "" {
			// A populated customAgentType is proof on its own that a role
			// definition attached — no need to recover what was requested.
			status = spawnCheckStatusOK
		} else if sc.Name == "" {
			// No spawn name to join against a parent transcript's Agent
			// tool_use input.name. Not expected in practice (the harness
			// only sets taskKind=in_process_teammate for named spawns), but
			// handled explicitly rather than guessing.
			status = spawnCheckStatusUnknown
			note = "sidecar has no name field to join against a parent transcript"
		} else {
			rt, determined, reason := spawnCheckRecoverRequestedType(path, sc)
			if !determined {
				status = spawnCheckStatusUnknown
				note = reason
			} else {
				requestedType = rt
				if spawnCheckIsRoleRequest(rt) {
					status = spawnCheckStatusDropped
				} else {
					status = spawnCheckStatusOK
				}
			}
		}

		findings = append(findings, spawnCheckFinding{
			File:            path,
			Name:            name,
			AgentType:       sc.AgentType,
			CustomAgentType: sc.CustomAgentType,
			RequestedType:   requestedType,
			Status:          status,
			Note:            note,
		})
	}

	return findings, warnings, nil
}

// spawnCheckIsRoleRequest reports whether subagentType names one of the
// agent-teams roles execution.md's Spawn rule injects via --agents — via
// either the correct hyphen key (agent-teams-<role>, which DOES attach a
// definition) or the removed colon key (agent-teams:<role>, which no longer
// resolves to anything and is the historical KNOWN-bad shape this verb was
// built to catch). Any other requested type (general-purpose, Explore, fork,
// claude-code-guide, a plugin-scoped name, ...) is not a role request and
// never carries customAgentType even when healthy.
func spawnCheckIsRoleRequest(subagentType string) bool {
	for _, role := range spawnCheckRoleNames {
		if subagentType == "agent-teams-"+role || subagentType == "agent-teams:"+role {
			return true
		}
	}
	return false
}

// spawnCheckAgentCall is one `Agent` tool_use invocation recovered from a
// transcript: what the caller actually asked for, before the harness drops
// subagent_type from the sidecar it later writes.
type spawnCheckAgentCall struct {
	Name         string
	Description  string
	SubagentType string
}

// spawnCheckParentTranscriptPath returns the transcript that should contain
// the `Agent` tool_use call which produced the sidecar at sidecarPath.
//
//   - spawnDepth 0 (parentAgentID == ""): the top-level session transcript,
//     <projectDir>/<session-id>.jsonl — a SIBLING of the <session-id>/
//     directory that contains subagents/, not a file inside it.
//   - spawnDepth >= 1 (parentAgentID set): the spawning agent's OWN subagent
//     transcript, <session-id>/subagents/agent-<parentAgentID>.jsonl — a
//     sibling of sidecarPath itself. Every subagent transcript, regardless
//     of nesting depth, lives flat in that one subagents/ directory keyed by
//     agent id, so no recursion is needed to find it.
func spawnCheckParentTranscriptPath(sidecarPath, parentAgentID string) string {
	subagentsDir := filepath.Dir(sidecarPath)
	if parentAgentID != "" {
		return filepath.Join(subagentsDir, "agent-"+parentAgentID+".jsonl")
	}
	sessionDir := filepath.Dir(subagentsDir)
	sessionID := filepath.Base(sessionDir)
	projectDir := filepath.Dir(sessionDir)
	return filepath.Join(projectDir, sessionID+".jsonl")
}

// spawnCheckReadAgentCalls scans a transcript file and returns every `Agent`
// tool_use call found in it. Transcript lines are independent JSON records;
// a line that fails to parse (as a whole, or because its message.content is
// not the tool_use array shape) is skipped rather than failing the scan —
// this join is best-effort corroboration of a fact the sidecar cannot state,
// not the primary source of truth the way the sidecar itself is.
func spawnCheckReadAgentCalls(path string) ([]spawnCheckAgentCall, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Real transcript lines can be several MB (long prompts/tool output) —
	// bufio.Scanner's 64KiB default token limit is far too small.
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)

	var calls []spawnCheckAgentCall
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var entry struct {
			Message *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Message == nil || len(entry.Message.Content) == 0 {
			continue
		}

		var blocks []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				SubagentType string `json:"subagent_type"`
				Name         string `json:"name"`
				Description  string `json:"description"`
			} `json:"input"`
		}
		// message.content is sometimes a plain string rather than a block
		// array (e.g. a user text turn) — that failure is expected, not an
		// error worth reporting.
		if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
			continue
		}

		for _, b := range blocks {
			if b.Type == "tool_use" && b.Name == "Agent" {
				calls = append(calls, spawnCheckAgentCall{
					Name:         b.Input.Name,
					Description:  b.Input.Description,
					SubagentType: b.Input.SubagentType,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return calls, err
	}
	return calls, nil
}

// spawnCheckMatchRequestedType finds which recovered Agent call produced a
// sidecar named name (with sidecar description description), and returns
// its requested subagent_type. Matching is on name first; when more than one
// call shares that name, description narrows it (a human re-messaging an
// already-named agent, or two distinct spawns that happened to reuse a
// name, are told apart by their distinct descriptions). If candidates still
// disagree on subagent_type after that, the result is ambiguous — reported
// as undetermined rather than guessed.
func spawnCheckMatchRequestedType(calls []spawnCheckAgentCall, name, description string) (subagentType string, determined bool, reason string) {
	var candidates []spawnCheckAgentCall
	for _, c := range calls {
		if c.Name == name {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return "", false, fmt.Sprintf("no Agent tool_use call named %q found in parent transcript", name)
	}

	if len(candidates) > 1 && description != "" {
		var filtered []spawnCheckAgentCall
		for _, c := range candidates {
			if c.Description == description {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
	}

	if len(candidates) == 1 {
		return candidates[0].SubagentType, true, ""
	}

	first := candidates[0].SubagentType
	for _, c := range candidates[1:] {
		if c.SubagentType != first {
			return "", false, fmt.Sprintf("ambiguous: %d Agent tool_use calls named %q with differing subagent_type in parent transcript", len(candidates), name)
		}
	}
	return first, true, ""
}

// spawnCheckRecoverRequestedType joins the sidecar at sidecarPath back to its
// parent transcript's Agent tool_use call to recover the subagent_type that
// was actually requested — the field the upstream bug drops from the
// sidecar itself (agent-teams-wf7o.19). determined is false when the parent
// transcript could not be read at all, or when the join was ambiguous;
// reason explains why, for the TYPE-UNKNOWN note.
func spawnCheckRecoverRequestedType(sidecarPath string, sc spawnCheckSidecar) (requestedType string, determined bool, reason string) {
	parentPath := spawnCheckParentTranscriptPath(sidecarPath, sc.ParentAgentID)
	calls, err := spawnCheckReadAgentCalls(parentPath)
	if err != nil {
		return "", false, fmt.Sprintf("could not read parent transcript %s: %v", parentPath, err)
	}
	return spawnCheckMatchRequestedType(calls, sc.Name, sc.Description)
}

// parseSpawnCheckSince accepts RFC3339 or a bare YYYY-MM-DD date (interpreted
// as UTC midnight), matching the two forms a human is likely to type for
// --since.
func parseSpawnCheckSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 (2026-08-04T00:00:00Z) or YYYY-MM-DD")
}

// renderSpawnCheckText prints one aligned line per finding, then a summary
// block for droppedCount > 0 and/or unknownCount > 0. A TYPE-UNKNOWN finding
// never changes the exit code (only DEFINITION-DROPPED does — that is the
// exact token execution.md reacts to) but it must still be surfaced plainly,
// never silently treated as healthy.
func renderSpawnCheckText(w io.Writer, findings []spawnCheckFinding, droppedCount, unknownCount int) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "ateam spawn-check: no in_process_teammate sidecars found (nothing to verify)")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNAME\tAGENT_TYPE\tCUSTOM_AGENT_TYPE\tREQUESTED_TYPE\tFILE")
	for _, f := range findings {
		cat := f.CustomAgentType
		if cat == "" {
			cat = "(none)"
		}
		rt := f.RequestedType
		if rt == "" {
			rt = "(n/a)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", f.Status, f.Name, f.AgentType, cat, rt, f.File)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if droppedCount > 0 {
		if _, err := fmt.Fprintf(w, "\n%d of %d teammate spawn(s) DEFINITION-DROPPED — the named role definition did not attach; that agent is running generic with full tools. Shut it down and respawn naming the hyphen key (agent-teams-<role>).\n", droppedCount, len(findings)); err != nil {
			return err
		}
	}

	if unknownCount > 0 {
		if _, err := fmt.Fprintf(w, "\n%d of %d teammate spawn(s) have TYPE-UNKNOWN status — the requested subagent_type could not be recovered from the parent transcript, so health cannot be determined automatically. Review manually:\n", unknownCount, len(findings)); err != nil {
			return err
		}
		for _, f := range findings {
			if f.Status != spawnCheckStatusUnknown {
				continue
			}
			if _, err := fmt.Fprintf(w, "  - %s: %s (%s)\n", f.Name, f.Note, f.File); err != nil {
				return err
			}
		}
	}

	return nil
}
