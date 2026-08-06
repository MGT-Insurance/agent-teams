// preflight_test.go: core-path tests for `ateam preflight`
// (agent-teams-25s3.3). No test execs a real claude binary — launch and
// sidecar reads are always injected fakes/fixtures, per the bead's
// acceptance criterion. Edge cases and E2E/live verification are the
// tester's lane, not this file's.
package verbs

import (
	"encoding/json"
	"errors"
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

const preflightFixtureGoodSidecar = `{"agentType":"preflight-probe","name":"preflight-probe","spawnDepth":0,"model":"sonnet","taskKind":"in_process_teammate","teamName":"session-abc","customAgentType":"agent-teams-implementer","permissionMode":"bypassPermissions"}`

// preflightFakeAgentsJSON is a minimal --agents payload fixture carrying the
// probed role's expected model, for tests that exercise the full Run() flow
// through role-model-attached (which reads this via
// preflightExpectedProbeModel rather than a hardcoded literal).
const preflightFakeAgentsJSON = `{"agent-teams-implementer":{"description":"d","prompt":"p","model":"sonnet"}}`

const preflightFixtureBadSidecar = `{"agentType":"preflight-probe","name":"preflight-probe","spawnDepth":0,"model":"opus","taskKind":"in_process_teammate","teamName":"session-abc","permissionMode":"default"}`

// ── preflightSidecarChecks: the ACCEPTANCE fixture pair ─────────────────────

func TestPreflightSidecarChecks_GoodFixture_AllFourPass(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	path := putSpawnCheckSidecar(t, root, "proj", "sessGood", "preflight-probe", preflightFixtureGoodSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sessGood", "sonnet", nil)
	if len(checks) != 4 {
		t.Fatalf("len(checks) = %d, want 4", len(checks))
	}
	for _, c := range checks {
		if c.Status != preflightPass {
			t.Errorf("check %s: status = %s, want PASS (detail: %s)", c.Check, c.Status, c.Detail)
		}
		if c.Remediation != "" {
			t.Errorf("check %s: PASS carries non-empty remediation %q", c.Check, c.Remediation)
		}
	}
	if checks[0].Witness != path {
		t.Errorf("spawn-record-present witness = %q, want sidecar path %q", checks[0].Witness, path)
	}
}

func TestPreflightSidecarChecks_BadFixture_AllThreeFieldChecksFailWithRemediation(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sessBad", "preflight-probe", preflightFixtureBadSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sessBad", "sonnet", nil)
	if len(checks) != 4 {
		t.Fatalf("len(checks) = %d, want 4", len(checks))
	}

	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}

	if got := byID[checkSpawnRecordPresent].Status; got != preflightPass {
		t.Errorf("spawn-record-present = %s, want PASS (the sidecar landed, just with bad fields)", got)
	}
	for _, id := range []string{checkRoleDefinitionAttached, checkRoleModelAttached, checkSpawnPermissionMode} {
		c := byID[id]
		if c.Status != preflightFail {
			t.Errorf("%s: status = %s, want FAIL", id, c.Status)
		}
		if c.Remediation == "" {
			t.Errorf("%s: FAIL carries empty remediation — a FAIL with no remediation is the exact failure mode this initiative exists to prevent", id)
		}
	}
}

// TestPreflightSidecarChecks_ConcreteModelID_SameFamilyPasses pins the
// 2026-08-06 AMENDMENT: a resolved concrete id (e.g. "claude-sonnet-5")
// must PASS against an expected alias of "sonnet" — the census showed 22 of
// 288 teammate sidecars already carry a concrete id instead of the alias.
func TestPreflightSidecarChecks_ConcreteModelID_SameFamilyPasses(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	fixture := `{"agentType":"preflight-probe","name":"preflight-probe","spawnDepth":0,"model":"claude-sonnet-5","taskKind":"in_process_teammate","teamName":"session-abc","customAgentType":"agent-teams-implementer","permissionMode":"bypassPermissions"}`
	putSpawnCheckSidecar(t, root, "proj", "sessConcrete", "preflight-probe", fixture)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}
	checks := preflightSidecarChecks(scan, noopSleep, "sessConcrete", "sonnet", nil)

	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}
	got := byID[checkRoleModelAttached]
	if got.Status != preflightPass {
		t.Errorf("role-model-attached = %s, want PASS for a same-family concrete id (detail: %s)", got.Status, got.Detail)
	}
}

// ── preflightRoleModelCheck: the false-green precondition gate ─────────────
//
// planner-at-6oxi's second-pass finding on the 2026-08-06 amendment: the
// first fix (runtime-read expected model, family matching) was correct but
// left the "must differ from the probe session's own model" precondition as
// a doc comment, not code. If roles/implementer.md's model ever becomes
// "opus" (the probe session's own model), an UNATTACHED spawn silently
// inherits "opus" too — sc.Model == expectedModel would PASS for the wrong
// reason. Both fixtures below are the regression guard; per agent-teams-
// 25s3.15 (A1), both directions must be demonstrated, not just documented.

