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

// writePreflightFixtureFile writes content at root/rel, creating parent dirs.
func writePreflightFixtureFile(t *testing.T, root, rel, content string, perm os.FileMode) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// preflightFixtureRoleBody is a minimal valid role definition whose body
// carries the step-1 learnings self-fetch preflightCheckLearningsWired looks for.
func preflightFixtureRoleBody(role string) string {
	return "---\nname: " + role + "\ndescription: fixture " + role + "\nmodel: sonnet\n---\n\n" + role + " body\n1. run `ateam learnings " + role + "`\n"
}

// makePreflightPluginRoot builds a minimal plugin root under t.TempDir(): the
// five role definitions in agents/, a plugin manifest, a hooks file, and a
// bin/ directory. Tests drive the REAL preflightPreparePlugin against it, so
// the temp copy, the token section, and the cleanup are exercised for real
// without ever depending on the install the test binary runs from.
func makePreflightPluginRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, role := range preflightRoleFiles {
		writePreflightFixtureFile(t, root, "agents/"+role+".md", preflightFixtureRoleBody(role), 0o644)
	}
	writePreflightFixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"agent-teams"}`, 0o644)
	writePreflightFixtureFile(t, root, "hooks/hooks.json", `{"hooks":{}}`, 0o644)
	writePreflightFixtureFile(t, root, "bin/ateam", "#!/bin/sh\n", 0o755)
	return root
}

// preflightFixturePrepare returns a preparePlugin that builds the probe copy
// from a fresh fixture root, ignoring the requested root. Every kong test
// that reaches launch uses it so none depends on the real install.
func preflightFixturePrepare(t *testing.T) preflightPreparePluginFunc {
	t.Helper()
	root := makePreflightPluginRoot(t)
	return func(_, token string) (string, func(), error) {
		return preflightPreparePlugin(root, token)
	}
}

// extractInjectedPreflightToken reads the PREFLIGHT-TOKEN value out of the
// probed role's definition in the probe's plugin copy (the pluginDir argument
// launch fakes receive, since preflightKong.Run builds the copy before ever
// calling launch). Fixtures that need to fabricate a skill verdict echoing
// the REAL minted token (rather than a stale hardcoded guess) call this from
// inside their launch closure, while the copy still exists.
func extractInjectedPreflightToken(t *testing.T, pluginDir string) string {
	t.Helper()
	path := filepath.Join(pluginDir, "agents", preflightProbedRoleFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("extractInjectedPreflightToken: %v", err)
	}
	const marker = "PREFLIGHT-TOKEN: "
	idx := strings.Index(string(b), marker)
	if idx == -1 {
		t.Fatalf("extractInjectedPreflightToken: %s has no %q marker: %q", path, marker, b)
	}
	rest := string(b)[idx+len(marker):]
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
const preflightFixtureGoodSidecar = `{"agentType":"agent-teams:implementer","description":"Preflight connectivity probe","name":"preflight-probe","toolUseId":"toolu_015W4vBLBFe2zbiajpV5YJqG","spawnDepth":1}`

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

// ── role definitions: validation and the probed-role guard ──────────────

func TestPreflightValidateRoleFile(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		wantErr       string
	}{
		{"valid", preflightFixtureRoleBody("implementer"), ""},
		{"no opening delimiter", "description: x\n---\nbody\n", "opening frontmatter"},
		{"no closing delimiter", "---\ndescription: x\nbody\n", "closing frontmatter"},
		{"empty description", "---\ndescription: \"\"\n---\nbody\n", "description"},
		{"no description", "---\nmodel: sonnet\n---\nbody\n", "description"},
		{"empty body", "---\ndescription: x\n---\n\n\n", "body is empty"},
	} {
		err := preflightValidateRoleFile(tc.content)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v, want nil", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: err = %v, want it to contain %q", tc.name, err, tc.wantErr)
		}
	}
}

// TestPreflightValidateRoleDefinitions_ProbedRoleMissingFails proves the
// pre-launch guard: a directory whose files all parse but which lacks the
// probed role's definition must still fail, because the probe would request a
// role nobody can supply.
func TestPreflightValidateRoleDefinitions_ProbedRoleMissingFails(t *testing.T) {
	root := makePreflightPluginRoot(t)
	agents := filepath.Join(root, "agents")
	if err := preflightValidateRoleDefinitions(agents); err != nil {
		t.Fatalf("healthy fixture: %v, want nil", err)
	}
	if err := os.Remove(filepath.Join(agents, preflightProbedRoleFile)); err != nil {
		t.Fatal(err)
	}
	err := preflightValidateRoleDefinitions(agents)
	if err == nil || !strings.Contains(err.Error(), preflightProbedRoleFile) {
		t.Errorf("err = %v, want it to name the missing %s", err, preflightProbedRoleFile)
	}
}

// TestPreflightValidateRoleDefinitions_StrayFileFails pins that no .md in
// agents/ is skipped by name: Claude Code registers every one as an agent, so
// a frontmatter-less README there is a defect, not a non-role to ignore.
func TestPreflightValidateRoleDefinitions_StrayFileFails(t *testing.T) {
	root := makePreflightPluginRoot(t)
	writePreflightFixtureFile(t, root, "agents/README.md", "# not a role\n", 0o644)
	if err := preflightValidateRoleDefinitions(filepath.Join(root, "agents")); err == nil {
		t.Error("want an error for a frontmatter-less agents/README.md")
	}
}

// TestPreflightKong_ProbedRoleMissing_FailsRoleDefinitionsPresent is the live-
// negative-control regression guard agent-teams-25s3.24 requires: with the
// probed role's definition absent, Run() must FAIL role-definitions-present
// and never reach launch.
func TestPreflightKong_ProbedRoleMissing_FailsRoleDefinitionsPresent(t *testing.T) {
	ctx, stdout, _ := makePreflightCtx()
	root := makePreflightPluginRoot(t)
	if err := os.Remove(filepath.Join(root, "agents", preflightProbedRoleFile)); err != nil {
		t.Fatal(err)
	}
	c := &preflightKong{
		preparePlugin: preflightPreparePlugin,
		PluginDir:     root,
		launch: func(string, string, string, string) (string, error) {
			t.Fatal("launch must not be called: the probed role is missing")
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
	if !strings.Contains(out, checkRoleDefinitionsPresent) || !strings.Contains(out, preflightFail) {
		t.Errorf("stdout = %q, want role-definitions-present FAIL", out)
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

// ── preflightLaunchArgs: the plugin-copy probe launch ───────────────────

// preflightArgValue returns the argument following flag in args.
func preflightArgValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 >= len(args) {
				t.Fatalf("args = %v, %s has no value", args, flag)
			}
			return args[i+1]
		}
	}
	t.Fatalf("args = %v, want %s present", args, flag)
	return ""
}

// TestPreflightLaunchArgs_PluginCopyAndDisabledInstall proves the probe loads
// the temp copy AND switches the installed plugin off, and that no --agents
// payload rides on argv any more.
func TestPreflightLaunchArgs_PluginCopyAndDisabledInstall(t *testing.T) {
	settings := `{"enabledPlugins":{"agent-teams@agent-teams":false}}`
	args := preflightLaunchArgs("sess-id", "/agent-teams:preflight", "", "/tmp/copy", settings)
	if got := preflightArgValue(t, args, "--plugin-dir"); got != "/tmp/copy" {
		t.Errorf("--plugin-dir = %q, want the temp copy", got)
	}
	if got := preflightArgValue(t, args, "--settings"); got != settings {
		t.Errorf("--settings = %q, want %q", got, settings)
	}
	for _, a := range args {
		if a == "--agents" {
			t.Fatalf("args = %v, --agents must not be passed: role agents come from the plugin copy", args)
		}
	}
	if args[len(args)-1] != "/agent-teams:preflight" {
		t.Errorf("last arg = %q, want the skill", args[len(args)-1])
	}
}

func TestPreflightLaunchArgs_MaxBudgetOnlyWhenSet(t *testing.T) {
	for _, a := range preflightLaunchArgs("s", "/skill", "", "/tmp/copy", "{}") {
		if a == "--max-budget-usd" {
			t.Fatal("--max-budget-usd must be absent when unset")
		}
	}
	args := preflightLaunchArgs("s", "/skill", "1.5", "/tmp/copy", "{}")
	if got := preflightArgValue(t, args, "--max-budget-usd"); got != "1.5" {
		t.Errorf("--max-budget-usd = %q, want 1.5", got)
	}
}

// TestPreflightDisabledPluginsSettings covers marketplace-key discovery: the
// default key is always disabled, any other agent-teams@<marketplace> key in
// the user settings is added, and unrelated plugins (including the codex
// plugin, whose name only shares the prefix) are left alone.
func TestPreflightDisabledPluginsSettings(t *testing.T) {
	decode := func(s string) map[string]bool {
		var out struct {
			EnabledPlugins map[string]bool `json:"enabledPlugins"`
		}
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatalf("settings %q is not JSON: %v", s, err)
		}
		return out.EnabledPlugins
	}

	got := decode(preflightDisabledPluginsSettings(filepath.Join(t.TempDir(), "absent.json")))
	if len(got) != 1 || got["agent-teams@agent-teams"] {
		t.Errorf("no settings file: got %v, want only the default key set false", got)
	}

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"enabledPlugins":{"agent-teams@my-market":true,"agent-teams-codex@my-market":true,"other@x":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got = decode(preflightDisabledPluginsSettings(path))
	for _, key := range []string{"agent-teams@agent-teams", "agent-teams@my-market"} {
		if v, ok := got[key]; !ok || v {
			t.Errorf("key %q: got %v (present=%v), want present and false", key, v, ok)
		}
	}
	if len(got) != 2 {
		t.Errorf("got %v, want exactly the two agent-teams@ keys", got)
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

// TestPreflightPreparePlugin_TokenReachesImplementerCopyOnly proves the token
// lands in the probed role's definition in the TEMP COPY, nowhere else in the
// copy, and never in the source tree.
func TestPreflightPreparePlugin_TokenReachesImplementerCopyOnly(t *testing.T) {
	root := makePreflightPluginRoot(t)
	token := "deadbeefcafef00d"
	srcBefore, err := os.ReadFile(filepath.Join(root, "agents", "implementer.md"))
	if err != nil {
		t.Fatal(err)
	}

	dir, cleanup, err := preflightPreparePlugin(root, token)
	if err != nil {
		t.Fatalf("preflightPreparePlugin: %v", err)
	}
	defer cleanup()
	if dir == root {
		t.Fatal("probe dir is the source root; it must be a copy")
	}

	impl, err := os.ReadFile(filepath.Join(dir, "agents", "implementer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(impl), "PREFLIGHT-TOKEN: "+token) {
		t.Errorf("copy implementer.md = %q, want it to carry PREFLIGHT-TOKEN: %s", impl, token)
	}
	if !strings.HasPrefix(string(impl), string(srcBefore)) {
		t.Errorf("copy implementer.md must keep the original content before the appended section")
	}
	tester, err := os.ReadFile(filepath.Join(dir, "agents", "tester.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tester), token) {
		t.Error("tester.md in the copy carries the token; only the probed role may")
	}

	srcAfter, err := os.ReadFile(filepath.Join(root, "agents", "implementer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(srcAfter) != string(srcBefore) {
		t.Error("the source implementer.md changed; the install must never be modified")
	}
}

// TestPreflightPreparePlugin_CleanupRemovesCopy pins that the returned cleanup
// deletes the temp copy.
func TestPreflightPreparePlugin_CleanupRemovesCopy(t *testing.T) {
	dir, cleanup, err := preflightPreparePlugin(makePreflightPluginRoot(t), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("copy missing before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Stat after cleanup = %v, want not-exist", err)
	}
}

// TestPreflightPreparePlugin_InvalidRoleIsNotEnvironment covers the error
// discriminator: a malformed role file is an install defect (no
// errPreflightEnvironment), and no temp copy is left behind.
func TestPreflightPreparePlugin_InvalidRoleIsNotEnvironment(t *testing.T) {
	root := makePreflightPluginRoot(t)
	writePreflightFixtureFile(t, root, "agents/broken.md", "no frontmatter\n", 0o644)
	dir, cleanup, err := preflightPreparePlugin(root, "tok")
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatal("want an error for a malformed role file")
	}
	if errors.Is(err, errPreflightEnvironment) {
		t.Errorf("err = %v, a malformed role file is an install defect, not an environment error", err)
	}
	if dir != "" {
		if _, statErr := os.Stat(dir); statErr == nil {
			t.Errorf("temp dir %s still exists after a failed prepare", dir)
		}
	}
}

// TestPreflightPreparePlugin_UnresolvableRootIsEnvironment pins the other
// side of the discriminator.
func TestPreflightPreparePlugin_UnresolvableRootIsEnvironment(t *testing.T) {
	_, _, err := preflightPreparePlugin(filepath.Join(t.TempDir(), "nope"), "tok")
	if !errors.Is(err, errPreflightEnvironment) {
		t.Errorf("err = %v, want it to wrap errPreflightEnvironment", err)
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
			t.Errorf("status %s: witness = %q, want it to name the argv residual (the temp copy's path rides on the parent's argv, so a disobedient probe with Bash can read the copy mid-run)", c.Status, c.Witness)
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
// launch path (preflightPreparePlugin building a real temp copy, not a hand-built
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
	var plantedTok string
	ctx, _, _ := makePreflightCtx()
	c := &preflightKong{
		JSON:          true,
		preparePlugin: preflightFixturePrepare(t),
		launch: func(sessionID, _, _, pluginDir string) (string, error) {
			observedToken = extractInjectedPreflightToken(t, pluginDir)
			skillChecks := []preflightCheck{
				{Check: "role-types-available", Status: preflightPass},
				{Check: "teammate-spawns", Status: preflightPass},
				{Check: checkRoleProseInContextID, Status: preflightFail, Detail: observedToken}, // FAIL is the fail-closed placeholder; the verb overwrites it
				{Check: checkLearningsLoadedID, Status: preflightFail, Detail: plantedTok},
			}
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return buildPreflightEnvelope(t, false, "success", buildPreflightVerdictJSON(t, skillChecks), 0.10), nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		plantSentinel:  func(_, tok string) error { plantedTok = tok; return nil },
		forgetSentinel: func(string) error { return nil },
		learningsWired: func(string) preflightCheck {
			return preflightCheck{Check: checkLearningsWiredID, Status: preflightPass, Detail: "all role files wired"}
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
		{Check: "learnings-loaded", Status: preflightPass},
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
// an unresolvable plugin root can (wrong cwd, unset $CLAUDE_PLUGIN_ROOT, binary
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
		preparePlugin: func(string, string) (string, func(), error) {
			return "", nil, errors.New("frontmatter missing a non-empty description")
		},
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
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("ExitCode = %d, want 1 — a FAIL was emitted, so a verdict WAS formed", code)
	}
	out := stdout.String()
	if !strings.Contains(out, checkRoleDefinitionsPresent) || !strings.Contains(out, preflightFail) {
		t.Errorf("stdout = %q, want role-definitions-present FAIL", out)
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
		preparePlugin: preflightFixturePrepare(t),
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
		preparePlugin: preflightFixturePrepare(t),
		launch:        func(_, _, maxBudgetUSD, _ string) (string, error) { return envelope, nil },
		MaxBudgetUSD:  "5",
		sleep:         noopSleep,
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
		preparePlugin: preflightFixturePrepare(t),
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
	var plantedTok2 string
	home := t.TempDir()
	root := filepath.Join(home, ".claude", "projects")

	ctx, stdout, stderr := makePreflightCtx()
	c := &preflightKong{
		JSON:          true,
		preparePlugin: preflightFixturePrepare(t),
		launch: func(sessionID, _, _, pluginDir string) (string, error) {
			// The skill's role-prose-in-context entry carries the probe's raw
			// observed reply, verbatim, in Detail — here that's the REAL
			// minted token, so a healthy round trip PASSes. Status/Witness/
			// Remediation on this entry are whatever the skill would emit (it
			// doesn't know token, so it can't compute PASS/FAIL) and are
			// fully overwritten by preflightOverrideTokenCheck; FAIL is the
			// fail-closed placeholder the skill's contract requires (never
			// PASS/SKIP), so a broken override path ships loud red, not a
			// decorative green.
			token := extractInjectedPreflightToken(t, pluginDir)
			skillChecks := []preflightCheck{
				{Check: "role-types-available", Status: preflightPass, Detail: "agent-teams-implementer available", Witness: "session agent type list"},
				{Check: "teammate-spawns", Status: preflightPass, Detail: "spawned preflight-probe", Witness: "live probe"},
				{Check: checkRoleProseInContextID, Status: preflightFail, Detail: token},
				// learnings-loaded carries the header the probe reported from its
				// in-session `ateam learnings implementer`; the verb cross-checks
				// its entry count (5) against the learnings func below, which
				// reports the same 5 — a healthy match PASSes.
				{Check: checkLearningsLoadedID, Status: preflightFail, Detail: plantedTok2},
			}
			verdictJSON := buildPreflightVerdictJSON(t, skillChecks)
			putSpawnCheckSidecar(t, root, "proj", sessionID, "preflight-probe", preflightFixtureGoodSidecar)
			return buildPreflightEnvelope(t, false, "success", verdictJSON, 0.37), nil
		},
		scanSidecars: func(sessionID string) ([]spawnCheckSidecarWithPath, error) {
			return scanTeammateSidecarsForSession(root, sessionID)
		},
		plantSentinel:  func(_, tok string) error { plantedTok2 = tok; return nil },
		forgetSentinel: func(string) error { return nil },
		learningsWired: func(string) preflightCheck {
			return preflightCheck{Check: checkLearningsWiredID, Status: preflightPass, Detail: "all role files wired"}
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
	// A healthy run now carries exactly EIGHT checks, all PASS: role-definitions-
	// present, probe-session-verdict, role-types-available, teammate-spawns,
	// role-prose-in-context (real PASS via the token round trip), learnings-
	// loaded (real PASS via the count cross-check), spawn-record-present, role-
	// type-registered. (role-model-attached and spawn-permission-mode remain
	// DELETED — agent-teams-25s3.24; learnings-loaded added by the memories
	// check.)
	const wantPass = 9
	if result.Pass != wantPass {
		t.Errorf("pass = %d, want %d", result.Pass, wantPass)
	}
	if len(result.Checks) != wantPass {
		t.Errorf("len(checks) = %d, want %d", len(result.Checks), wantPass)
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
		preparePlugin: preflightFixturePrepare(t),
		launch:        func(string, string, string, string) (string, error) { return "not json", nil },
		sleep:         noopSleep,
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
// 2026-08-06): with agents/implementer.md deleted, the skill CORRECTLY refused
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

// TestPreflightKong_PluginRootUnresolvable_Exit2NoFail is the other half of
// the discriminator: the plugin root could not be located or copied at all,
// which a healthy install can hit from the wrong cwd. The tool formed NO
// judgement, so it must emit no FAIL and exit 2 — the anti-false-green code
// doing the job it exists for.
func TestPreflightKong_PluginRootUnresolvable_Exit2NoFail(t *testing.T) {
	ctx, stdout, stderr := makePreflightCtx()
	c := &preflightKong{
		preparePlugin: func(string, string) (string, func(), error) {
			return "", nil, fmt.Errorf("resolve plugin root: no agents/ found; tried: /nope: %w", errPreflightEnvironment)
		},
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
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("ExitCode = %d, want 2 — the tool could not run the check at all", code)
	}
	if strings.Contains(stdout.String(), preflightFail) {
		t.Errorf("stdout carries a FAIL: %q — exit 2 must never accompany a FAIL, or the report contradicts its own exit code", stdout.String())
	}
	if !strings.Contains(stderr.String(), "resolve plugin root") {
		t.Errorf("stderr = %q, want it to name the unresolvable plugin root", stderr.String())
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

// TestPreflightStartProgress_WritesToStderrNotStdout pins the progress fix
// (a live run printed nothing for ~1 min and looked hung, 2026-08-10): the
// launch notice goes to STDERR so --json stdout stays pure, and the whole
// thing is a no-op under --json.
func TestPreflightStartProgress_WritesToStderrNotStdout(t *testing.T) {
	var stdout, stderr strings.Builder
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}

	stop := preflightStartProgress(ctx, false, "sess-abc")
	stop() // stops the heartbeat immediately; the launch notice already printed

	if stdout.Len() != 0 {
		t.Errorf("progress wrote to STDOUT %q — must be stderr-only so --json stays pure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "sess-abc") {
		t.Errorf("stderr = %q, want the launch notice naming the session id", stderr.String())
	}
	if !strings.Contains(stderr.String(), "spawns a real teammate") {
		t.Errorf("stderr = %q, want it to say what is happening", stderr.String())
	}
}

// TestPreflightStartProgress_JSONIsNoOp: under --json, nothing is printed on
// either stream — machine consumers get exactly the JSON on stdout.
func TestPreflightStartProgress_JSONIsNoOp(t *testing.T) {
	var stdout, stderr strings.Builder
	ctx := &cli.Context{Stdout: &stdout, Stderr: &stderr}

	stop := preflightStartProgress(ctx, true, "sess-abc")
	stop()

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("under --json, progress must be silent: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// ── learnings-loaded (memories primed) + learnings-wired ────────────────────

func TestCheckLearningsLoadedResult(t *testing.T) {
	const tok = "abc123"
	for _, tc := range []struct {
		name     string
		probe    string
		token    string
		plantErr error
		want     string
	}{
		{"echoed token passes", tok, tok, nil, preflightPass},
		{"echoed with whitespace passes", "  abc123\n", tok, nil, preflightPass},
		{"wrong value fails", "def456", tok, nil, preflightFail},
		{"no-answer sentinel fails", preflightProbeNoAnswerSentinel, tok, nil, preflightFail},
		{"plant error fails", tok, tok, errors.New("learn boom"), preflightFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkLearningsLoadedResult(tc.probe, tc.token, tc.plantErr)
			if got.Status != tc.want {
				t.Errorf("status = %s, want %s (detail: %s)", got.Status, tc.want, got.Detail)
			}
			if got.Status == preflightFail && got.Remediation == "" {
				t.Error("a FAIL learnings-loaded must carry a remediation")
			}
		})
	}
}

// TestPreflightOverrideLearningsCheck_TokenRoundTrip proves the verb compares
// the probe's report against the planted token — a probe that did NOT echo the
// exact token FAILs, so this witnesses a real load rather than the probe's word.
func TestPreflightOverrideLearningsCheck_TokenRoundTrip(t *testing.T) {
	checks := []preflightCheck{{Check: checkLearningsLoadedID, Status: preflightFail, Detail: "the-real-token"}}
	if out := preflightOverrideLearningsCheck(checks, "the-real-token", nil); out[0].Status != preflightPass {
		t.Errorf("matching token = %s, want PASS", out[0].Status)
	}
	checks = []preflightCheck{{Check: checkLearningsLoadedID, Status: preflightFail, Detail: "something-else"}}
	if out := preflightOverrideLearningsCheck(checks, "the-real-token", nil); out[0].Status != preflightFail {
		t.Errorf("non-matching token = %s, want FAIL — a probe that didn't load the sentinel must not pass", out[0].Status)
	}
}

func TestPreflightOverrideLearningsCheck_AbsentIsNoOp(t *testing.T) {
	checks := []preflightCheck{{Check: "role-types-available", Status: preflightFail}}
	out := preflightOverrideLearningsCheck(checks, "tok", nil)
	if len(out) != 1 || out[0].Check != "role-types-available" {
		t.Errorf("no-op altered the checks: %+v", out)
	}
}

// TestPreflightCheckLearningsWired proves the per-role wiring check: PASS when
// every role file carries its `ateam learnings <role>` fetch, FAIL naming any
// that drops it.
func TestPreflightCheckLearningsWired(t *testing.T) {
	dir := t.TempDir()
	for _, r := range preflightRoleFiles {
		if err := os.WriteFile(filepath.Join(dir, r+".md"), []byte("step 1: run `ateam learnings "+r+"` first\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := preflightCheckLearningsWired(dir); got.Status != preflightPass {
		t.Errorf("all wired = %s, want PASS (%s)", got.Status, got.Detail)
	}
	// drop tester's fetch
	if err := os.WriteFile(filepath.Join(dir, "tester.md"), []byte("no fetch here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := preflightCheckLearningsWired(dir)
	if got.Status != preflightFail {
		t.Fatalf("missing fetch = %s, want FAIL", got.Status)
	}
	if !strings.Contains(got.Detail, "tester") {
		t.Errorf("detail %q should name the role that dropped its fetch", got.Detail)
	}
}
