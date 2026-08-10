// preflight_test.go: core-path tests for `ateam preflight`
// (agent-teams-25s3.3). No test execs a real claude binary — launch and
// sidecar reads are always injected fakes/fixtures, per the bead's
// acceptance criterion. Edge cases and E2E/live verification are the
// tester's lane, not this file's.
package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// noopSleep never actually sleeps — used by every test that exercises
// pollPreflightSidecars, so a poll-to-deadline path runs in microseconds
// instead of the real 10s.
func noopSleep(time.Duration) {}

// buildPreflightEnvelope marshals a fake `claude -p --output-format json`
// result envelope. resultPayload is embedded as the (string) "result"
// field's value; callers pass the raw text they want the verb to see
// there — either a verdict JSON string (double-encoded, matching a real
// envelope) or deliberately-broken text.
func buildPreflightEnvelope(t *testing.T, isError bool, subtype, resultPayload string, costUSD float64) string {
	t.Helper()
	env := map[string]any{
		"type":           "result",
		"subtype":        subtype,
		"is_error":       isError,
		"result":         resultPayload,
		"total_cost_usd": costUSD,
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(b)
}

// buildPreflightVerdictJSON marshals a fake skill verdict (contract shape
// (4)) carrying the given checks.
func buildPreflightVerdictJSON(t *testing.T, checks []preflightCheck) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"checks": checks})
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	return string(b)
}

// extractInjectedPreflightToken parses a tokenized --agents payload (the
// second argument launch fakes receive, since preflightKong.Run injects the
// token before ever calling launch) and returns the PREFLIGHT-TOKEN value
// injected into the probed role's prompt — the reverse of
// injectPreflightToken. Fixtures that need to fabricate a skill verdict
// echoing the REAL minted token (rather than a stale hardcoded guess) call
// this from inside their launch closure, where the tokenized payload is
// available.
func extractInjectedPreflightToken(t *testing.T, agentsJSON string) string {
	t.Helper()
	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(agentsJSON), &payload); err != nil {
		t.Fatalf("extractInjectedPreflightToken: parse payload: %v", err)
	}
	entry, ok := payload[preflightProbedRoleKey]
	if !ok {
		t.Fatalf("extractInjectedPreflightToken: payload has no %q entry", preflightProbedRoleKey)
	}
	const marker = "PREFLIGHT-TOKEN: "
	idx := strings.Index(entry.Prompt, marker)
	if idx == -1 {
		t.Fatalf("extractInjectedPreflightToken: prompt has no %q marker: %q", marker, entry.Prompt)
	}
	rest := entry.Prompt[idx+len(marker):]
	end := strings.IndexAny(rest, "\r\n")
	if end == -1 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// preflightFixtureGoodSidecar is the REAL sidecar `ateam preflight`'s own
// live checkpoint run wrote for its probe teammate — captured verbatim,
// 2026-08-06, from
// ~/.claude/projects/.../dd11802e-2876-4093-a53c-bb2b0128b8ed/subagents/.
// Fixtures for this file MUST come from a real `claude -p` session, never a
// hand-built dispatched-session (rich) shape: agent-teams-25s3.19/.20's
// finding was exactly that every prior fixture here was built to the rich
// eleven-key shape, which is not what this verb's only launch mode ever
// produces, and neither the tests nor a manual spot-check had ever seen a
// real one before that bead.
const preflightFixtureGoodSidecar = `{"agentType":"agent-teams-implementer","description":"Preflight connectivity probe","name":"preflight-probe","toolUseId":"toolu_015W4vBLBFe2zbiajpV5YJqG","spawnDepth":1}`

// preflightFixtureBadSidecar is the same real thin shape with agentType
// mutated to a wrong-but-plausible value. There is no "real" negative
// fixture to capture here: a bogus subagent_type is rejected before any
// spawn and writes NO sidecar at all (measured live, agent-teams-25s3.19),
// so the only way to exercise the FAIL branch is to pin the real shape and
// mutate the one field under test.
const preflightFixtureBadSidecar = `{"agentType":"general-purpose","description":"Preflight connectivity probe","name":"preflight-probe","toolUseId":"toolu_015W4vBLBFe2zbiajpV5YJqG","spawnDepth":1}`

// preflightFixtureIrrelevantSidecar is a REAL thin sidecar from a DIFFERENT
// named spawn in the same kind of session (peer-a, from the peer-messaging
// keystone experiment, agent-teams-25s3.19) — captured verbatim from
// ~/.claude/projects/.../d2d12d3c-3412-48b2-a66e-409eeadf8c31/subagents/.
// Used to prove scanTeammateSidecarsForSession's name filter actually
// discriminates rather than matching every sidecar in the session.
const preflightFixtureIrrelevantSidecar = `{"agentType":"general-purpose","description":"peer-a sender","name":"peer-a","toolUseId":"toolu_019L4mhKJcSSr2js4U9iyf9o","spawnDepth":1}`

// preflightFixturePayload is a minimal --agents payload carrying the probed
// role. Tests that exercise the LAUNCH path must supply one: the verb now
// asserts pre-launch that the payload contains the role the probe will
// request, so an empty "{}" — which no real roles/ directory can produce —
// correctly fails that check and never reaches the code under test.
const preflightFixturePayload = `{"agent-teams-implementer":{"description":"impl","prompt":"body","model":"sonnet"}}`

// ── preflightSidecarChecks: the ACCEPTANCE fixture pair ─────────────────────
//
// NARROWED by agent-teams-25s3.24 (RETIRE UNPINNED) from the earlier
// agent-teams-25s3.19/.20 amendment's four checks to two: role-model-
// attached and spawn-permission-mode, which were unconditionally UNPINNED
// once a sidecar was found under this verb's only launch mode (claude -p),
// are deleted rather than merely silenced. spawn-record-present and role-
// type-registered are the two remaining real per-fixture predicates.

func TestPreflightSidecarChecks_GoodFixture_SpawnRecordAndRoleTypePass(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	path := putSpawnCheckSidecar(t, root, "proj", "sessGood", "preflight-probe", preflightFixtureGoodSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sessGood", false)
	if len(checks) != 2 {
		t.Fatalf("len(checks) = %d, want 2", len(checks))
	}
	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}

	for _, id := range []string{checkSpawnRecordPresent, checkRoleTypeRegistered} {
		c := byID[id]
		if c.Status != preflightPass {
			t.Errorf("%s: status = %s, want PASS (detail: %s)", id, c.Status, c.Detail)
		}
		if c.Remediation != "" {
			t.Errorf("%s: PASS carries non-empty remediation %q", id, c.Remediation)
		}
	}
	if checks[0].Witness != path {
		t.Errorf("spawn-record-present witness = %q, want sidecar path %q", checks[0].Witness, path)
	}
}

