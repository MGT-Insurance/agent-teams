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
	"errors"
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
//
// checkRoleDefinitionAttached was RENAMED to checkRoleTypeRegistered by the
// agent-teams-25s3.19/.20 amendment. Measured live (2026-08-06): the ONLY
// session this verb ever launches is `claude -p`, and its sidecar is a THIN
// shape (agentType, description, name, spawnDepth, toolUseId — no
// customAgentType, no taskKind, no model, no permissionMode). What that
// shape can witness is that the requested role TYPE resolved (an
// unresolvable subagent_type is rejected before any spawn, writing no
// sidecar at all — verified with two deliberately-bogus type requests that
// produced zero sidecars), NOT that the role's definition body attached.
// The rename is mandatory, not cosmetic: a check named for attachment while
// witnessing only registration is the exact silent-overclaim species this
// initiative exists to eliminate (the corpus contains a real sidecar,
// planner-baretest, where the type registered and the spawn succeeded but
// the rich-shape attachment witness, customAgentType, was silently absent —
// anthropics/claude-code#78234).
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
	checkSpawnRecordPresent  = "spawn-record-present"
	checkRoleTypeRegistered  = "role-type-registered"
	checkRoleModelAttached   = "role-model-attached"
	checkSpawnPermissionMode = "spawn-permission-mode"
)

// preflightProbedRoleKey is the --agents payload key (and expected
// agentType under this verb's thin -p sidecars) for the role the skill
// spawns (agent-teams-25s3.4 step 2: "agent-teams-implementer", hyphen key,
// no model argument).
const preflightProbedRoleKey = "agent-teams-implementer"

// preflightProbeSpawnName is the fixed name the skill spawns its one
// teammate under (agent-teams-25s3.4 step 2: `name: "preflight-probe"`).
// Matching sidecars by this name — rather than by taskKind, which the thin
// -p shape never carries — is what makes spawn-record-present salvageable
// under this launch mode (agent-teams-25s3.19/.20 amendment).
const preflightProbeSpawnName = "preflight-probe"

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

// ── role-prose-in-context: the token probe (agent-teams-25s3.4 step 3) ──────
//
// FROZEN 2026-08-06, SUPERSEDES the earlier UNPINNED ruling this same check
// id shipped under (see the bead's notes: three earlier predicates — counts,
// line positions, verbatim spans — each false-red'd a healthy install before
// UNPINNED, and UNPINNED itself was then overruled). Every earlier shape
// assumed the question had to be about text ALREADY IN the role file, which
// is why "a disobedient probe could just read roles/*.md" looked unclosable.
// This one inverts the direction: mint a value the role file could never
// contain, inject it into the probed role's prompt, and compare.
//
// OWNERSHIP SPLIT, AND WHY THE COMPARE LOGIC LIVES HERE RATHER THAN IN THE
// SKILL — the stated reason contract artifact (1)(c) requires before any
// check logic lands on the Go side: the skill asks the probe for the value
// and must report it back VERBATIM AND UNINTERPRETED, and it must NEVER be
// told the correct answer — a component that knows the right answer can leak
// it into the probe's own prompt by accident (a paraphrase, a hint, a retry
// that quotes it). Minting and comparing here instead means the only process
// that knows ground truth is one that never talks to the probe at all. That
// is the same discipline as REASON-POST-EXIT: the observer sits outside the
// thing observed.
//
// PAYLOAD SHAPE THIS VERB REQUIRES OF THE SKILL (pinned as a note on
// agent-teams-25s3.4, 2026-08-06 — no contract-artifact-(3) shape change,
// the check object stays {check,status,detail,witness,remediation}):
//   - Detail carries the probe's raw reply, VERBATIM AND UNTRIMMED. Never
//     empty — if the probe gave no reply at all (spawn or ask failed), the
//     skill emits preflightProbeNoAnswerSentinel instead of "", so "the
//     install is broken" (a healthy answer that's simply wrong) is never
//     conflated with "the probe machinery is broken" (no answer was ever
//     obtained). If the probe genuinely answered that it has nothing, the
//     skill emits preflightNoTokenSentinel (a THIRD, DIFFERENT case).
//   - Status is a placeholder the skill cannot compute — it never has
//     token, by design — and MUST be FAIL, never PASS or SKIP. Direction
//     matters: this verb unconditionally overwrites Status below, but if
//     that override path ever breaks (a refactor, an early return, an
//     unhandled branch), a FAIL placeholder ships a loud wrong red, while a
//     PASS placeholder would ship a decorative green manufactured by a
//     plumbing bug — inside the one check this initiative built specifically
//     to catch decorative greens. Do not "tidy" this placeholder to PASS.
//   - Witness/Remediation on that object are likewise placeholders, ignored
//     and fully overwritten by preflightOverrideTokenCheck below.
const checkRoleProseInContextID = "role-prose-in-context"