// TestPreflightRoleModelCheck_SameFamilyAsProbeSession_Unpinned is the FALSE
// GREEN guard: an expected model sharing the probe session's own family
// must report UNPINNED, never PASS, however the sidecar's model reads.
func TestPreflightRoleModelCheck_SameFamilyAsProbeSession_Unpinned(t *testing.T) {
	sc := spawnCheckSidecarWithPath{Path: "sidecar.json", spawnCheckSidecar: spawnCheckSidecar{Model: preflightProbeSessionModel}}
	got := preflightRoleModelCheck(sc, preflightProbeSessionModel, nil)
	if got.Status != preflightUnpinned {
		t.Fatalf("status = %s, want UNPINNED — the property is not witnessable when the role's expected model matches the probe session's own", got.Status)
	}
}

// TestPreflightRoleModelCheck_NoExpectedModel_Unpinned is the FALSE RED
// guard: a role declaring no discriminating model (frontmatter absent or
// "inherit") is a property this check cannot witness, not a broken install
// — UNPINNED, never FAIL.
func TestPreflightRoleModelCheck_NoExpectedModel_Unpinned(t *testing.T) {
	sc := spawnCheckSidecarWithPath{Path: "sidecar.json"} // Model absent too
	got := preflightRoleModelCheck(sc, "", nil)
	if got.Status != preflightUnpinned {
		t.Fatalf("status = %s, want UNPINNED, not FAIL", got.Status)
	}
}

// TestPreflightRoleModelCheck_AbsentModel_FailsWhenExpectationDiscriminates
// proves the gate doesn't swallow a REAL absence: when the expected model
// actually differs from the probe session's own, a missing sidecar model
// field is still a hard FAIL (never observed on a teammate, 0/288 in the
// census).
func TestPreflightRoleModelCheck_AbsentModel_FailsWhenExpectationDiscriminates(t *testing.T) {
	sc := spawnCheckSidecarWithPath{Path: "sidecar.json"} // Model absent
	got := preflightRoleModelCheck(sc, "sonnet", nil)
	if got.Status != preflightFail {
		t.Fatalf("status = %s, want FAIL", got.Status)
	}
	if got.Remediation == "" {
		t.Error("FAIL carries empty remediation")
	}
}

// TestPreflightSidecarChecks_ExpectedModelErr_FailsWithoutHidingOtherChecks
// proves that a failure to determine the expected model degrades ONLY
// role-model-attached, not the other three sidecar checks.
func TestPreflightSidecarChecks_ExpectedModelErr_FailsWithoutHidingOtherChecks(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")
	putSpawnCheckSidecar(t, root, "proj", "sessErr", "preflight-probe", preflightFixtureGoodSidecar)

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID)
	}
	checks := preflightSidecarChecks(scan, noopSleep, "sessErr", "", errors.New("--agents payload has no \"agent-teams-implementer\" entry"))

	byID := map[string]preflightCheck{}
	for _, c := range checks {
		byID[c.Check] = c
	}
	if got := byID[checkRoleModelAttached].Status; got != preflightFail {
		t.Errorf("role-model-attached = %s, want FAIL", got)
	}
	if got := byID[checkSpawnRecordPresent].Status; got != preflightPass {
		t.Errorf("spawn-record-present = %s, want PASS (unaffected by the model-lookup error)", got)
	}
	if got := byID[checkRoleDefinitionAttached].Status; got != preflightPass {
		t.Errorf("role-definition-attached = %s, want PASS (unaffected by the model-lookup error)", got)
	}
}