func TestPreflightSidecarChecks_BadFixture_RoleTypeFailsWithRemediation(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sessBad", "preflight-probe", preflightFixtureBadSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sessBad", false)
	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}

	if got := byID[checkSpawnRecordPresent].Status; got != preflightPass {
		t.Errorf("spawn-record-present = %s, want PASS (the sidecar landed under its expected name, just with the wrong type)", got)
	}
	roleType := byID[checkRoleTypeRegistered]
	if roleType.Status != preflightFail {
		t.Errorf("role-type-registered = %s, want FAIL", roleType.Status)
	}
	if roleType.Remediation == "" {
		t.Error("role-type-registered FAIL carries empty remediation — a FAIL with no remediation is the exact failure mode this initiative exists to prevent")
	}
}

// TestPreflightSidecarChecks_IgnoresOtherNamedSidecarsInSameSession proves
// the rewritten name-based match actually discriminates: a session
// containing another real named spawn (not the probe) must not be counted,
// and must not satisfy spawn-record-present on its own.
func TestPreflightSidecarChecks_IgnoresOtherNamedSidecarsInSameSession(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sessMixed", "peer-a", preflightFixtureIrrelevantSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}
	checks := preflightSidecarChecks(scan, noopSleep, "sessMixed", false)
	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}
	if got := byID[checkSpawnRecordPresent].Status; got != preflightFail {
		t.Errorf("spawn-record-present = %s, want FAIL — the only sidecar present is peer-a, not preflight-probe", got)
	}

	// Now add the real probe sidecar alongside peer-a's and confirm exactly
	// one match (the peer sidecar must not be double-counted or mistaken
	// for the probe's).
	putSpawnCheckSidecar(t, root, "proj", "sessMixed", "preflight-probe", preflightFixtureGoodSidecar)
	checks = preflightSidecarChecks(scan, noopSleep, "sessMixed", false)
	byID = map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}
	spawnRecord := byID[checkSpawnRecordPresent]
	if spawnRecord.Status != preflightPass {
		t.Errorf("spawn-record-present = %s, want PASS once the probe's own sidecar is present", spawnRecord.Status)
	}
	if !strings.Contains(spawnRecord.Detail, "observed 1") {
		t.Errorf("detail = %q, want it to report exactly 1 match despite peer-a also being present", spawnRecord.Detail)
	}
}

// ── preflightProbedRolePresent ──────────────────────────────────────────
//
// RENAMED AND SIMPLIFIED from preflightExpectedProbeModel by agent-teams-
// 25s3.24 (RETIRE UNPINNED): the earlier version also returned the role's
// declared model, read by role-model-attached's now-deleted precondition
// gate (preflightRoleModelCheck, also deleted along with preflightModelFamily/
// preflightModelFamilies/preflightProbeSessionModel). This is now a pure
// presence check, still load-bearing for the pre-launch roles-payload-builds
// guard: it must keep FAILing when the probed role is missing from the
// resolved roles directory (verified end to end by
// TestPreflightKong_ProbedRoleMissing_FailsRolesPayloadBuilds below).
func TestPreflightProbedRolePresent_PayloadPresenceCheck(t *testing.T) {
	payload := `{"agent-teams-implementer":{"description":"d","prompt":"p","model":"sonnet"},"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}`
	if err := preflightProbedRolePresent(payload); err != nil {
		t.Fatalf("preflightProbedRolePresent: %v, want nil (the probed role is present)", err)
	}

	if err := preflightProbedRolePresent(`{"agent-teams-planner":{"model":"opus"}}`); err == nil {
		t.Error("want an error when the payload has no agent-teams-implementer entry")
	}
}

// TestPreflightKong_ProbedRoleMissing_FailsRolesPayloadBuilds is the live-
// negative-control regression guard agent-teams-25s3.24 requires: with the
// probed role absent from a built --agents payload, Run() must still FAIL
// roles-payload-builds and never reach launch — the property this bead's
// spec calls "the caller's behaviour must not change" even after
// preflightExpectedProbeModel was renamed/simplified to
// preflightProbedRolePresent.
func TestPreflightKong_ProbedRoleMissing_FailsRolesPayloadBuilds(t *testing.T) {
	ctx, stdout, _ := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) {
			return `{"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}`, nil
		},
		launch: func(string, string, string, string, string) (string, error) {
			t.Fatal("launch must not be called: the probed role is missing from the payload")
			return "", nil
		},
		scanSidecars: func(string) ([]spawnCheckSidecarWithPath, error) {
			t.Fatal("scanSidecars must not be called")
			return nil, nil
		},
		sleep: noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("ExitCode = %d, want 1 — a FAIL was emitted, so a verdict WAS formed", code)
	}
	out := stdout.String()
	if !strings.Contains(out, checkRolesPayloadBuilds) || !strings.Contains(out, preflightFail) {
		t.Errorf("stdout = %q, want roles-payload-builds FAIL", out)
	}
}

func TestPreflightSidecarChecks_NoSidecar_SpawnRecordFailsAndRestSkip(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID) // never written
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sess-missing", false)
	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}

	spawnRecord := byID[checkSpawnRecordPresent]
	if spawnRecord.Status != preflightFail {
		t.Errorf("spawn-record-present = %s, want FAIL — zero findings when N are expected must never pass", spawnRecord.Status)
	}
	if spawnRecord.Remediation == "" {
		t.Error("spawn-record-present FAIL carries empty remediation")
	}
	if !strings.Contains(spawnRecord.Detail, "expected 1") {
		t.Errorf("detail = %q, want it to report observed vs expected", spawnRecord.Detail)
	}
	for _, id := range []string{checkRoleTypeRegistered} {
		if got := byID[id].Status; got != preflightSkip {
			t.Errorf("%s: status = %s, want SKIP (no sidecar to inspect)", id, got)
		}
	}

	// One root cause must produce exactly ONE red. The per-check assertions
	// above are individually correct but collectively blind: a second check
	// that also FAILs on this branch leaves every one of them green while
	// the report degrades from naming a cause to shouting four of them.
	// Verified by mutation — appending an extra FAIL to this branch alone
	// left the whole internal/verbs suite green without this count.
	fails := 0
	for _, c := range checks {
		if c.Status == preflightFail {
			fails++
		}
	}
	if fails != 1 {
		t.Errorf("FAIL count = %d, want exactly 1 — one root cause must yield one red and the rest SKIP", fails)
	}
}