// preflightNoTokenSentinel and preflightProbeNoAnswerSentinel are the two
// reserved values the skill's payload-shape contract requires in Detail
// instead of an empty string (see the note above) — named here, not just
// documented in prose, so checkRoleProseInContext's branches can't silently
// drift from what the skill actually emits.
//
//	preflightNoTokenSentinel:      the probe ANSWERED and had no token — the
//	                               install is broken (the role definition
//	                               likely didn't attach).
//	preflightProbeNoAnswerSentinel: no answer was ever obtained at all — the
//	                               probe/spawn machinery itself failed, a
//	                               different root cause from the above.
const (
	preflightNoTokenSentinel       = "NO-TOKEN"
	preflightProbeNoAnswerSentinel = "PROBE-NO-ANSWER"
)

// preflightTokenSectionFormat is the exact section appended to the probed
// role's prompt in the --agents payload. FROZEN wording — verified working
// in the DRI's live control. An earlier phrasing that asked for "your
// preflight token" by name failed: a HEALTHY agent replied NO-TOKEN because
// it had the text but had not categorised it under that name. The skill's
// own question must point at this section explicitly for the same reason —
// never assume an agent has named its own instructions the way we would.
const preflightTokenSectionFormat = "\n\n## Preflight self-check\n\nPREFLIGHT-TOKEN: %s\n\nIf asked, reply with that value. It is a disposable check value, not a secret.\n"

// mintPreflightToken returns a fresh, cryptographically random hex token.
// FRESHNESS IS LOAD-BEARING: a cached, reused, or derived token would let a
// probe that saw a PREVIOUS run's transcript (or a hardcoded guess) replay
// the answer without the role ever attaching on THIS run. Mint per
// invocation; never cache; never derive from anything stable (env, cwd, the
// session id).
func mintPreflightToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Same posture as newPreflightSessionID: no entropy source is
		// effectively unrecoverable for a check whose entire value depends
		// on the token being unguessable.
		panic(fmt.Sprintf("ateam preflight: crypto/rand: %v", err))
	}
	return fmt.Sprintf("%x", b)
}

// injectPreflightToken appends the token section to the probed role's
// prompt (preflightProbedRoleKey) inside the built --agents payload and
// returns the re-marshaled payload. This touches ONLY the in-memory payload
// on its way to argv — never a file, never a log (token-hygiene requirement
// 2: the payload was already verified not to reach disk anywhere today, and
// this function adds no new write path). Re-validates that the probed role
// entry exists rather than trusting that a caller's earlier gate ran first.
func injectPreflightToken(agentsJSON, token string) (string, error) {
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(agentsJSON), &payload); err != nil {
		return "", fmt.Errorf("parse --agents payload: %w", err)
	}
	entry, ok := payload[preflightProbedRoleKey]
	if !ok {
		return "", fmt.Errorf("--agents payload has no %q entry", preflightProbedRoleKey)
	}
	entry.Prompt += fmt.Sprintf(preflightTokenSectionFormat, token)
	payload[preflightProbedRoleKey] = entry

	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal --agents payload with token section: %w", err)
	}
	return string(b), nil
}

