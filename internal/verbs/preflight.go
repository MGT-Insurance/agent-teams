// Package verbs — preflight.go implements `ateam preflight`
// (agent-teams-25s3.3), the launcher half of the live preflight check.
//
// Contract: agent-teams-25s3.2 (FROZEN) is normative for everything this
// file assumes about status vocabulary, check-result/top-level JSON shape,
// the session-id handoff, the launch argv, and exit codes — read it before
// touching this file rather than re-deriving any of that from this comment.
//
// SHAPE AND AUTHORITY (contract artifact (1), inverted 2026-08-06 on a
// direct human ruling): plugins/agent-teams/skills/preflight/SKILL.md is the
// primary artifact and owns every check's LOGIC. This verb is a LAUNCHER —
// it owns exactly four things: launching the probe session, minting and
// passing the session id, reading what the harness recorded about that
// session AFTER the child process exits, and exit codes/rendering. Each of
// the six checks implemented directly in this file carries a
// REASON-POST-EXIT or REASON-NO-SESSION justification at its predicate
// (contract artifact (1)(c)) — this is a CLOSED list; do not add a seventh
// here without a stated reason and a note on the contract bead.
package verbs

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// Status vocabulary — contract artifact (2). Exactly these four tokens;
// UNPINNED is never produced by this file today (none of the six verb-owned
// checks has a proxy-only predicate) but the constant exists so a future
// check can't invent its own spelling.
const (
	preflightPass     = "PASS"
	preflightFail     = "FAIL"
	preflightSkip     = "SKIP"
	preflightUnpinned = "UNPINNED"
)

// The six verb-owned check ids, CLOSED per contract artifact (1) as of the
// 2026-08-06 freeze. Check ids are stable API (contract artifact (3)) —
// renaming one is a breaking change.
const (
	// REASON-NO-SESSION: runs before launch; its own failure IS "no session
	// can be launched" (contract artifact (1)(c)).
	checkRolesPayloadBuilds = "roles-payload-builds"
	// REASON-POST-EXIT: a session that fails to emit a verdict cannot
	// report that fact from inside itself.
	checkProbeSessionVerdict = "probe-session-verdict"
	// REASON-POST-EXIT (this and the three below): the sidecar is written
	// by the harness beside the transcript and cannot be read reliably from
	// inside the session that produces it — amendment (A)'s write-lag race.
	checkSpawnRecordPresent     = "spawn-record-present"
	checkRoleDefinitionAttached = "role-definition-attached"
	checkRoleModelAttached      = "role-model-attached"
	checkSpawnPermissionMode    = "spawn-permission-mode"
)

// preflightProbedRoleKey is the --agents payload key (and expected
// customAgentType) for the role the skill spawns (agent-teams-25s3.4 step
// 2: "agent-teams-implementer", hyphen key, no model argument).
const preflightProbedRoleKey = "agent-teams-implementer"

// preflightProbeSessionModel is the probe session's own --model (contract
// artifact (6)) — named as a constant, not a comment, so
// preflightRoleModelCheck's precondition gate can reference the SAME value
// productionPreflightLaunch passes as --model, rather than a doc comment
// describing it that the code never checks.
const preflightProbeSessionModel = "opus"

// preflightModelFamilies names the alias->concrete-id naming convention
// recognized as "the same family" for role-model-attached (agent-teams-
// 25s3.3 AMENDMENT 2026-08-06): a resolved concrete id like
// "claude-sonnet-5" for the "sonnet" alias. Verified against the corpus —
// 22 of 288 teammate sidecars on the census machine already carried a
// concrete id instead of an alias, so this is observed current behavior,
// not a speculative future case.
var preflightModelFamilies = []string{"sonnet", "opus", "haiku"}

// preflightModelFamily returns which family (sonnet/opus/haiku) model
// belongs to, matching either the bare alias or a "claude-<family>-..."
// concrete id. Returns "" for anything else (an unrecognized id, or "").
func preflightModelFamily(model string) string {
	for _, fam := range preflightModelFamilies {
		if model == fam || strings.HasPrefix(model, "claude-"+fam+"-") {
			return fam
		}
	}
	return ""
}