// ── pollPreflightSidecars ────────────────────────────────────────────────

func TestPollPreflightSidecars_ReturnsEarlyOnceExpectMet(t *testing.T) {
	calls := 0
	scan := func(string) ([]spawnCheckSidecarWithPath, error) {
		calls++
		if calls < 3 {
			return nil, nil
		}
		return []spawnCheckSidecarWithPath{{Path: "found"}}, nil
	}
	got := pollPreflightSidecars(scan, noopSleep, "sess", 1, 10*time.Second, 250*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want exactly 3 (stop as soon as expect is met)", calls)
	}
}

func TestPollPreflightSidecars_ExhaustsDeadlineAndReturnsLastRead(t *testing.T) {
	scan := func(string) ([]spawnCheckSidecarWithPath, error) {
		return nil, nil // never satisfies expect
	}
	got := pollPreflightSidecars(scan, noopSleep, "sess", 1, 10*time.Second, 250*time.Millisecond)
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

// ── preflightLaunchArgs: the --plugin-dir pre-merge verification seam ────
//
// team-lead's finding: skills resolve from the main checkout's marketplace
// directory, not from a feature branch, so before this branch merges the
// probe session cannot reach /agent-teams:preflight through the ordinary
// path at all. --plugin-dir is a hidden, test-only escape hatch for that —
// the ABSENT case matters most, since a stray --plugin-dir in a normal run
// would silently change which plugin tree (and skill) the probe loads.

// TestPreflightLaunchArgs_PluginDirAbsentWhenUnset is the case that matters:
// an empty pluginDir must leave the argv byte-identical to before this seam
// existed — no --plugin-dir anywhere in it.
func TestPreflightLaunchArgs_PluginDirAbsentWhenUnset(t *testing.T) {
	args := preflightLaunchArgs("sess-id", "{}", "/agent-teams:preflight", "", "")
	for _, a := range args {
		if a == "--plugin-dir" {
			t.Fatalf("args = %v, --plugin-dir must be absent when unset", args)
		}
	}
}

// TestPreflightLaunchArgs_PluginDirReachesArgvWhenSet proves the seam
// actually works when a caller does set it.
func TestPreflightLaunchArgs_PluginDirReachesArgvWhenSet(t *testing.T) {
	args := preflightLaunchArgs("sess-id", "{}", "/agent-teams:preflight", "", "/path/to/plugin")
	for i, a := range args {
		if a == "--plugin-dir" {
			if i+1 >= len(args) || args[i+1] != "/path/to/plugin" {
				t.Fatalf("args = %v, want --plugin-dir immediately followed by the path", args)
			}
			return
		}
	}
	t.Fatalf("args = %v, want --plugin-dir present when pluginDir is set", args)
}

// ── newPreflightSessionID ────────────────────────────────────────────────

func TestNewPreflightSessionID_LooksLikeUUIDv4AndIsUnique(t *testing.T) {
	a := newPreflightSessionID()
	b := newPreflightSessionID()
	if a == b {
		t.Fatalf("two calls produced the same id: %s", a)
	}
	for _, id := range []string{a, b} {
		parts := strings.Split(id, "-")
		if len(parts) != 5 {
			t.Fatalf("id %q has %d dash-separated parts, want 5", id, len(parts))
		}
		if parts[2][0] != '4' {
			t.Errorf("id %q: version nibble = %q, want '4'", id, string(parts[2][0]))
		}
		variant := parts[3][0]
		if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
			t.Errorf("id %q: variant nibble = %q, want one of 8/9/a/b", id, string(variant))
		}
	}
}

// ── the token probe: mint, inject, compare (agent-teams-25s3.4 step 3) ─────
//
// All comparisons are exercised against FAKE injected observed values — no
// test here execs a real claude binary or spawns a real teammate; the three
// live controls (positive/negative/cheat) are the DRI's to run separately.

// TestMintPreflightToken_FreshPerCall is the freshness requirement: a
// cached/reused token would let a probe that saw a PREVIOUS run's
// transcript replay the answer without the role ever attaching this run.
func TestMintPreflightToken_FreshPerCall(t *testing.T) {
	a := mintPreflightToken()
	b := mintPreflightToken()
	if a == "" || b == "" {
		t.Fatalf("mintPreflightToken returned empty: %q, %q", a, b)
	}
	if a == b {
		t.Fatalf("two calls produced the same token: %s — freshness is load-bearing", a)
	}
}

// TestInjectPreflightToken_ReachesImplementerPromptOnly proves injection
// lands in the PROBED ROLE'S prompt in the --agents payload, and nowhere
// else in the payload — a sibling role's prompt must be untouched.
func TestInjectPreflightToken_ReachesImplementerPromptOnly(t *testing.T) {
	agentsJSON := `{"agent-teams-implementer":{"description":"impl","prompt":"implementer body","model":"sonnet"},"agent-teams-tester":{"description":"test","prompt":"tester body","model":"sonnet"}}`
	token := "deadbeefcafef00d"

	got, err := injectPreflightToken(agentsJSON, token)
	if err != nil {
		t.Fatalf("injectPreflightToken: %v", err)
	}

	var payload map[string]agentDefinition
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	impl, ok := payload[preflightProbedRoleKey]
	if !ok {
		t.Fatalf("result payload has no %q entry", preflightProbedRoleKey)
	}
	if !strings.Contains(impl.Prompt, "PREFLIGHT-TOKEN: "+token) {
		t.Errorf("implementer prompt = %q, want it to carry PREFLIGHT-TOKEN: %s", impl.Prompt, token)
	}
	if !strings.HasPrefix(impl.Prompt, "implementer body") {
		t.Errorf("implementer prompt = %q, want the original body preserved before the appended section", impl.Prompt)
	}
	tester, ok := payload["agent-teams-tester"]
	if !ok {
		t.Fatalf("result payload lost the untouched %q entry", "agent-teams-tester")
	}
	if tester.Prompt != "tester body" || strings.Contains(tester.Prompt, token) {
		t.Errorf("tester prompt = %q, want it unmodified and never carrying the token", tester.Prompt)
	}
}

// TestInjectPreflightToken_MissingProbedRole_Errors is the defensive
// re-validation: injectPreflightToken must not silently no-op when the
// payload doesn't carry the role it's supposed to inject into.
func TestInjectPreflightToken_MissingProbedRole_Errors(t *testing.T) {
	_, err := injectPreflightToken(`{"agent-teams-tester":{"description":"test","prompt":"body"}}`, "sometoken")
	if err == nil {
		t.Fatal("injectPreflightToken: want an error when the payload has no probed-role entry")
	}
}