// checkRoleProseInContext computes the FINAL role-prose-in-context verdict.
// observed is the skill's verbatim, uninterpreted report of what the probe
// replied (see the payload-shape note above); token is the value this
// invocation minted and injected.
//
// EXACT match only (token-hygiene requirement 3): no case-folding, no
// substring — the whole point is an arbitrary value reproduced precisely.
// observed is trimmed of surrounding whitespace before comparing (planner
// ruling 2026-08-06): that is JSON/LLM-output hygiene (a stray trailing
// newline is not a different answer), and it is the ONE narrow allowance —
// no case-folding, no substring, no punctuation stripping, no markdown
// unwrapping. The skill still reports verbatim and untrimmed; the trim
// happens only here, on the verb side, where ground truth lives — the
// skill must never become a place where a reply gets tidied before
// comparison.
//
// Detail IS SHIPPED TEXT, not a transport slot (planner correction
// 2026-08-06): the skill's raw Detail is consumed here and REPLACED with a
// human-facing sentence for every outcome, always naming the raw value
// that came back so an operator can see what actually happened rather than
// just that it was wrong. The three FAIL shapes are kept distinct because
// they point at different root causes (also planner correction — collapsing
// them loses "the install is broken" vs "the probe machinery is broken"):
//   - preflightNoTokenSentinel: the probe answered and had nothing — the
//     role definition likely never attached.
//   - preflightProbeNoAnswerSentinel: no answer was ever obtained — the
//     probe/spawn machinery failed, independent of role attachment.
//   - anything else (including the cheat-control's "NOT-IN-FILE", or a
//     genuinely wrong value): a generic mismatch.
//
// WITNESS WORDING IS PINNED (agent-teams-25s3.4 step 3, FROZEN): state the
// raised bar, never claim uncheatability. The token rides on the --agents
// argv (preflightLaunchArgs), so a disobedient probe with Bash could shell
// out and read its parent process's argv mid-run without the role ever
// attaching — that residual is real and is named here, not hidden. This is
// the fifth residual claimed in this initiative; the previous four were each
// declared closed and each declaration was wrong, so this wording must never
// be loosened into a stronger claim.
func checkRoleProseInContext(observed, token string) preflightCheck {
	const witness = "live probe (raised bar: a probe would have to ignore its instructions AND read its parent process's argv mid-run to obtain the token without the role attaching)"

	trimmed := strings.TrimSpace(observed)

	switch trimmed {
	case token:
		return preflightCheck{
			Check:   checkRoleProseInContextID,
			Status:  preflightPass,
			Detail:  "token matched",
			Witness: witness,
		}
	case preflightNoTokenSentinel:
		return preflightCheck{
			Check:       checkRoleProseInContextID,
			Status:      preflightFail,
			Detail:      fmt.Sprintf("token absent — probe replied %q", trimmed),
			Witness:     witness,
			Remediation: "the probe answered but reported no token — the probed role's assembled prompt likely never carried the injected Preflight self-check section; confirm --agents is present in the probe launch argv and that the probed role's definition attached",
		}
	case preflightProbeNoAnswerSentinel:
		return preflightCheck{
			Check:       checkRoleProseInContextID,
			Status:      preflightFail,
			Detail:      fmt.Sprintf("no reply obtained — probe replied %q", trimmed),
			Witness:     witness,
			Remediation: "the probe never produced an answer to compare — this points at the skill's own spawn/ask step failing, not necessarily a dropped role definition; check the probe session's transcript for why it never replied",
		}
	default:
		return preflightCheck{
			Check:       checkRoleProseInContextID,
			Status:      preflightFail,
			Detail:      fmt.Sprintf("token mismatch — probe replied %q", trimmed),
			Witness:     witness,
			Remediation: "the probe answered with a value that doesn't match the injected token — confirm --agents is present in the probe launch argv and that the probed role's definition attached (or, if the probe was told it may read files, that this isn't the cheat-control shape reading roles/*.md directly)",
		}
	}
}

