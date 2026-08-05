// Package verbs — spawncheck.go implements `ateam spawn-check`
// (agent-teams-wf7o.6, contract agent-teams-wf7o.9 artifact (7)).
//
// WHY THIS EXISTS: after wf7o.16 moved the four role definitions out of the
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
package verbs

import (
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
	spawnCheckStatusOK      = "OK"
	spawnCheckStatusDropped = "DEFINITION-DROPPED"
)

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
	AgentType       string `json:"agentType"`
	CustomAgentType string `json:"customAgentType"`
}

// spawnCheckFinding is one in_process_teammate sidecar's verdict. JSON field
// names are this verb's own contract — nothing downstream depends on them
// yet, so they are not frozen elsewhere.
type spawnCheckFinding struct {
	File            string `json:"file"`
	Name            string `json:"name"`
	AgentType       string `json:"agent_type"`
	CustomAgentType string `json:"custom_agent_type,omitempty"`
	Status          string `json:"status"`
}

// spawnCheckResult is the top-level --json payload.
type spawnCheckResult struct {
	Findings     []spawnCheckFinding `json:"findings"`
	DroppedCount int                 `json:"dropped_count"`
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

	droppedCount := 0
	for _, f := range findings {
		if f.Status == spawnCheckStatusDropped {
			droppedCount++
		}
	}

	if c.JSON {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(spawnCheckResult{Findings: findings, DroppedCount: droppedCount}); err != nil {
			return fmt.Errorf("ateam spawn-check: encode JSON: %w", err)
		}
	} else if err := renderSpawnCheckText(ctx.Stdout, findings, droppedCount); err != nil {
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

		status := spawnCheckStatusDropped
		if sc.CustomAgentType != "" {
			status = spawnCheckStatusOK
		}

		findings = append(findings, spawnCheckFinding{
			File:            path,
			Name:            name,
			AgentType:       sc.AgentType,
			CustomAgentType: sc.CustomAgentType,
			Status:          status,
		})
	}

	return findings, warnings, nil
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
// line naming which agents were dropped when droppedCount > 0.
func renderSpawnCheckText(w io.Writer, findings []spawnCheckFinding, droppedCount int) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "ateam spawn-check: no in_process_teammate sidecars found (nothing to verify)")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNAME\tAGENT_TYPE\tCUSTOM_AGENT_TYPE\tFILE")
	for _, f := range findings {
		cat := f.CustomAgentType
		if cat == "" {
			cat = "(none)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f.Status, f.Name, f.AgentType, cat, f.File)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if droppedCount > 0 {
		_, err := fmt.Fprintf(w, "\n%d of %d teammate spawn(s) DEFINITION-DROPPED — the named role definition did not attach; that agent is running generic with full tools. Shut it down and respawn naming the hyphen key (agent-teams-<role>).\n", droppedCount, len(findings))
		return err
	}
	return nil
}