func TestPreflightModelFamily(t *testing.T) {
	cases := map[string]string{
		"sonnet":          "sonnet",
		"claude-sonnet-5": "sonnet",
		"opus":            "opus",
		"claude-opus-4-8": "opus",
		"haiku":           "haiku",
		"":                "",
		"gpt-4":           "",
	}
	for in, want := range cases {
		if got := preflightModelFamily(in); got != want {
			t.Errorf("preflightModelFamily(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPreflightExpectedProbeModel_ReadsFromAgentsPayload(t *testing.T) {
	payload := `{"agent-teams-implementer":{"description":"d","prompt":"p","model":"sonnet"},"agent-teams-planner":{"description":"d","prompt":"p","model":"opus"}}`
	got, err := preflightExpectedProbeModel(payload)
	if err != nil {
		t.Fatalf("preflightExpectedProbeModel: %v", err)
	}
	if got != "sonnet" {
		t.Errorf("got %q, want %q", got, "sonnet")
	}

	if _, err := preflightExpectedProbeModel(`{"agent-teams-planner":{"model":"opus"}}`); err == nil {
		t.Error("want an error when the payload has no agent-teams-implementer entry")
	}
}

func TestPreflightSidecarChecks_NoSidecar_SpawnRecordFailsAndRestSkip(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	scan := func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
		return scanTeammateSidecarsForSession(root, sessionID) // never written
	}

	checks := preflightSidecarChecks(scan, noopSleep, "sess-missing", "sonnet", nil)
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
	for _, id := range []string{checkRoleDefinitionAttached, checkRoleModelAttached, checkSpawnPermissionMode} {
		if got := byID[id].Status; got != preflightSkip {
			t.Errorf("%s: status = %s, want SKIP (no sidecar to inspect)", id, got)
		}
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

func TestPreflightKong_RolesPayloadBuildFailure_Exit2AllSkip(t *testing.T) {
	ctx, stdout, _ := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return "", errors.New("frontmatter missing a non-empty description") },
		launch: func(string, string, string, string) (string, error) {
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
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2", code)
	}
	out := stdout.String()
	if !strings.Contains(out, checkRolesPayloadBuilds) || !strings.Contains(out, preflightFail) {
		t.Errorf("stdout = %q, want roles-payload-builds FAIL", out)
	}
	for _, id := range []string{checkProbeSessionVerdict, checkSpawnRecordPresent, checkRoleDefinitionAttached, checkRoleModelAttached, checkSpawnPermissionMode} {
		if !strings.Contains(out, id) {
			t.Errorf("stdout missing skipped check %s", id)
		}
	}
	if !strings.Contains(out, "0 pass, 1 fail, 5 skip, 0 unpinned") {
		t.Errorf("stdout summary line missing/wrong: %q", out)
	}
}

func TestPreflightKong_LaunchError_Exit2(t *testing.T) {
	ctx, _, stderr := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return "{}", nil },
		launch: func(string, string, string, string) (string, error) {
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
		buildAgentsPayload: func() (string, error) { return "{}", nil },
		launch:             func(_, _, _, maxBudgetUSD string) (string, error) { return envelope, nil },
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
		buildAgentsPayload: func() (string, error) { return preflightFakeAgentsJSON, nil },
		launch: func(sessionID, _, _, _ string) (string, error) {
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
	// The verdict FAIL must not swallow the four verb-owned sidecar checks —
	// they read the harness's own record and are independent of whether the
	// skill's JSON parsed.
	for _, id := range []string{checkSpawnRecordPresent, checkRoleDefinitionAttached, checkRoleModelAttached, checkSpawnPermissionMode} {
		if !strings.Contains(out, id) {
			t.Errorf("stdout missing sidecar check %s even though the sidecar was healthy: %q", id, out)
		}
	}
	if mintedSession == "" {
		t.Fatal("launch was never called with a minted session id")
	}
}

func TestPreflightKong_HappyPath_JSONShapeAndCostFooter(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	skillChecks := []preflightCheck{
		{Check: "role-types-available", Status: preflightPass, Detail: "agent-teams-implementer available", Witness: "session agent type list"},
		{Check: "teammate-spawns", Status: preflightPass, Detail: "spawned preflight-probe", Witness: "live probe"},
		{Check: "role-prose-in-context", Status: preflightPass, Detail: "answered from role instructions", Witness: "live probe (weak)"},
	}
	verdictJSON := buildPreflightVerdictJSON(t, skillChecks)
	envelope := buildPreflightEnvelope(t, false, "success", verdictJSON, 0.37)

	ctx, stdout, stderr := makePreflightCtx()
	c := &preflightKong{
		JSON:               true,
		buildAgentsPayload: func() (string, error) { return preflightFakeAgentsJSON, nil },
		launch: func(sessionID, _, _, _ string) (string, error) {
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return envelope, nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		sleep: noopSleep,
	}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v, want nil (all ten checks should pass)", err)
	}

	var result preflightResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("stdout is not the contract JSON shape: %v (stdout: %s)", err, stdout.String())
	}
	if result.Fail != 0 || result.Skip != 0 {
		t.Errorf("result = %+v, want fail=0 skip=0", result)
	}
	wantChecks := len(skillChecks) + 1 /* roles-payload-builds */ + 1 /* probe-session-verdict */ + 4 /* sidecar checks */
	if result.Pass != wantChecks {
		t.Errorf("pass = %d, want %d", result.Pass, wantChecks)
	}
	if result.SessionID == "" {
		t.Error("session_id is empty")
	}
	if strings.Contains(stdout.String(), "probe session cost") {
		t.Error("--json stdout must be pure contract-shape JSON — the cost footer belongs on stderr")
	}
	if !strings.Contains(stderr.String(), "$0.3700") {
		t.Errorf("stderr = %q, want the cost footer", stderr.String())
	}
}

func TestPreflightKong_EnvelopeUnparseable_Exit2(t *testing.T) {
	ctx, _, stderr := makePreflightCtx()
	c := &preflightKong{
		buildAgentsPayload: func() (string, error) { return "{}", nil },
		launch:             func(string, string, string, string) (string, error) { return "not json", nil },
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