// TestCheckRoleProseInContext_ExactMatch_Passes is the healthy case: the
// probe reproduced the injected token precisely.
func TestCheckRoleProseInContext_ExactMatch_Passes(t *testing.T) {
	c := checkRoleProseInContext("6738a6028acc8c31", "6738a6028acc8c31")
	if c.Status != preflightPass {
		t.Errorf("Status = %s, want PASS", c.Status)
	}
	if c.Check != checkRoleProseInContextID {
		t.Errorf("Check = %s, want %s", c.Check, checkRoleProseInContextID)
	}
	if c.Detail != "token matched" {
		t.Errorf(`Detail = %q, want "token matched" (planner ruling 2026-08-06: PASS need not repeat the value)`, c.Detail)
	}
	if c.Remediation != "" {
		t.Errorf("Remediation = %q, want empty on PASS", c.Remediation)
	}
}

// TestCheckRoleProseInContext_NoToken_Fails is the negative-control shape: a
// probe with no role definition attached reports it has nothing, via the
// reserved sentinel (planner correction 2026-08-06: never empty).
func TestCheckRoleProseInContext_NoToken_Fails(t *testing.T) {
	c := checkRoleProseInContext(preflightNoTokenSentinel, "6738a6028acc8c31")
	if c.Status != preflightFail {
		t.Errorf("Status = %s, want FAIL", c.Status)
	}
	if c.Detail != `token absent — probe replied "NO-TOKEN"` {
		t.Errorf("Detail = %q, want the pinned NO-TOKEN phrasing naming the raw reply", c.Detail)
	}
	if c.Remediation == "" {
		t.Error("Remediation is empty, want a non-empty remediation on FAIL (contract artifact (3))")
	}
}

// TestCheckRoleProseInContext_ProbeNoAnswer_FailsWithDistinctRemediation
// covers the OTHER reserved sentinel (planner correction 2026-08-06): no
// answer was ever obtained at all, a different root cause (the probe/spawn
// machinery failing) from NO-TOKEN (the probe answered and had nothing).
// Collapsing the two loses "the install is broken" vs "the probe machinery
// is broken" — pinned here as a remediation that must differ, not just a
// status/Detail difference.
func TestCheckRoleProseInContext_ProbeNoAnswer_FailsWithDistinctRemediation(t *testing.T) {
	c := checkRoleProseInContext(preflightProbeNoAnswerSentinel, "6738a6028acc8c31")
	if c.Status != preflightFail {
		t.Errorf("Status = %s, want FAIL", c.Status)
	}
	if c.Detail != `no reply obtained — probe replied "PROBE-NO-ANSWER"` {
		t.Errorf("Detail = %q, want the pinned PROBE-NO-ANSWER phrasing naming the raw reply", c.Detail)
	}
	noToken := checkRoleProseInContext(preflightNoTokenSentinel, "6738a6028acc8c31")
	if c.Remediation == "" {
		t.Error("Remediation is empty, want a non-empty remediation on FAIL")
	}
	if c.Remediation == noToken.Remediation {
		t.Error("PROBE-NO-ANSWER and NO-TOKEN must carry DIFFERENT remediations — they are different root causes (probe machinery vs. dropped role definition), and an identical remediation collapses that distinction")
	}
}

// TestCheckRoleProseInContext_WrongValue_Fails is the cheat-control shape: a
// probe that read the role file instead of its assembled prompt reports a
// value that is present but wrong (e.g. "NOT-IN-FILE", or any other string
// that isn't the minted token, and isn't one of the two reserved sentinels).
func TestCheckRoleProseInContext_WrongValue_Fails(t *testing.T) {
	c := checkRoleProseInContext("NOT-IN-FILE", "6738a6028acc8c31")
	if c.Status != preflightFail {
		t.Errorf("Status = %s, want FAIL", c.Status)
	}
	if c.Detail != `token mismatch — probe replied "NOT-IN-FILE"` {
		t.Errorf("Detail = %q, want the pinned generic-mismatch phrasing naming the raw reply", c.Detail)
	}
}

// TestCheckRoleProseInContext_Empty_Fails covers the probe answering with
// nothing at all (declined, crashed before reporting, or the skill's own
// report field came back blank) — a CONTRACT VIOLATION under the
// never-empty payload shape (the skill should emit
// preflightProbeNoAnswerSentinel instead), but this must still fail safely
// rather than panicking or silently passing if the skill's contract is
// ever violated.
func TestCheckRoleProseInContext_Empty_Fails(t *testing.T) {
	c := checkRoleProseInContext("", "6738a6028acc8c31")
	if c.Status != preflightFail {
		t.Errorf("Status = %s, want FAIL", c.Status)
	}
	if c.Remediation == "" {
		t.Error("Remediation is empty, want a non-empty remediation on FAIL")
	}
}

// TestCheckRoleProseInContext_WitnessNamesRaisedBarNotUncheatable pins the
// FROZEN witness wording (agent-teams-25s3.4 step 3): state the raised bar,
// never claim the residual is closed. This is the fifth residual claimed in
// this initiative; the previous four were each declared closed and wrong.
func TestCheckRoleProseInContext_WitnessNamesRaisedBarNotUncheatable(t *testing.T) {
	for _, c := range []preflightCheck{
		checkRoleProseInContext("tok", "tok"),
		checkRoleProseInContext("wrong", "tok"),
	} {
		for _, forbidden := range []string{"uncheatable", "cannot be faked", "tamper-proof", "proves the definition attached and nothing else could"} {
			if strings.Contains(strings.ToLower(c.Witness), forbidden) || strings.Contains(strings.ToLower(c.Detail), forbidden) {
				t.Errorf("status %s: witness/detail contains forbidden overclaim %q: witness=%q detail=%q", c.Status, forbidden, c.Witness, c.Detail)
			}
		}
		if !strings.Contains(c.Witness, "argv") {
			t.Errorf("status %s: witness = %q, want it to name the argv residual (the token rides on --agents argv, readable mid-run by a disobedient probe with Bash)", c.Status, c.Witness)
		}
	}
}