// preflightExpectedProbeModel extracts the `model` frontmatter value the
// --agents payload resolved for the probed role, so role-model-attached
// compares against a value read at RUNTIME rather than a hardcoded literal
// (agent-teams-25s3.3 AMENDMENT 2026-08-06: the sidecar's model field does
// NOT always carry the frontmatter alias — the census above — so a
// hardcoded == "sonnet" would eventually false-red a healthy install).
// Editing the role's model line moves both sides together, since this and
// buildAgentsJSON read the same source. An empty return means the role
// declares no discriminating model (frontmatter absent or "inherit") —
// callers must still treat that as a real predicate (the spawned sidecar
// then inherits the probe session's own model), not a skip.
func preflightExpectedProbeModel(agentsJSON string) (string, error) {
	var payload map[string]struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(agentsJSON), &payload); err != nil {
		return "", fmt.Errorf("parse --agents payload: %w", err)
	}
	entry, ok := payload[preflightProbedRoleKey]
	if !ok {
		return "", fmt.Errorf("--agents payload has no %q entry", preflightProbedRoleKey)
	}
	return entry.Model, nil
}

// preflightDisplayModel renders an expected-model value for a check detail
// string, naming the "no model declared" case explicitly rather than
// printing a bare empty string.
func preflightDisplayModel(model string) string {
	if model == "" {
		return "(no model declared in roles/implementer.md)"
	}
	return model
}

// preflightExpectedTeammates is how many in_process_teammate sidecars a
// healthy probe run produces. Frozen by the skill's own spec
// (agent-teams-25s3.4 step 2: "Spawn EXACTLY ONE teammate") — if the skill
// ever spawns more, this constant and the skill bead move together.
const preflightExpectedTeammates = 1

// preflightSkillName is the skill this verb launches (contract artifact
// (6)), overridable via the hidden --skill flag for tests / the loop-closed
// checkpoint's fixture skills.
const preflightSkillName = "/agent-teams:preflight"

// preflightSidecarPollDeadline / Interval implement amendment (A) on
// agent-teams-25s3.3: reading sidecars once, immediately after the child
// exits, can still race the harness's last write to the file (observed
// live: an immediate spawn-check read reported zero findings; the same
// command seconds later reported OK). Poll to a bounded deadline instead of
// reading once.
const (
	preflightSidecarPollDeadline = 10 * time.Second
	preflightSidecarPollInterval = 250 * time.Millisecond
)

// preflightBudgetAbortSubtype is the claude CLI's literal `"subtype"` value
// on a budget-exhaustion stop, verified against the installed binary
// (2.1.223): the `"type":"result"` envelope carries
// `"is_error":true,"subtype":"error_max_budget_usd"`. A budget abort is a
// TOOL failure, not an install failure (contract agent-teams-25s3.2
// AMENDMENT): no FAIL check, exit 2, every check SKIP.
const preflightBudgetAbortSubtype = "error_max_budget_usd"