// preflightOverrideTokenCheck replaces the skill's role-prose-in-context
// entry — which the payload shape above says carries only the probe's raw
// observed reply, verbatim and untrimmed, in Detail, with a FAIL placeholder
// in Status — with this verb's own PASS/FAIL comparison against token.
// EVERY field (Status, Detail, Witness, Remediation) is overwritten: Detail
// is shipped text, not a transport slot, so the skill's raw value must not
// survive into the final report unformatted. Does nothing if the check is
// absent: that is only legitimate on Step 1's standalone stop, and
// parsePreflightVerdict/preflightMissingSkillChecks already assert it can't
// be silently absent for any other reason before this function ever runs.
func preflightOverrideTokenCheck(checks []preflightCheck, token string) []preflightCheck {
	for i, c := range checks {
		if c.Check == checkRoleProseInContextID {
			checks[i] = checkRoleProseInContext(c.Detail, token)
			return checks
		}
	}
	return checks
}

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
		// STATUSES AND EXIT CODE MUST AGREE (agent-teams-25s3.2 artifact (7),
		// interpretation note 2026-08-06). Exit 2 is an anti-false-green
		// device for when the INSTRUMENT failed; using it alongside a FAIL
		// says "the install is broken" and "I could not form a judgement" in
		// one breath. The discriminator: could this condition exist on a
		// HEALTHY install? An unresolvable roles dir — wrong cwd, unset
		// $CLAUDE_PLUGIN_ROOT, binary run from a scratch path — yes, so it is
		// environment: no FAIL, all SKIP, exit 2. A malformed role file
		// inside a directory that DID resolve — no, that is the install
		// being broken, which is what exit 1 means.
		if errors.Is(err, errRolesDirUnresolvable) {
			checks := preflightSkippedChecks("the roles directory could not be resolved; no probe session was launched")
			result := buildPreflightResult(checks, "")
			if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
				return renderErr
			}
			fmt.Fprintf(ctx.Stderr, "ateam preflight: %v\n", err)
			return cli.Silent(2)
		}
		checks := append([]preflightCheck{{
			Check:       checkRolesPayloadBuilds,
			Status:      preflightFail,
			Detail:      err.Error(),
			Witness:     "plugins/agent-teams/roles/*.md",
			Remediation: "fix the role file named above (missing/malformed frontmatter) so the --agents payload can be built, then re-run",
		}}, preflightSkippedChecks("the --agents payload failed to build; no probe session was launched")...)
		result := buildPreflightResult(checks, "")
		if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
			return renderErr
		}
		return cli.Silent(1)
	}

	// The payload BUILDING is not the property. The property is that it
	// carries the role the probe is about to request — buildPayload succeeds
	// on any well-formed roles/ directory, including one the probed role has
	// been deleted from. Live negative control (agent-teams-25s3.3,
	// 2026-08-06): with roles/implementer.md removed, this check PASSED at
	// 36733 bytes, the run spent $0.25, and the failure surfaced downstream
	// as role-types-available with a remediation telling the operator to run
	// the command they had just run. Asserting it here is REASON-NO-SESSION,
	// deterministic, free, and can name the actual missing file.
	if _, err := preflightExpectedProbeModel(agentsJSON); err != nil {
		checks := append([]preflightCheck{{
			Check:       checkRolesPayloadBuilds,
			Status:      preflightFail,
			Detail:      fmt.Sprintf("--agents payload built (%d bytes) but carries no %q entry: %v", len(agentsJSON), preflightProbedRoleKey, err),
			Witness:     "plugins/agent-teams/roles/*.md (built payload)",
			Remediation: fmt.Sprintf("the probed role is missing from the resolved roles directory — restore roles/implementer.md (the %q definition), then re-run", preflightProbedRoleKey),
		}}, preflightSkippedChecks("the --agents payload does not carry the probed role; no probe session was launched")...)
		result := buildPreflightResult(checks, "")
		if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
			return renderErr
		}
		// Exit 1, not 2: a FAIL was emitted, so a verdict WAS formed. A
		// missing role file cannot happen on a healthy install — that is the
		// install being broken, which is exactly what this tool exists to
		// report. Exit 2 here would read as infrastructure flakiness in CI
		// and get retried instead of acted on.
		return cli.Silent(1)
	}

	// TOKEN PROBE (agent-teams-25s3.4 step 3, FROZEN): mint fresh and inject
	// into the probed role's prompt ONLY, before this payload ever reaches
	// argv. See the checkRoleProseInContext doc comment for the full design.
	token := mintPreflightToken()
	tokenizedAgentsJSON, err := injectPreflightToken(agentsJSON, token)
	if err != nil {
		// Unreachable in practice — preflightExpectedProbeModel above already
		// confirmed this payload carries preflightProbedRoleKey — guarded
		// anyway rather than silently launching a session nothing will probe.
		checks := append([]preflightCheck{{
			Check:       checkRolesPayloadBuilds,
			Status:      preflightFail,
			Detail:      fmt.Sprintf("could not inject the preflight token into the probed role's prompt: %v", err),
			Witness:     "plugins/agent-teams/roles/*.md (built payload)",
			Remediation: "confirm roles/implementer.md still resolves and its frontmatter parses",
		}}, preflightSkippedChecks("token injection failed; no probe session was launched")...)
		result := buildPreflightResult(checks, "")
		if renderErr := renderPreflight(ctx, result, c.JSON); renderErr != nil {
			return renderErr
		}
		return cli.Silent(1)
	}

	sessionID := newPreflightSessionID()

	stdout, err := launch(sessionID, tokenizedAgentsJSON, skill, c.MaxBudgetUSD, c.PluginDir)
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
		checks = append(checks, preflightOverrideTokenCheck(verdict.Checks, token)...)
	}

	// Verb-owned sidecar checks run regardless of whether the skill's own
	// JSON verdict parsed: the sidecar is the harness's own record and is
	// independent of what the skill printed, so a broken skill emission
	// doesn't hide a healthy (or unhealthy) spawn.
	checks = append(checks, preflightSidecarChecks(scan, sleep, sessionID, preflightSkillDeclinedToSpawn(checks))...)

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
	ids := []string{checkProbeSessionVerdict, checkSpawnRecordPresent, checkRoleTypeRegistered, checkRoleModelAttached, checkSpawnPermissionMode}
	out := make([]preflightCheck, 0, len(ids))
	for _, id := range ids {
		out = append(out, preflightCheck{Check: id, Status: preflightSkip, Detail: reason})
	}
	return out
}