// TestPreflightOverrideTokenCheck_ExactMatch_ReplacesWithPass proves the
// full merge: a fabricated skill verdict carrying the skill's raw report
// (Detail only, per the payload-shape contract — Status/Witness/Remediation
// are whatever the skill guessed, since it never knows token; Status is
// FAIL per the fail-closed placeholder rule) gets its role-prose-in-context
// entry fully replaced — ALL FOUR fields, not just Status/Witness — by the
// verb's own comparison. Detail is the one planner corrected: it is shipped
// text, not a transport slot, so the raw token must not survive verbatim
// into the final report.
func TestPreflightOverrideTokenCheck_ExactMatch_ReplacesWithPass(t *testing.T) {
	token := "abc123"
	checks := []preflightCheck{
		{Check: "role-types-available", Status: preflightPass},
		{Check: "teammate-spawns", Status: preflightPass},
		{Check: checkRoleProseInContextID, Status: preflightFail, Detail: token, Witness: "whatever the skill guessed", Remediation: "whatever the skill guessed"},
	}
	got := preflightOverrideTokenCheck(checks, token)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3 (override replaces in place, never adds/removes)", len(got))
	}
	for _, c := range got {
		if c.Check == checkRoleProseInContextID {
			if c.Status != preflightPass {
				t.Errorf("Status = %s, want PASS", c.Status)
			}
			if c.Detail != "token matched" {
				t.Errorf(`Detail = %q, want "token matched" — the skill's raw report (the bare token) must not survive into the shipped report`, c.Detail)
			}
			if c.Witness == "whatever the skill guessed" {
				t.Error("Witness was not overwritten — the skill's guess must never survive into the final verdict")
			}
			if c.Remediation != "" {
				t.Errorf("Remediation = %q, want empty on PASS — the skill's guessed remediation must not survive either", c.Remediation)
			}
			return
		}
	}
	t.Fatal("role-prose-in-context missing from the merged checks")
}

// TestPreflightOverrideTokenCheck_AbsentCheck_NoOp covers the legitimate
// standalone-stop case: role-prose-in-context never ran, so there is nothing
// to override, and the function must not fabricate an entry.
func TestPreflightOverrideTokenCheck_AbsentCheck_NoOp(t *testing.T) {
	checks := []preflightCheck{{Check: "role-types-available", Status: preflightFail}}
	got := preflightOverrideTokenCheck(checks, "abc123")
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (no fabricated entry)", len(got))
	}
	for _, c := range got {
		if c.Check == checkRoleProseInContextID {
			t.Fatal("preflightOverrideTokenCheck fabricated a role-prose-in-context entry that was never in the input")
		}
	}
}

// TestPreflightKong_Run_TokenNeverWrittenToAnyFile is the token-hygiene
// requirement 2 end to end through Run(): mint a real token via the real
// launch path (buildAgentsPayload + injectPreflightToken, not a hand-built
// fixture) and confirm it never lands on disk anywhere under this run's
// scratch $HOME (t.TempDir, via home/root below — so "no file" is
// checkable: nothing this process writes lands outside it). The token
// legitimately DOES appear in the rendered stdout verdict (Detail says what
// was observed, same style as role-model-attached's "model=%s, want %s") —
// that's the tool's one designed output, not a log/file/bead write, and the
// token is stated to the probe as "a disposable check value, not a secret".
// What must never happen is a SEPARATE, silent persistence of it.
func TestPreflightKong_Run_TokenNeverWrittenToAnyFile(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	var observedToken string
	ctx, _, _ := makePreflightCtx()
	c := &preflightKong{
		JSON:               true,
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch: func(sessionID, agentsJSON, _, _, _ string) (string, error) {
			observedToken = extractInjectedPreflightToken(t, agentsJSON)
			if !strings.Contains(agentsJSON, observedToken) {
				t.Fatalf("sanity: token %q not found in its own source payload", observedToken)
			}
			skillChecks := []preflightCheck{
				{Check: "role-types-available", Status: preflightPass},
				{Check: "teammate-spawns", Status: preflightPass},
				{Check: checkRoleProseInContextID, Status: preflightFail, Detail: observedToken}, // FAIL is the fail-closed placeholder; the verb overwrites it
			}
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return buildPreflightEnvelope(t, false, "success", buildPreflightVerdictJSON(t, skillChecks), 0.10), nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		sleep: noopSleep,
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if observedToken == "" {
		t.Fatal("launch was never given a tokenized payload")
	}
	filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(b), observedToken) {
			t.Errorf("file %s contains the token %q — the token must never reach disk via a code path this verb owns", path, observedToken)
		}
		return nil
	})
}

// ── parsePreflightVerdict: the skill-owned-checks completeness guard ──────
//
// team-lead's finding (contract addendum agent-teams-25s3.15 (A5), one level
// up from the empty-set case): a NON-empty verdict that silently dropped one
// of the skill's owned checks must still go red — "ran something, but not
// everything expected" is the same defect class as "ran nothing". The one
// legitimate exception (agent-teams-25s3.4 step 1) is standalone-stop mode:
// role-types-available FAILing alone, by design, before any spawn.

// TestParsePreflightVerdict_MissingOwnedCheck_Fails is the NEGATIVE control:
// role-types-available PASSes (so this is NOT standalone-stop mode) but
// role-prose-in-context never showed up. Must FAIL and name the missing id.
func TestParsePreflightVerdict_MissingOwnedCheck_Fails(t *testing.T) {
	verdictJSON := buildPreflightVerdictJSON(t, []preflightCheck{
		{Check: "role-types-available", Status: preflightPass, Detail: "agent-teams-implementer available"},
		{Check: "teammate-spawns", Status: preflightPass, Detail: "spawned preflight-probe"},
		// role-prose-in-context is missing.
	})
	_, err := parsePreflightVerdict(verdictJSON)
	if err == nil {
		t.Fatal("want an error for a verdict missing an owned check id")
	}
	if !strings.Contains(err.Error(), "role-prose-in-context") {
		t.Errorf("error = %q, want it to name the missing check id", err.Error())
	}
}

// TestParsePreflightVerdict_StandaloneStopAlone_DoesNotTripMissingCheck is
// the POSITIVE control: the real standalone-mode verdict (role-types-
// available FAILing alone, by design, before any spawn) must NOT be reported
// as missing checks.
func TestParsePreflightVerdict_StandaloneStopAlone_DoesNotTripMissingCheck(t *testing.T) {
	verdictJSON := buildPreflightVerdictJSON(t, []preflightCheck{
		{Check: "role-types-available", Status: preflightFail, Detail: "agent-teams-implementer not in this session's available agent types", Remediation: "run `ateam preflight` rather than invoking the skill directly"},
	})
	if _, err := parsePreflightVerdict(verdictJSON); err != nil {
		t.Fatalf("parsePreflightVerdict: %v, want nil — a standalone-stop verdict is complete, not an under-count", err)
	}
}