// preflightCheck is one entry in the contract-(3) checks array.
type preflightCheck struct {
	Check       string `json:"check"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	Witness     string `json:"witness"`
	Remediation string `json:"remediation,omitempty"`
}

// preflightResult is the contract-(4) top-level JSON shape. Pass/Fail/Skip/
// Unpinned are recomputed by this verb over the MERGED check list (the
// skill's own checks plus the four verb-owned sidecar checks) rather than
// trusted from the skill's echo of them — see buildPreflightResult.
type preflightResult struct {
	Checks    []preflightCheck `json:"checks"`
	Pass      int              `json:"pass"`
	Fail      int              `json:"fail"`
	Skip      int              `json:"skip"`
	Unpinned  int              `json:"unpinned"`
	SessionID string           `json:"session_id"`
}

// preflightVerdict is the subset of the skill's own contract-(4) emission
// this verb reads: its checks array. The skill's own pass/fail/skip/
// unpinned/session_id fields are ignored — session_id is this verb's own
// minted uuid, not an echo, and the totals are recomputed once verb-owned
// checks are merged in (buildPreflightResult).
type preflightVerdict struct {
	Checks []preflightCheck `json:"checks"`
}

// preflightEnvelope is the subset of `claude -p --output-format json`'s
// result envelope this verb needs. Field names verified against the
// installed claude CLI (2.1.223) binary's own literal object keys:
// total_cost_usd sits beside is_error/subtype/result/type at the top level
// of the `"type":"result"` message.
type preflightEnvelope struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// preflightLaunchFunc launches the probe session and returns its captured
// stdout. Injected so tests never exec a real claude binary (bead
// acceptance: "No test may exec a real `claude`"). err is non-nil ONLY when
// no output could be captured at all — the process never got as far as
// emitting the --output-format json envelope (contract exit code 2, "the
// tool could not run the check at all"). A non-empty stdout is always
// returned as success even when the underlying process exited non-zero:
// claude -p can exit non-zero while still emitting a valid result envelope
// (e.g. an is_error result), and that envelope — not the process exit code
// — is the precise signal downstream parsing decides on.
type preflightLaunchFunc func(sessionID, agentsJSON, skill, maxBudgetUSD, pluginDir string) (stdout string, err error)

// preflightSidecarScanFunc returns the in_process_teammate sidecars found
// for sessionID at the moment it is called — ONE poll attempt, not a
// deadline loop (pollPreflightSidecars owns the loop). Injected so tests
// control exactly which reads return which fixtures without a real sleep.
type preflightSidecarScanFunc func(sessionID string) ([]spawnCheckSidecarWithPath, error)

// preflightKong is the kong-native form of `ateam preflight`.
type preflightKong struct {
	JSON         bool   `name:"json" help:"Output machine-readable JSON (contract shape) instead of a table."`
	MaxBudgetUSD string `name:"max-budget-usd" help:"Cap the probe session's API spend in USD. Unset: uncapped, passed through only when supplied."`
	Skill        string `name:"skill" hidden:"" help:"Override the skill invoked by the probe session (for tests)."`
	// PluginDir is a PRE-MERGE VERIFICATION SEAM, not a user-facing feature.
	// The verb and the skill ship as one artifact but resolve through two
	// paths: skills load from the MAIN CHECKOUT's marketplace plugin
	// directory, not from a feature branch, so before this branch merges
	// the probe session cannot reach /agent-teams:preflight through the
	// ordinary path at all — there is no other way to exercise this verb
	// end to end pre-merge. `claude --help` documents --plugin-dir as
	// "Load a plugin from a directory or .zip for this session only", so
	// it is session-scoped and leaves no residue. Hidden (never appears in
	// --help — this is not an invitation to run preflight against an
	// arbitrary plugin tree) and unset by default: empty means the flag is
	// simply absent from argv, i.e. today's exact behavior.
	PluginDir string `name:"plugin-dir" hidden:"" help:"Load the plugin from this directory for the probe session only (pre-merge verification seam)."`

	buildAgentsPayload func() (string, error)   `kong:"-"`
	launch             preflightLaunchFunc      `kong:"-"`
	scanSidecars       preflightSidecarScanFunc `kong:"-"`
	sleep              func(time.Duration)      `kong:"-"` // nil => time.Sleep
}

// RegisterPreflightKong registers the preflight verb onto p.
func RegisterPreflightKong(p *cli.Parser) {
	p.AddVerb("preflight", "Launch a live probe session and verify the agent-teams role/model/spawn plumbing end to end.", &preflightKong{
		buildAgentsPayload: buildAgentsPayload,
		launch:             productionPreflightLaunch,
		scanSidecars:       productionScanTeammateSidecars,
		sleep:              time.Sleep,
	})
}

// Validate rejects an unusable --max-budget-usd before anything is launched.
// The message text mirrors the installed claude CLI's own validation
// wording for the flag it passes through to, verified against the binary.
func (c *preflightKong) Validate() error {
	if c.MaxBudgetUSD != "" {
		v, err := strconv.ParseFloat(c.MaxBudgetUSD, 64)
		if err != nil || v <= 0 {
			return cli.Usagef("ateam preflight: --max-budget-usd must be a positive number greater than 0")
		}
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *preflightKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam preflight: nil context")
	}

	buildPayload := c.buildAgentsPayload
	if buildPayload == nil {
		buildPayload = buildAgentsPayload
	}
	launch := c.launch
	if launch == nil {
		launch = productionPreflightLaunch
	}
	scan := c.scanSidecars
	if scan == nil {
		scan = productionScanTeammateSidecars
	}
	sleep := c.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	skill := c.Skill
	if skill == "" {
		skill = preflightSkillName
	}

	// ── check 1: roles-payload-builds ────────────────────────────────────
	agentsJSON, err := buildPayload()
	if err != nil {
		checks := append([]preflightCheck{{
			Check:       checkRolesPayloadBuilds,
			Status:      preflightFail,
			Detail:      err.Error(),
			Witness:     "plugins/agent-teams/roles/*.md",
			Remediation: "fix the role file named above (missing/malformed frontmatter, or roles/ moved/renamed) so the --agents payload can be built, then re-run",
		}}, preflightSkippedChecks("the --agents payload failed to build; no probe session was launched")...)
		result := buildPreflightResult(checks, "")
		if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
			return renderErr
		}
		return cli.Silent(2)
	}

	sessionID := newPreflightSessionID()

	stdout, err := launch(sessionID, agentsJSON, skill, c.MaxBudgetUSD, c.PluginDir)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam preflight: could not launch the probe session: %v\n", err)
		return cli.Silent(2)
	}

	envelope, err := parsePreflightEnvelope(stdout)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam preflight: probe session produced no parseable --output-format json envelope: %v\n", err)
		return cli.Silent(2)
	}

	if envelope.IsError && envelope.Subtype == preflightBudgetAbortSubtype {
		fmt.Fprintf(ctx.Stderr, "ateam preflight: budget cap $%s reached before the probe finished (spent $%.2f) — re-run with a higher --max-budget-usd than %s, or omit the flag to run uncapped\n", c.MaxBudgetUSD, envelope.TotalCostUSD, c.MaxBudgetUSD)
		return cli.Silent(2)
	}

	checks := []preflightCheck{{
		Check:   checkRolesPayloadBuilds,
		Status:  preflightPass,
		Detail:  fmt.Sprintf("--agents payload built (%d bytes)", len(agentsJSON)),
		Witness: "plugins/agent-teams/roles/*.md",
	}}

	verdict, verdictErr := parsePreflightVerdict(envelope.Result)
	if verdictErr != nil {
		checks = append(checks, preflightCheck{
			Check:       checkProbeSessionVerdict,
			Status:      preflightFail,
			Detail:      verdictErr.Error(),
			Witness:     "probe session final message (--output-format json .result)",
			Remediation: "re-run `ateam preflight`; if it recurs, run the probe session's skill manually and confirm its final message is bare contract-shape JSON with no prose or fencing",
		})
	} else {
		checks = append(checks, preflightCheck{
			Check:   checkProbeSessionVerdict,
			Status:  preflightPass,
			Detail:  fmt.Sprintf("probe session emitted %d check(s)", len(verdict.Checks)),
			Witness: "probe session final message (--output-format json .result)",
		})
		checks = append(checks, verdict.Checks...)
	}

	// Verb-owned sidecar checks run regardless of whether the skill's own
	// JSON verdict parsed: the sidecar is the harness's own record and is
	// independent of what the skill printed, so a broken skill emission
	// doesn't hide a healthy (or unhealthy) spawn.
	expectedModel, expectedModelErr := preflightExpectedProbeModel(agentsJSON)
	checks = append(checks, preflightSidecarChecks(scan, sleep, sessionID, expectedModel, expectedModelErr)...)

	result := buildPreflightResult(checks, sessionID)
	if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
		return renderErr
	}
	// Cost footer (BUDGET section, agent-teams-25s3.3): report the per-run
	// cost from the --output-format json usage envelope. Always stderr, so
	// --json stdout stays exactly contract shape (4) for machine
	// consumption.
	fmt.Fprintf(ctx.Stderr, "probe session cost: $%.4f\n", envelope.TotalCostUSD)

	if result.Fail > 0 {
		return cli.Silent(1)
	}
	return nil
}

// preflightSkippedChecks returns SKIP entries for the five checks that
// never ran because roles-payload-builds failed before any session could
// be launched.
func preflightSkippedChecks(reason string) []preflightCheck {
	ids := []string{checkProbeSessionVerdict, checkSpawnRecordPresent, checkRoleDefinitionAttached, checkRoleModelAttached, checkSpawnPermissionMode}
	out := make([]preflightCheck, 0, len(ids))
	for _, id := range ids {
		out = append(out, preflightCheck{Check: id, Status: preflightSkip, Detail: reason})
	}
	return out
}

// preflightRoleModelCheck computes the role-model-attached verdict for one
// sidecar. MEASURED, not assumed (agent-teams-25s3.2 note, clean
// first-party control captured 2026-08-05: spawning agent-teams-tester from
// an opus session with NO model argument produced sidecar model=sonnet,
// matching roles/tester.md's frontmatter, not the caller's session model).
//
// PRECONDITION GATE (agent-teams-25s3.3 AMENDMENT 2026-08-06, second pass):
// this check can only discriminate "the role definition attached" from "it
// silently fell back to a generic agent" when the probed role's expected
// model DIFFERS from the probe session's own (preflightProbeSessionModel,
// contract artifact (6)) — an unattached spawn inherits the session's
// model, so if the role's declared model happens to share the session's
// family (or declares none at all), a healthy-looking match is
// indistinguishable from a silent fallback. That was previously a doc
// comment a human had to notice before repointing the probe at a different
// role; it is now a gate the code enforces, reporting UNPINNED — the
// property genuinely cannot be witnessed here (contract artifact (2)) —
// rather than a PASS that would land right for the wrong reason
// (agent-teams-25s3.15 (A1)/(A2): a false green is the direction this
// initiative exists to catch, more dangerous than a false red).
//
// Past the gate: the expected value is READ AT RUNTIME (expectedModel,
// computed by the caller from the --agents payload build), never
// hardcoded — AMENDMENT 2026-08-06 (first pass) found the sidecar's model
// field does not always carry the frontmatter alias verbatim (22 of 288
// teammate sidecars on the census machine carried a resolved concrete id,
// e.g. "claude-sonnet-5", instead). So: exact match, or a concrete id of
// the same family: PASS; absence (never observed on a teammate, 0/288) or
// any other value: FAIL.
func preflightRoleModelCheck(sc spawnCheckSidecarWithPath, expectedModel string, expectedModelErr error) preflightCheck {
	if expectedModelErr != nil {
		return preflightCheck{Check: checkRoleModelAttached, Status: preflightFail, Detail: fmt.Sprintf("could not determine the expected model: %v", expectedModelErr), Witness: "plugins/agent-teams/roles/implementer.md#model (via --agents payload)", Remediation: "confirm roles/implementer.md still resolves and its frontmatter parses"}
	}

	if expectedModel == "" || preflightModelFamily(expectedModel) == preflightModelFamily(preflightProbeSessionModel) {
		return preflightCheck{
			Check:   checkRoleModelAttached,
			Status:  preflightUnpinned,
			Detail:  fmt.Sprintf("the probed role's expected model (%s) is not distinguishable from the probe session's own (%s) — an unattached spawn silently inherits the session default and would look identical to a healthy one", preflightDisplayModel(expectedModel), preflightProbeSessionModel),
			Witness: "plugins/agent-teams/roles/implementer.md#model (via --agents payload)",
		}
	}

	switch {
	case sc.Model == "":
		return preflightCheck{Check: checkRoleModelAttached, Status: preflightFail, Detail: fmt.Sprintf("model=(absent), want %s", expectedModel), Witness: sc.Path + "#model", Remediation: "the spawned role's model did not resolve — confirm roles/implementer.md still declares a `model:` frontmatter line"}
	case sc.Model == expectedModel:
		return preflightCheck{Check: checkRoleModelAttached, Status: preflightPass, Detail: fmt.Sprintf("model=%s", sc.Model), Witness: sc.Path + "#model"}
	case preflightModelFamily(sc.Model) != "" && preflightModelFamily(sc.Model) == preflightModelFamily(expectedModel):
		return preflightCheck{Check: checkRoleModelAttached, Status: preflightPass, Detail: fmt.Sprintf("model=%s (resolved concrete id for alias %s)", sc.Model, expectedModel), Witness: sc.Path + "#model"}
	default:
		return preflightCheck{Check: checkRoleModelAttached, Status: preflightFail, Detail: fmt.Sprintf("model=%s, want %s", sc.Model, expectedModel), Witness: sc.Path + "#model", Remediation: "the spawned role's model did not resolve — confirm roles/implementer.md still declares its expected `model:` frontmatter line"}
	}
}

// preflightSidecarChecks implements the four verb-owned checks that read
// the harness's OWN post-exit sidecar record for the probe's one spawned
// teammate. All four are REASON-POST-EXIT (contract artifact (1)(c)): the
// sidecar is written by the harness beside the transcript and cannot be
// read reliably from inside the session that produces it.
func preflightSidecarChecks(scan preflightSidecarScanFunc, sleep func(time.Duration), sessionID, expectedModel string, expectedModelErr error) []preflightCheck {
	sidecars := pollPreflightSidecars(scan, sleep, sessionID, preflightExpectedTeammates, preflightSidecarPollDeadline, preflightSidecarPollInterval)

	if len(sidecars) < preflightExpectedTeammates {
		// AMENDMENT (A): EXPECT-N, not best-effort. Fewer teammate sidecars
		// than the skill was told to spawn is a hard FAIL, never a silent
		// pass — a healthy-looking zero-findings read is indistinguishable
		// from "the sidecar has not landed yet" otherwise. ZERO FINDINGS
		// WHEN N ARE EXPECTED MUST NEVER EXIT 0.
		detail := fmt.Sprintf("observed %d in_process_teammate sidecar(s) for session %s, expected %d", len(sidecars), sessionID, preflightExpectedTeammates)
		reason := "no teammate sidecar was found to inspect"
		return []preflightCheck{
			{Check: checkSpawnRecordPresent, Status: preflightFail, Detail: detail, Witness: "harness subagent sidecar (~/.claude/projects/*/<session>/subagents/*.meta.json)", Remediation: "the probe's teammate spawn never landed a sidecar — re-run `ateam preflight`; if it recurs, this is the upstream teammate-spawn regression this initiative exists to catch"},
			{Check: checkRoleDefinitionAttached, Status: preflightSkip, Detail: reason},
			{Check: checkRoleModelAttached, Status: preflightSkip, Detail: reason},
			{Check: checkSpawnPermissionMode, Status: preflightSkip, Detail: reason},
		}
	}

	sc := sidecars[0]
	checks := []preflightCheck{{
		Check:   checkSpawnRecordPresent,
		Status:  preflightPass,
		Detail:  fmt.Sprintf("observed %d in_process_teammate sidecar(s), expected %d", len(sidecars), preflightExpectedTeammates),
		Witness: sc.Path,
	}}

	// role-definition-attached: customAgentType is the harness's own record
	// of which role definition attached to the named spawn — the spawned
	// agent cannot influence it (agent-teams-25s3.3 step 5a; spawncheck.go's
	// package doc explains why this field, not agentType, is the
	// discriminator).
	if sc.CustomAgentType == preflightProbedRoleKey {
		checks = append(checks, preflightCheck{Check: checkRoleDefinitionAttached, Status: preflightPass, Detail: "customAgentType=" + preflightProbedRoleKey, Witness: sc.Path + "#customAgentType"})
	} else {
		got := sc.CustomAgentType
		if got == "" {
			got = "(absent)"
		}
		checks = append(checks, preflightCheck{Check: checkRoleDefinitionAttached, Status: preflightFail, Detail: fmt.Sprintf("customAgentType=%s, want %s", got, preflightProbedRoleKey), Witness: sc.Path + "#customAgentType", Remediation: "the named spawn did not attach a role definition — confirm --agents is present in the probe launch argv and that roles/implementer.md still resolves"})
	}

	checks = append(checks, preflightRoleModelCheck(sc, expectedModel, expectedModelErr))

	// spawn-permission-mode (amendment B): execution.md requires
	// bypassPermissions for hands-off operation — a teammate without it
	// stalls on prompts in a background session with no one to answer.
	if sc.PermissionMode == "bypassPermissions" {
		checks = append(checks, preflightCheck{Check: checkSpawnPermissionMode, Status: preflightPass, Detail: "permissionMode=bypassPermissions", Witness: sc.Path + "#permissionMode"})
	} else {
		got := sc.PermissionMode
		if got == "" {
			got = "(absent)"
		}
		checks = append(checks, preflightCheck{Check: checkSpawnPermissionMode, Status: preflightFail, Detail: fmt.Sprintf("permissionMode=%s, want bypassPermissions", got), Witness: sc.Path + "#permissionMode", Remediation: "the teammate spawn did not request bypassPermissions — a background teammate without it stalls on prompts with no one to answer; fix the spawn's mode argument"})
	}

	return checks
}

// pollPreflightSidecars implements the bounded-deadline poll amendment (A)
// requires: up to deadline/interval attempts, sleeping interval between
// them via the injected sleep func (a no-op in tests, so the loop runs to
// completion instantly without waiting on wall-clock time). Returns as soon
// as a read meets expect; otherwise returns the last read observed, however
// short, for spawn-record-present's "observed vs expected" detail.
func pollPreflightSidecars(scan preflightSidecarScanFunc, sleep func(time.Duration), sessionID string, expect int, deadline, interval time.Duration) []spawnCheckSidecarWithPath {
	attempts := int(deadline/interval) + 1
	var last []spawnCheckSidecarWithPath
	for i := 0; i < attempts; i++ {
		found, err := scan(sessionID)
		if err == nil {
			last = found
			if len(found) >= expect {
				return found
			}
		}
		if i < attempts-1 {
			sleep(interval)
		}
	}
	return last
}

// buildPreflightResult tallies the merged check list into contract shape
// (4), recomputing pass/fail/skip/unpinned itself rather than trusting any
// upstream total.
func buildPreflightResult(checks []preflightCheck, sessionID string) preflightResult {
	r := preflightResult{Checks: checks, SessionID: sessionID}
	for _, c := range checks {
		switch c.Status {
		case preflightPass:
			r.Pass++
		case preflightFail:
			r.Fail++
		case preflightSkip:
			r.Skip++
		case preflightUnpinned:
			r.Unpinned++
		}
	}
	if r.Checks == nil {
		r.Checks = []preflightCheck{}
	}
	return r
}

// parsePreflightEnvelope parses claude -p --output-format json's stdout as
// the result envelope (contract artifact (6)).
func parsePreflightEnvelope(stdout string) (preflightEnvelope, error) {
	var env preflightEnvelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &env); err != nil {
		return preflightEnvelope{}, fmt.Errorf("not valid JSON: %w", err)
	}
	if env.Type != "result" {
		return preflightEnvelope{}, fmt.Errorf(`unexpected envelope "type": %q, want "result"`, env.Type)
	}
	return env, nil
}

// preflightSkillOwnedChecks are the check ids plugins/agent-teams/skills/
// preflight/SKILL.md (agent-teams-25s3.4) emits on a normal (non-standalone-
// stop) run. THIS SET MUST MOVE WITH THE SKILL: if that bead adds, renames,
// or drops an owned check, this list — and the fixture pair pinning it
// (TestParsePreflightVerdict_MissingOwnedCheck_Fails and
// TestParsePreflightVerdict_StandaloneStopAlone_DoesNotTripMissingCheck) —
// must move too. Per contract addendum agent-teams-25s3.15 (A6): a stated
// validity condition needs a fixture, or it is documentation.
var preflightSkillOwnedChecks = []string{"role-types-available", "teammate-spawns", "role-prose-in-context"}

// preflightSkillStandaloneStopID is the one owned check whose FAIL means
// the skill stopped BY DESIGN before spawning anything (agent-teams-25s3.4
// step 1: invoked in a session not launched with --agents). A verdict where
// this FAILed and the other two owned ids are absent is COMPLETE — they
// were never going to run — not an under-count.
const preflightSkillStandaloneStopID = "role-types-available"

// preflightMissingSkillChecks reports which of the skill's owned check ids
// are absent from checks, honoring the standalone-stop exception above. An
// UNDER-COUNT is unguarded otherwise: parsePreflightVerdict's zero-checks
// floor only catches an EMPTY verdict, not one that silently dropped some
// of what the skill owns — "ran something, found something, but not
// everything expected" is the same defect class contract addendum (A5)
// names for the empty-set case, one level up.
func preflightMissingSkillChecks(checks []preflightCheck) []string {
	present := make(map[string]bool, len(checks))
	var stopStatus string
	for _, c := range checks {
		present[c.Check] = true
		if c.Check == preflightSkillStandaloneStopID {
			stopStatus = c.Status
		}
	}
	if stopStatus == preflightFail {
		return nil
	}
	var missing []string
	for _, id := range preflightSkillOwnedChecks {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// parsePreflightVerdict parses the envelope's .result as contract shape (4)
// (only the checks array is actually consumed — see preflightVerdict). A
// parse failure, valid JSON carrying zero checks, or a non-standalone-stop
// verdict missing one of the skill's owned check ids, is reported as the
// probe-session-verdict FAIL by the caller — never a silent zero-check (or
// under-count) pass (the standing rule amendment (A) states: any existing
// reader we lean on gets demonstrated against a known-bad input before we
// trust it; applied here to the skill's own handoff, not just the sidecar
// reader).
func parsePreflightVerdict(result string) (preflightVerdict, error) {
	var v preflightVerdict
	if err := json.Unmarshal([]byte(strings.TrimSpace(result)), &v); err != nil {
		return preflightVerdict{}, fmt.Errorf("final message is not valid JSON: %w", err)
	}
	if len(v.Checks) == 0 {
		return preflightVerdict{}, fmt.Errorf("final message parsed but carried zero checks")
	}
	if missing := preflightMissingSkillChecks(v.Checks); len(missing) > 0 {
		return preflightVerdict{}, fmt.Errorf("final message is missing check(s) the skill owns: %s", strings.Join(missing, ", "))
	}
	return v, nil
}

// renderPreflight renders result as a table (default) or JSON (--json),
// per contract artifact (7)'s "rendering" verb responsibility. In JSON mode
// stdout carries EXACTLY contract shape (4) — no cost footer or anything
// else — so a machine consumer never has to skip non-JSON lines.
func renderPreflight(ctx *cli.Context, result preflightResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(ctx.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("ateam preflight: encode JSON: %w", err)
		}
		return nil
	}

	tw := tabwriter.NewWriter(ctx.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCHECK\tDETAIL")
	for _, c := range result.Checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Status, c.Check, c.Detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(ctx.Stdout, "\n%d pass, %d fail, %d skip, %d unpinned\n", result.Pass, result.Fail, result.Skip, result.Unpinned)
	for _, c := range result.Checks {
		if c.Status == preflightFail && c.Remediation != "" {
			fmt.Fprintf(ctx.Stdout, "  - %s: %s\n", c.Check, c.Remediation)
		}
	}
	return nil
}

// newPreflightSessionID mints a random UUID v4 — the load-bearing
// session-id handoff, contract artifact (5). Self-contained crypto/rand
// construction rather than an external uuid dependency (none is vendored in
// this module).
func newPreflightSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// No entropy source is effectively unrecoverable — panic rather than
		// silently minting a low-entropy or colliding session id and using
		// it to scope a live, money-spending probe session.
		panic(fmt.Sprintf("ateam preflight: crypto/rand: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// preflightLaunchArgs returns the argv slice (everything after "claude") for
// the probe session launch, per contract artifact (6). Extracted as a pure
// function (mirroring bgSessionArgs, dispatch.go) so the argv itself is
// testable without executing a real claude binary. No --settings: this
// synchronous -p probe, launched and reaped inline by this verb, has
// nothing to encode in it — bgSessionSettingsJSON's env map exists for
// background-session role/initiative signaling (dispatch.go), which does
// not apply here. Contract artifact (6)'s "<payload>" is the general argv
// shape when a settings payload exists, not a mandate to always emit one.
//
// pluginDir is a PRE-MERGE VERIFICATION SEAM (see preflightKong.PluginDir):
// --plugin-dir is appended ONLY when pluginDir is non-empty, so an unset
// flag leaves the argv byte-identical to before this seam existed — a stray
// --plugin-dir in an ordinary run would silently change which plugin tree
// (and therefore which skill) the probe loads.
func preflightLaunchArgs(sessionID, agentsJSON, skill, maxBudgetUSD, pluginDir string) []string {
	args := []string{
		"-p",
		"--session-id", sessionID,
		"--output-format", "json",
		"--agents", agentsJSON,
		"--permission-mode", "bypassPermissions",
		"--model", preflightProbeSessionModel,
	}
	if maxBudgetUSD != "" {
		args = append(args, "--max-budget-usd", maxBudgetUSD)
	}
	if pluginDir != "" {
		args = append(args, "--plugin-dir", pluginDir)
	}
	return append(args, skill)
}

// productionPreflightLaunch is the production preflightLaunchFunc.
func productionPreflightLaunch(sessionID, agentsJSON, skill, maxBudgetUSD, pluginDir string) (string, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("'claude' not found in PATH")
	}

	args := preflightLaunchArgs(sessionID, agentsJSON, skill, maxBudgetUSD, pluginDir)

	cmd := exec.Command("claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if stdout.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("probe session produced no output: %s", msg)
	}
	// A non-nil runErr with SOME stdout is deliberately not surfaced here:
	// claude -p can exit non-zero while still emitting a valid
	// --output-format json result envelope (e.g. an is_error result), and
	// that envelope — parsed by the caller — is the precise signal, not the
	// process exit code.
	return stdout.String(), nil
}

// productionScanTeammateSidecars is the production preflightSidecarScanFunc.
func productionScanTeammateSidecars(sessionID string) ([]spawnCheckSidecarWithPath, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	root := filepath.Join(home, ".claude", "projects")
	return scanTeammateSidecarsForSession(root, sessionID)
}