// preflightRoleModelCheck computes the role-model-attached verdict for one
// sidecar — the REAL predicate, correct, and mutation-verified twice
// (agent-teams-25s3.3 commits 35fbaff, 7a8b87d). CURRENTLY UNREACHABLE FROM
// Run(): agent-teams-25s3.19/.20 measured live that the ONLY sidecar shape
// this verb's `claude -p` launch mode ever produces is the THIN five-key
// shape, which never carries a `model` field at all (0 written, not merely
// 0 matching an expectation) — so this function's entire premise, a sidecar
// that MIGHT carry the field, does not hold under this launch mode.
// preflightSidecarChecks ROUTES AROUND this function today, emitting a
// direct UNPINNED for role-model-attached instead of calling it — calling
// it against an always-absent field would report a per-run FAIL for a
// property this launch mode can never witness, which is exactly the
// false-red direction this rewrite exists to fix.
//
// KEPT, NOT DELETED, on team-lead's explicit instruction: this becomes live
// again — reachable from preflightSidecarChecks once more — the moment the
// launch mode changes to one that writes a rich sidecar (e.g. contract
// artifact (6) is amended to launch a dispatched/background session rather
// than `-p`). Its own fixtures (TestPreflightRoleModelCheck_*) keep proving
// it correct in isolation so it is ready to be wired back in without
// re-deriving any of this.
//
// MEASURED, not assumed (agent-teams-25s3.2 note, clean first-party control
// captured 2026-08-05: spawning agent-teams-tester from an opus session
// with NO model argument produced sidecar model=sonnet, matching
// roles/tester.md's frontmatter, not the caller's session model) — that
// control was against a DISPATCHED session's rich sidecar, which is exactly
// the population this function is designed for and exactly the population
// this verb never creates.
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
//
// REWRITTEN by the agent-teams-25s3.19/.20 amendment: this verb only ever
// launches `claude -p`, and that launch mode's sidecar is the THIN shape
// (agentType, description, name, spawnDepth, toolUseId — no taskKind, no
// customAgentType, no model, no permissionMode), never the rich shape a
// dispatched session produces. Two of the four checks stay real predicates
// under the thin shape (spawn-record-present, role-type-registered); two
// become honest UNPINNED because their fields are never written under this
// launch mode at all (role-model-attached, spawn-permission-mode) — see
// preflightRoleModelCheck's doc comment for why that function is kept but
// routed around rather than called.
//
// SKIP CASCADE PRESERVED: when no sidecar is found, all three dependent
// checks SKIP, never FAIL — a live checkpoint run (agent-teams-25s3.3,
// 2026-08-06) confirmed one root cause should produce one FAIL and three
// honest SKIPs, not four reds, so the report points at the single broken
// thing.
// preflightSkillDeclinedToSpawn reports whether the skill's own verdict says
// it correctly refused to spawn: role-types-available FAIL is its specified
// Step 1 stop, not a fault. An absent sidecar is then the RIGHT outcome, and
// reporting spawn-record-present FAIL would be a second red for one root
// cause — accusing the upstream teammate-spawn regression, which is exactly
// what is NOT happening. Same keying the skill-owned-check assertion already
// uses (preflightMissingSkillChecks); this branch was simply never given it.
// Live negative control, agent-teams-25s3.3 2026-08-06.
func preflightSkillDeclinedToSpawn(checks []preflightCheck) bool {
	for _, c := range checks {
		if c.Check == preflightSkillStandaloneStopID {
			return c.Status == preflightFail
		}
	}
	return false
}