// TestParsePreflightVerdict_AllOwnedChecksPresent_Succeeds pins the ordinary
// full-verdict shape so the completeness guard doesn't regress the common
// case.
func TestParsePreflightVerdict_AllOwnedChecksPresent_Succeeds(t *testing.T) {
	verdictJSON := buildPreflightVerdictJSON(t, []preflightCheck{
		{Check: "role-types-available", Status: preflightPass},
		{Check: "teammate-spawns", Status: preflightPass},
		{Check: "role-prose-in-context", Status: preflightPass},
	})
	if _, err := parsePreflightVerdict(verdictJSON); err != nil {
		t.Fatalf("parsePreflightVerdict: %v, want nil", err)
	}
}

// ── preflightKong.Run: core paths ────────────────────────────────────────

func makePreflightCtx() (*cli.Context, *strings.Builder, *strings.Builder) {
	var stdout, stderr strings.Builder
	return &cli.Context{Stdout: &stdout, Stderr: &stderr}, &stdout, &stderr
}

// TestPreflightKong_MalformedRoleFile_Exit1WithFail and its sibling below pin
// the exit-code discriminator ruled on 2026-08-06 (agent-teams-25s3.2
// interpretation note): STATUSES AND THE EXIT CODE MUST AGREE. Exit 2 is an
// anti-false-green device for when the INSTRUMENT failed, so emitting a FAIL
// alongside it says "the install is broken" and "I could not form a
// judgement" in one breath.
//
// The discriminator is whether the condition could exist on a HEALTHY install:
// an unresolvable roles dir can (wrong cwd, unset $CLAUDE_PLUGIN_ROOT, binary
// run from a scratch path) -> environment -> no FAIL, all SKIP, exit 2. A
// malformed role file inside a directory that DID resolve cannot -> that is
// the install being broken -> FAIL, exit 1.
//
// This test formerly asserted exit 2 for the malformed case. That assertion
// was changed deliberately under the ruling above — not to make a failing test
// pass — and split in two so each class is pinned separately.
func TestPreflightKong_MalformedRoleFile_Exit1WithFail(t *testing.T) {
	ctx, stdout, _ := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return "", errors.New("frontmatter missing a non-empty description") },
		launch: func(string, string, string, string, string) (string, error) {
			t.Fatal("launch must not be called")
			return "", nil
		},
		scanSidecars: func(string) ([]spawnCheckSidecarWithPath, error) {
			t.Fatal("scanSidecars must not be called")
			return nil, nil
		},
		sleep: noopSleep,
	}
	err := c.Run(ctx)
	if err == nil {
		t.Fatal("Run: want non-nil error")
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("ExitCode = %d, want 1 — a FAIL was emitted, so a verdict WAS formed", code)
	}
	out := stdout.String()
	if !strings.Contains(out, checkRolesPayloadBuilds) || !strings.Contains(out, preflightFail) {
		t.Errorf("stdout = %q, want roles-payload-builds FAIL", out)
	}
	for _, id := range []string{checkProbeSessionVerdict, checkSpawnRecordPresent, checkRoleTypeRegistered} {
		if !strings.Contains(out, id) {
			t.Errorf("stdout missing skipped check %s", id)
		}
	}
	if !strings.Contains(out, "0 pass, 1 fail, 3 skip") {
		t.Errorf("stdout summary line missing/wrong: %q", out)
	}
}

func TestPreflightKong_LaunchError_Exit2(t *testing.T) {
	ctx, _, stderr := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch: func(string, string, string, string, string) (string, error) {
			return "", errors.New("'claude' not found in PATH")
		},
		sleep: noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "could not launch") {
		t.Errorf("stderr = %q, want a launch-failure message", stderr.String())
	}
}

func TestPreflightKong_BudgetAbort_Exit2NoFailMessageNamesCap(t *testing.T) {
	ctx, stdout, stderr := makePreflightCtx()
	envelope := buildPreflightEnvelope(t, true, preflightBudgetAbortSubtype, "", 4.99)
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch:             func(_, _, _, maxBudgetUSD, _ string) (string, error) { return envelope, nil },
		MaxBudgetUSD:       "5",
		sleep:              noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2 (a budget abort is a tool failure, not exit 1)", code)
	}
	if strings.Contains(stdout.String(), preflightFail) {
		t.Errorf("stdout = %q, budget abort must never produce a FAIL check", stdout.String())
	}
	if !strings.Contains(stderr.String(), "$5") {
		t.Errorf("stderr = %q, want it to name the cap", stderr.String())
	}
}

func TestPreflightKong_ProbeVerdictParseFailure_StillRunsSidecarChecksExit1(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	ctx, stdout, _ := makePreflightCtx()
	var mintedSession string
	envelope := buildPreflightEnvelope(t, false, "success", "not json at all, a stray prose reply", 0.10)
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch: func(sessionID, _, _, _, _ string) (string, error) {
			mintedSession = sessionID
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return envelope, nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		sleep: noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("ExitCode = %d, want 1", code)
	}
	out := stdout.String()
	if !strings.Contains(out, checkProbeSessionVerdict) {
		t.Fatalf("stdout missing probe-session-verdict: %q", out)
	}
	// The verdict FAIL must not swallow the two verb-owned sidecar checks —
	// they read the harness's own record and are independent of whether the
	// skill's JSON parsed.
	for _, id := range []string{checkSpawnRecordPresent, checkRoleTypeRegistered} {
		if !strings.Contains(out, id) {
			t.Errorf("stdout missing sidecar check %s even though the sidecar was healthy: %q", id, out)
		}
	}
	if mintedSession == "" {
		t.Fatal("launch was never called with a minted session id")
	}
}

// LOAD-BEARING, do not weaken. This test and TestPreflightKong_Run_TokenNeverWrittenToAnyFile
// are the two tests that exercise the override WIRING through Run(); the dedicated
// TestCheckRoleProseInContext_* tests cover the compare LOGIC directly and never
// touch Run(). Verified by mutation (agent-teams-25s3.3): bypassing
// preflightOverrideTokenCheck in Run() reddens exactly those two and leaves the
// compare tests green. So wiring coverage and compare coverage are complementary.
//
// WHAT MAKES THE WIRING DETECTABLE: the skill fixture ships a fail-closed
// placeholder Status: preflightFail. An unwired override lets that FAIL survive
// into the result, and the wantPass=7 / clean-exit assertions below then trip.
// The extraction (extractInjectedPreflightToken echoing the REAL minted token) is
// also required, but hardcoding a WRONG token there fails LOUDLY through the
// compare — it is not the fragile part. Protect the fail-closed placeholder and
// the count/exit assertions.
//
// This comment was wrong TWICE before this line — first aimed at the extraction
// (a loud failure, not the guard), then claimed this was the ONLY wiring test
// (it is one of two). Both errors were caught by re-running the bypass mutation,
// not by re-reading. If you edit this comment, re-run that mutation; proximity to
// the code makes a claim more likely READ, not more likely RIGHT.
func TestPreflightKong_HappyPath_JSONShapeAndCostFooter(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	ctx, stdout, stderr := makePreflightCtx()
	c := &preflightKong{
		JSON:               true,
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch: func(sessionID, agentsJSON, _, _, _ string) (string, error) {
			// The skill's role-prose-in-context entry carries the probe's raw
			// observed reply, verbatim, in Detail — here that's the REAL
			// minted token, so a healthy round trip PASSes. Status/Witness/
			// Remediation on this entry are whatever the skill would emit (it
			// doesn't know token, so it can't compute PASS/FAIL) and are
			// fully overwritten by preflightOverrideTokenCheck; FAIL is the
			// fail-closed placeholder the skill's contract requires (never
			// PASS/SKIP), so a broken override path ships loud red, not a
			// decorative green.
			token := extractInjectedPreflightToken(t, agentsJSON)
			skillChecks := []preflightCheck{
				{Check: "role-types-available", Status: preflightPass, Detail: "agent-teams-implementer available", Witness: "session agent type list"},
				{Check: "teammate-spawns", Status: preflightPass, Detail: "spawned preflight-probe", Witness: "live probe"},
				{Check: checkRoleProseInContextID, Status: preflightFail, Detail: token},
			}
			verdictJSON := buildPreflightVerdictJSON(t, skillChecks)
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return buildPreflightEnvelope(t, false, "success", verdictJSON, 0.37), nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		sleep: noopSleep,
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v, want a clean exit", err)
	}

	var result preflightResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("stdout is not the contract JSON shape: %v (stdout: %s)", err, stdout.String())
	}
	if result.Fail != 0 || result.Skip != 0 {
		t.Errorf("result = %+v, want fail=0 skip=0", result)
	}
	// role-model-attached and spawn-permission-mode are DELETED (agent-teams-
	// 25s3.24, RETIRE UNPINNED) — a healthy run now carries exactly seven
	// checks, all PASS: roles-payload-builds, probe-session-verdict, role-
	// types-available, teammate-spawns, role-prose-in-context (now a real
	// PASS via the token round trip), spawn-record-present, role-type-
	// registered.
	const wantPass = 7
	if result.Pass != wantPass {
		t.Errorf("pass = %d, want %d", result.Pass, wantPass)
	}
	if len(result.Checks) != wantPass {
		t.Errorf("len(checks) = %d, want %d (no UNPINNED rows survive)", len(result.Checks), wantPass)
	}
	if result.SessionID == "" {
		t.Error("session_id is empty")
	}
	if strings.Contains(stdout.String(), "unpinned") {
		t.Errorf("stdout = %q, the `unpinned` field must be gone from the contract shape (agent-teams-25s3.24)", stdout.String())
	}
	if strings.Contains(stdout.String(), "probe session cost") {
		t.Error("--json stdout must be pure contract-shape JSON — the cost footer belongs on stderr")
	}
	if !strings.Contains(stderr.String(), "$0.3700") {
		t.Errorf("stderr = %q, want the cost footer", stderr.String())
	}
	if !strings.Contains(stderr.String(), "What this cannot verify") {
		t.Errorf("stderr = %q, want the limits footer (agent-teams-25s3.24)", stderr.String())
	}
}

// TestPreflightLimitsNote_NamesLimitsWithoutNamingTransportVendor pins
// contract artifact (8): the limits note must name what it cannot verify
// (model, permission mode, the inbound transport leg) without naming the
// concrete human-message transport's medium or vendor.
func TestPreflightLimitsNote_NamesLimitsWithoutNamingTransportVendor(t *testing.T) {
	for _, want := range []string{"model", "permission mode", "transport", "inbound"} {
		if !strings.Contains(preflightLimitsNote, want) {
			t.Errorf("preflightLimitsNote missing %q: %s", want, preflightLimitsNote)
		}
	}
	for _, forbidden := range []string{"Slack", "email", "SMS", "iMessage"} {
		if strings.Contains(preflightLimitsNote, forbidden) {
			t.Errorf("preflightLimitsNote names a concrete transport medium/vendor (%q), forbidden by contract artifact (8): %s", forbidden, preflightLimitsNote)
		}
	}
	if (&preflightKong{}).Help() != preflightLimitsNote {
		t.Error("preflightKong.Help() must surface preflightLimitsNote verbatim so `ateam preflight --help` carries it")
	}
}

func TestPreflightKong_EnvelopeUnparseable_Exit2(t *testing.T) {
	ctx, _, stderr := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return preflightFixturePayload, nil },
		launch:             func(string, string, string, string, string) (string, error) { return "not json", nil },
		sleep:              noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "no parseable") {
		t.Errorf("stderr = %q, want a parse-failure message", stderr.String())
	}
}

// ── Validate ─────────────────────────────────────────────────────────────