func preflightSidecarChecks(scan preflightSidecarScanFunc, sleep func(time.Duration), sessionID string, skillDeclinedToSpawn bool) []preflightCheck {
	if skillDeclinedToSpawn {
		reason := "the skill correctly did not spawn: the probed role type was absent from the probe session (role-types-available FAIL)"
		return []preflightCheck{
			{Check: checkSpawnRecordPresent, Status: preflightSkip, Detail: reason, Witness: "probe session verdict (role-types-available)"},
			{Check: checkRoleTypeRegistered, Status: preflightSkip, Detail: reason},
			{Check: checkRoleModelAttached, Status: preflightSkip, Detail: reason},
			{Check: checkSpawnPermissionMode, Status: preflightSkip, Detail: reason},
		}
	}

	sidecars := pollPreflightSidecars(scan, sleep, sessionID, preflightExpectedTeammates, preflightSidecarPollDeadline, preflightSidecarPollInterval)

	if len(sidecars) < preflightExpectedTeammates {
		// AMENDMENT (A): EXPECT-N, not best-effort. Fewer teammate sidecars
		// than the skill was told to spawn is a hard FAIL, never a silent
		// pass — a healthy-looking zero-findings read is indistinguishable
		// from "the sidecar has not landed yet" otherwise. ZERO FINDINGS
		// WHEN N ARE EXPECTED MUST NEVER EXIT 0.
		detail := fmt.Sprintf("observed %d sidecar(s) named %q for session %s, expected %d", len(sidecars), preflightProbeSpawnName, sessionID, preflightExpectedTeammates)
		reason := "no teammate sidecar was found to inspect"
		return []preflightCheck{
			{Check: checkSpawnRecordPresent, Status: preflightFail, Detail: detail, Witness: "harness subagent sidecar (~/.claude/projects/*/<session>/subagents/*.meta.json)", Remediation: "the probe's teammate spawn never landed a sidecar — re-run `ateam preflight`; if it recurs, this is the upstream teammate-spawn regression this initiative exists to catch"},
			{Check: checkRoleTypeRegistered, Status: preflightSkip, Detail: reason},
			{Check: checkRoleModelAttached, Status: preflightSkip, Detail: reason},
			{Check: checkSpawnPermissionMode, Status: preflightSkip, Detail: reason},
		}
	}

	sc := sidecars[0]
	checks := []preflightCheck{{
		Check:   checkSpawnRecordPresent,
		Status:  preflightPass,
		Detail:  fmt.Sprintf("observed %d sidecar(s) named %q, expected %d", len(sidecars), preflightProbeSpawnName, preflightExpectedTeammates),
		Witness: sc.Path,
	}}

	// role-type-registered (RENAMED from role-definition-attached — see the
	// doc comment on the const block above for why the rename is mandatory).
	// agentType is the thin shape's REQUESTED subagent_type; a sidecar
	// existing at all with agentType == the probed role proves the type
	// RESOLVED in this session (an unresolvable type is rejected outright
	// before any spawn, writing no sidecar — measured live,
	// agent-teams-25s3.19). This witnesses REGISTRATION, never ATTACHMENT:
	// it does not prove the role's prose body was non-empty. That property
	// is instead caught deterministically, pre-launch, by parseRoleFile's
	// hard-fail (agentsjson.go) surfacing through roles-payload-builds — the
	// two checks together cover what this initiative wants; this one alone
	// does not.
	if sc.AgentType == preflightProbedRoleKey {
		checks = append(checks, preflightCheck{Check: checkRoleTypeRegistered, Status: preflightPass, Detail: "agentType=" + preflightProbedRoleKey + " (type registered)", Witness: sc.Path + "#agentType"})
	} else {
		got := sc.AgentType
		if got == "" {
			got = "(absent)"
		}
		checks = append(checks, preflightCheck{Check: checkRoleTypeRegistered, Status: preflightFail, Detail: fmt.Sprintf("agentType=%s, want %s", got, preflightProbedRoleKey), Witness: sc.Path + "#agentType", Remediation: "the named spawn's role type did not resolve — confirm --agents is present in the probe launch argv and that roles/implementer.md still resolves"})
	}

	// role-model-attached — UNPINNED, unconditionally, under this verb's
	// ONLY launch mode. Measured live (agent-teams-25s3.19, 2026-08-06): a
	// `claude -p` probe's sidecar never carries a `model` field at all — not
	// "carries one that happens not to match", genuinely absent by launch
	// mode, every time. ROUTES AROUND preflightRoleModelCheck rather than
	// calling it (see that function's doc comment): calling it here would
	// report a per-run FAIL for a property this launch mode can never
	// witness, which is the exact false-red direction this rewrite exists
	// to fix.
	checks = append(checks, preflightCheck{
		Check:   checkRoleModelAttached,
		Status:  preflightUnpinned,
		Detail:  "a claude -p probe session's sidecar never carries a model field — not witnessable under this launch mode",
		Witness: sc.Path,
	})

	// spawn-permission-mode — UNPINNED for the same reason: the thin -p
	// sidecar never carries a permissionMode field either.
	checks = append(checks, preflightCheck{
		Check:   checkSpawnPermissionMode,
		Status:  preflightUnpinned,
		Detail:  "a claude -p probe session's sidecar never carries a permissionMode field — not witnessable under this launch mode",
		Witness: sc.Path,
	})

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
var preflightSkillOwnedChecks = []string{"role-types-available", "teammate-spawns", checkRoleProseInContextID}

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