func TestPreflightKong_Validate_MaxBudgetUSD(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"unset", "", false},
		{"valid", "12.50", false},
		{"zero", "0", true},
		{"negative", "-1", true},
		{"not a number", "lots", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &preflightKong{MaxBudgetUSD: tc.value}
			err := c.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// ── --help renders from kong tags (bead acceptance) ─────────────────────

func TestPreflightKong_Help_RendersFromKongTags(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterPreflightKong(p)
	_, parseErr := p.Parse([]string{"preflight", "--help"})
	// kong prints help and returns a *kong.ParseError{Value: kong.ErrorHelp}-
	// shaped sentinel-ish flow depending on version; the load-bearing
	// assertion is that registering the verb and parsing --help doesn't
	// panic or fail to resolve the flags defined via struct tags.
	if parseErr != nil && !strings.Contains(parseErr.Error(), "help") {
		t.Fatalf("Parse --help: unexpected error: %v", parseErr)
	}
}

// TestPreflightSidecarChecks_SkillDeclinedToSpawn_NoRedForACorrectRefusal pins
// the second defect the live negative control found (agent-teams-25s3.3,
// 2026-08-06): with roles/implementer.md deleted, the skill CORRECTLY refused
// to spawn per its Step 1, and the verb reported spawn-record-present FAIL
// accusing "the upstream teammate-spawn regression this initiative exists to
// catch" — which was not happening. Two reds for one root cause, one of them
// blaming an innocent party. An absent sidecar is the RIGHT outcome here.
func TestPreflightSidecarChecks_SkillDeclinedToSpawn_NoRedForACorrectRefusal(t *testing.T) {
	scan := func(string) ([]spawnCheckSidecarWithPath, error) {
		t.Fatal("scan must not run: the skill declined to spawn, so there is nothing to poll for")
		return nil, nil
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sess-declined", true)

	fails := 0
	for _, c := range checks {
		if c.Status == preflightFail {
			fails++
		}
		if c.Status != preflightSkip {
			t.Errorf("%s: status = %s, want SKIP — the skill's refusal to spawn is specified behaviour, not a fault", c.Check, c.Status)
		}
	}
	if fails != 0 {
		t.Errorf("FAIL count = %d, want 0 — role-types-available already carries the one red for this root cause", fails)
	}
}

// TestPreflightSkillDeclinedToSpawn_KeysOnStandaloneStop proves the keying
// itself discriminates, rather than the caller always passing false.
func TestPreflightSkillDeclinedToSpawn_KeysOnStandaloneStop(t *testing.T) {
	declined := []preflightCheck{{Check: preflightSkillStandaloneStopID, Status: preflightFail}}
	if !preflightSkillDeclinedToSpawn(declined) {
		t.Error("role-types-available FAIL must read as a declined spawn")
	}
	spawned := []preflightCheck{{Check: preflightSkillStandaloneStopID, Status: preflightPass}}
	if preflightSkillDeclinedToSpawn(spawned) {
		t.Error("role-types-available PASS must NOT read as a declined spawn — the sidecar checks have to run")
	}
	if preflightSkillDeclinedToSpawn(nil) {
		t.Error("absent role-types-available must not read as declined — a broken verdict must not suppress the sidecar checks")
	}
}

// TestPreflightKong_RolesDirUnresolvable_Exit2NoFail is the other half of the
// discriminator: the roles directory could not be located at all, which a
// healthy install can hit from the wrong cwd. The tool formed NO judgement, so
// it must emit no FAIL and exit 2 — the anti-false-green code doing the job it
// exists for.
func TestPreflightKong_RolesDirUnresolvable_Exit2NoFail(t *testing.T) {
	ctx, stdout, stderr := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) {
			return "", fmt.Errorf("resolve plugin roles dir: no roles/*.md found; tried: /nope: %w", errRolesDirUnresolvable)
		},
		launch: func(string, string, string, string, string) (string, error) {
			t.Fatal("launch must not be called")
			return "", nil
		},
		scanSidecars: func(string) ([]spawnCheckSidecarWithPath, error) {
			t.Fatal("scanSidecars must not be called")
			return nil, nil
		},
		sleep: noopSleep,
	}
	err := c.Run(ctx)
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2 — the tool could not run the check at all", code)
	}
	if strings.Contains(stdout.String(), preflightFail) {
		t.Errorf("stdout carries a FAIL: %q — exit 2 must never accompany a FAIL, or the report contradicts its own exit code", stdout.String())
	}
	if !strings.Contains(stderr.String(), "resolve plugin roles dir") {
		t.Errorf("stderr = %q, want it to name the unresolvable roles dir", stderr.String())
	}
}

// TestPreflightProbeSpawnNameMatchesSkill pins the ONE literal that lives in
// two artifacts: preflightProbeSpawnName here, and the spawn instruction in
// plugins/agent-teams/skills/preflight/SKILL.md. The verb finds the probe's
// harness record BY THAT NAME, so if the skill is renamed and this constant
// is not, the verb matches zero sidecars and reports spawn-record-present
// FAIL on a healthy install — a false red produced by a rename nobody would
// think to check, in the tool whose whole value is being believed when it
// goes red.
//
// A recognizer and its producer must share ONE literal; two hand-typed copies
// cannot witness their own divergence. This test is what makes the drift loud
// instead of silent. (The structurally better fix — the verb passing the name
// to the skill at launch — would remove the second copy entirely, but it
// changes the frozen launch argv and standalone mode has no verb to supply
// it; recorded on agent-teams-25s3.3 as the move if this ever bites twice.)
//
// Asserts the spawn-parameter FORM, not the bare name: a test that only
// checked the string appears somewhere would pass vacuously if the name
// survived in prose while the spawn instruction was renamed. The shutdown
// reference is pinned too — a rename that updates the spawn but not the
// shutdown leaves the probe running.
func TestPreflightProbeSpawnNameMatchesSkill(t *testing.T) {
	// Deliberately FATAL, never skip, when SKILL.md is absent. A test that
	// skips when its subject is missing silently stops checking, which is the
	// exact defect class this initiative exists to remove — shipping one here
	// would be indefensible.
	path := filepath.Join("..", "..", "plugins", "agent-teams", "skills", "preflight", "SKILL.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — this pin must FAIL, never skip, when its subject is missing", path, err)
	}
	skill := string(body)

	spawnParam := `name: "` + preflightProbeSpawnName + `"`
	if !strings.Contains(skill, spawnParam) {
		t.Errorf("SKILL.md carries no %s — the skill and preflightProbeSpawnName (%q) have diverged, so the verb will match zero sidecars and report a healthy install as broken",
			spawnParam, preflightProbeSpawnName)
	}
	// EXACT and DELIMITED on both sides. An unanchored Contains does not
	// discriminate: renaming the shutdown reference to `preflight-probe-x`
	// still contains "`preflight-probe" as a prefix, so a prefix-or-suffix
	// test matches the mutant and passes. Appending is how people rename
	// things, so that is the likeliest drift, not an exotic one.
	// (Demonstrated: with the prefix/suffix form, mutating ONLY line 76 left
	// this test green — the exact failure it exists to prevent.)
	// This stays independent of the spawn assertion above: SKILL.md's spawn
	// line wraps the whole `name: "preflight-probe"` expression in backticks,
	// so the bare name is not backtick-adjacent there and cannot satisfy this.
	if !strings.Contains(skill, "`"+preflightProbeSpawnName+"`") {
		t.Errorf("SKILL.md never references %q as a delimited bare name — the shutdown step must name the same probe, or a rename that updates the spawn but not the shutdown leaves the probe running", preflightProbeSpawnName)
	}
}
