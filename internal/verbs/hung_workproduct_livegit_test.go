package verbs

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// ── Tester-authored (agent-teams-sgr5.3): real-git / real-bd fidelity for the
// D1/D2/D3/D6/D8/D9 work-product path ───────────────────────────────────────
//
// The implementer's own tests (hung_workproduct_test.go) exercise
// computeWorkProductClock, the ladder functions, and scanHung's wiring with
// hungGitProbeFunc/hungProjectClaimedBeadFunc FAKED (swapped package-level
// vars). That is the right unit-test granularity, but it never runs the real
// `git` subprocess (defaultGitWorkProductProbe) against a real repo, nor the
// real `bd` claimed-bead subprocess (defaultProjectHasClaimedBead) against a
// real project .beads DB.
//
// These tests fill that gap with REAL git + REAL bd, anchored to actual
// wall-clock time rather than artificially backdated file mtimes.
//
// IMPORTANT DISCOVERY (see the tester's report / discovery bead): naively
// backdating a repo's commit/index timestamps via GIT_COMMITTER_DATE +
// os.Chtimes does NOT reliably stick -- `git status --porcelain` (the very
// first subprocess defaultGitWorkProductProbe runs, used as its availability
// check) opportunistically REWRITES the index file's on-disk mtime to the
// REAL current wall-clock time whenever the index's cached stat data doesn't
// already agree with the working tree, clobbering any artificial backdate on
// first probe. This is real, observed git behavior (verified empirically:
// repeated probes on an untouched repo stabilize after 1-2 calls, but any
// attempt to force the index mtime backward via os.Chtimes gets silently
// re-clobbered to real-now on the very next `git status`).
//
// The reliable technique used below instead: create the real repo/commit at
// the ACTUAL current wall-clock time (never backdated), then advance the
// `now()` PARAMETER passed into scanHung/doHungTick forward from that real
// anchor -- exactly mirroring how a real flatline is observed in production
// (ticks moving forward in real time relative to a real last-change), with
// zero fighting against git's own index-refresh semantics.
func mustGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "tester@example.com")
	run("config", "user.name", "Tester")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(dir+"/f.txt", []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "seed")
}

// mustClaimedBeadRepo runs real `bd init`/`bd create`/`bd update --claim`
// against dir's own .beads DB, so defaultProjectHasClaimedBead(dir) -- the
// REAL implementation, not a fake -- observes a genuine claimed in_progress
// bead exactly as it would for a real implementer's project repo. bd doesn't
// require dir to be a git repo.
func mustClaimedBeadRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		// bd init specifically requires an actual cwd change (cmd.Dir), not
		// "-C dir" -- it refuses "-C" with "no beads project found" before
		// the project exists. Using cmd.Dir uniformly here keeps every call
		// in this helper consistent.
		cmd := exec.Command("bd", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd %v (dir=%s): %v\n%s", args, dir, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	createOut := run("create", "scratch claimed work", "-t", "task", "--json")
	id := extractBDID(t, createOut)
	run("update", id, "--claim")
}

// extractBDID pulls the "id" field out of `bd create --json`'s output
// (pretty-printed, not one-line) without pulling in the full bd.Issue struct
// just for this helper.
func extractBDID(t *testing.T, jsonOut string) string {
	t.Helper()
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &obj); err != nil || obj.ID == "" {
		t.Fatalf("no id field in bd create output: %s (err=%v)", jsonOut, err)
	}
	return obj.ID
}

// TestLiveGit_WorkProductFlatline_RealArtifactsBoundaryAndLadder drives
// scanHung/doHungTick with REAL git + REAL bd against a real, untouched repo,
// confirming the 30m/1h D6 boundary and the wake/wake/alert ladder (Scenario
// 1 of tripwire-scenarios.md) fire at the exact documented thresholds against
// real artifacts, not just faked probe results. The repo's real commit/index
// state is the anchor; `now()` is advanced forward from it (see the package
// comment above for why this is more reliable than backdating).
func TestLiveGit_WorkProductFlatline_RealArtifactsBoundaryAndLadder(t *testing.T) {
	primary := t.TempDir()
	track := t.TempDir()

	mustGitRepo(t, primary)
	mustGitRepo(t, track) // untouched from here on -- no freshness advantage over primary
	mustClaimedBeadRepo(t, primary)

	t0 := time.Now().UTC()

	issues := []bd.Issue{{
		ID:          "at-live1",
		Title:       "live-git flatline",
		Description: "worktree: " + primary + "\ntrack-worktree: " + track + "\nmode: bg\n",
		Status:      "open",
		Labels:      []string{"at-live1", "thread:999"},
		UpdatedAt:   t0.Format(time.RFC3339), // pinned to the real anchor -- must not dominate as "now" once we advance the clock
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	agentsFunc := func() ([]agentSession, error) {
		return []agentSession{{CWD: primary, Status: "busy", PID: &pid, SessionID: "no-such-session"}}, nil
	}

	// ── 29 minutes flat: below D2's 30m threshold -- must NOT be trip-eligible.
	out, err := scanHung(ctx, agentsFunc, fixedNow(t0.Add(29*time.Minute)), false)
	if err != nil {
		t.Fatalf("scan @29m: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("@29m: classification = %q, want WORKING", out[0].Classification)
	}
	if out[0].WorkProductTripEligible {
		t.Error("@29m: expected NOT trip-eligible (below 30m threshold)")
	}
	// Generous tolerance: real git/bd subprocess setup overhead (a few
	// seconds) sits between t0's capture and the artifacts' true creation.
	if got, want := out[0].WorkProductFlatSeconds, int64(29*60); got < want-30 || got > want+5 {
		t.Errorf("@29m: wp_flat_seconds = %d, want ~%d (+/- setup overhead)", got, want)
	}

	// ── 31 minutes flat: past the threshold, real claimed bead present, no
	// gate, no real transcript file (defaultTranscriptTail's missing-file
	// path) -- must be trip-eligible and not downgraded.
	out, err = scanHung(ctx, agentsFunc, fixedNow(t0.Add(31*time.Minute)), false)
	if err != nil {
		t.Fatalf("scan @31m: %v", err)
	}
	if !out[0].WorkProductTripEligible {
		t.Fatal("@31m: expected trip-eligible against a real repo + real claimed bead")
	}
	if out[0].WorkProductDowngraded {
		t.Error("@31m: expected no downgrade (no real transcript file exists to corroborate recent work)")
	}
	if out[0].FailureTokensFound {
		t.Error("@31m: expected no failure tokens (no transcript file at all)")
	}

	// ── Ladder: wake @31m, wake @40m, none @46m (2 wakes already spent, still
	// under 1h), alert @61m -- driven through the REAL doHungTick, persisting
	// real anchor state across calls in ctx.Home, stubbing only wakeSend/topicPost.
	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "999"}
	deps := hungTickDeps{agentsFunc: agentsFunc, wakeSend: wake.send, topicPost: defaultHungTopicPost, transport: ft}

	deps.now = fixedNow(t0.Add(31 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @31m: %v", err)
	}
	if len(wake.bodies) != 1 {
		t.Fatalf("@31m tick: got %d wake sends, want 1 (first wake)", len(wake.bodies))
	}
	if !strings.Contains(wake.bodies[0], "at-live1") || !strings.Contains(wake.bodies[0], "flat work product") {
		t.Errorf("@31m wake body missing expected content: %q", wake.bodies[0])
	}

	deps.now = fixedNow(t0.Add(40 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @40m: %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Fatalf("@40m tick: got %d cumulative wake sends, want 2 (second wake)", len(wake.bodies))
	}

	deps.now = fixedNow(t0.Add(46 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @46m: %v", err)
	}
	// Regression guard for agent-teams-sgr5.7: the spec/ladder contract says
	// the 3rd tick past 30m (WakeAttempts already at the cap) sends NO
	// further wake. Before the fix, WorkProductLastProgressAt's
	// whole-second-precision persistence compared against a
	// freshly-recomputed sub-second-precision `lastProgress` made every
	// unchanged tick look like "real progress happened," wiping
	// WorkProductWakeAttempts/AlertedAt back to zero each time and firing a
	// 3rd wake here. Now asserted as a hard failure.
	if len(wake.bodies) != 2 {
		t.Fatalf("@46m tick: got %d cumulative wake sends, want 2 (ladder should be exhausted, not yet past 1h)", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Fatalf("@46m tick: got %d direct alerts, want 0 (not yet past 1h)", len(ft.calls))
	}

	deps.now = fixedNow(t0.Add(61 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @61m: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("@61m tick: got %d direct alerts, want 1 (past 1h direct-alert threshold)", len(ft.calls))
	}
	if !strings.Contains(ft.calls[0].Body, "at-live1") {
		t.Errorf("@61m alert body missing initiative id: %q", ft.calls[0].Body)
	}

	// ── D8: the journal must have a real, append-only record of this episode.
	journalData, err := os.ReadFile(hungJournalPath(StewardHome(ctx)))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	journal := string(journalData)
	if !strings.Contains(journal, `"ladder":"workproduct"`) {
		t.Errorf("journal missing workproduct ladder entries: %q", journal)
	}
	if !strings.Contains(journal, `"ladder_action":"alert"`) {
		t.Errorf("journal missing the final alert action: %q", journal)
	}
}

// TestLiveGit_WorkProductFlatline_TrackWorktreeUnionKeepsClockFresh proves D9
// end-to-end: a stale primary "worktree:" (a plain claimed-bead-only project
// dir, no fresh git activity ever) unioned with a REAL, just-committed
// "track-worktree:" repo (D9's union) must have the combined work-product
// clock reflect the FRESH track-worktree, not the stale primary -- real
// evidence that discoverWorktrees' union, not just the primary path, feeds
// computeWorkProductClock. The primary's own "staleness" is a controlled fake
// (hungGitProbeFunc dispatches to a fixed old timestamp for exactly that one
// path); the track-worktree's freshness is 100% real git, freshly committed
// moments before the assertions run.
func TestLiveGit_WorkProductFlatline_TrackWorktreeUnionKeepsClockFresh(t *testing.T) {
	primary := t.TempDir() // no git needed -- its probe is faked stale below
	track := t.TempDir()

	mustClaimedBeadRepo(t, primary)
	mustGitRepo(t, track) // real, fresh commit -- this IS "now"

	trackRealNow := time.Now().UTC()
	staleAnchor := trackRealNow.Add(-2 * time.Hour) // arbitrarily old; primary never had a real commit to backdate

	origProbe := hungGitProbeFunc
	defer func() { hungGitProbeFunc = origProbe }()
	hungGitProbeFunc = func(worktree string) gitProbeResult {
		if worktree == primary {
			return gitProbeResult{Available: true, StatusHash: "stale-forever", IndexMtime: staleAnchor, CommitTime: staleAnchor}
		}
		return defaultGitWorkProductProbe(worktree) // REAL probe for the track-worktree
	}

	issues := []bd.Issue{{
		ID:          "at-live2",
		Title:       "live-git union freshness",
		Description: "worktree: " + primary + "\ntrack-worktree: " + track + "\nmode: bg\n",
		Status:      "open",
		UpdatedAt:   staleAnchor.Format(time.RFC3339), // must not itself supply freshness
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	agentsFunc := func() ([]agentSession, error) {
		return []agentSession{{CWD: primary, Status: "busy", PID: &pid}}, nil
	}

	// At trackRealNow+9m: the primary's FAKE anchor is already ~2h+9m stale
	// (would be enormously over any threshold alone), but the union must
	// pick up the track-worktree's REAL, recent commit as the freshest
	// signal -- flat should be ~9 minutes, not ~2 hours, and NOT trip-eligible.
	out, err := scanHung(ctx, agentsFunc, fixedNow(trackRealNow.Add(9*time.Minute)), false)
	if err != nil {
		t.Fatalf("scan @+9m: %v", err)
	}
	if got, want := out[0].WorkProductFlatSeconds, int64(9*60); got < want-30 || got > want+5 {
		t.Errorf("wp_flat_seconds = %d, want ~%d -- union should reflect the fresh track-worktree, not the ~2h-stale primary", got, want)
	}
	if out[0].WorkProductTripEligible {
		t.Error("expected NOT trip-eligible: the union clock is only ~9m flat thanks to the fresh track-worktree")
	}

	// Push past 30 minutes relative to the track-worktree's REAL commit --
	// the union clock must now trip, proving the threshold is evaluated
	// against the union's freshest member, not a stale primary alone (which
	// would have tripped ages ago) nor frozen at the primary's fake time.
	out, err = scanHung(ctx, agentsFunc, fixedNow(trackRealNow.Add(31*time.Minute)), false)
	if err != nil {
		t.Fatalf("scan @+31m: %v", err)
	}
	if !out[0].WorkProductTripEligible {
		t.Error("@+31m (31m past the track-worktree's real commit): expected trip-eligible")
	}
}

// TestLiveGit_WorkProductLadder_KnownBug_AlertRepeatsPastOneHour is a pinned
// regression guard for agent-teams-sgr5.7: once a work-product flatline
// crosses the 1h direct-alert threshold, a sub-second-precision-loss bug
// used to wipe WorkProductAlertedAt back to "" every tick (comparing a
// freshly-recomputed, full-precision lastProgress against its own
// whole-second-truncated persisted prior self), defeating the "alert
// exactly once per episode" guard in nextWorkProductLadderAction. Fixed by
// truncating lastProgress to second precision before the comparison in
// scanHung (internal/verbs/hung_scan.go).
func TestLiveGit_WorkProductLadder_KnownBug_AlertRepeatsPastOneHour(t *testing.T) {
	primary := t.TempDir()
	mustGitRepo(t, primary)
	mustClaimedBeadRepo(t, primary)
	t0 := time.Now().UTC()

	issues := []bd.Issue{{
		ID:          "at-live3",
		Title:       "live-git alert-dedup",
		Description: "worktree: " + primary + "\nmode: bg\n",
		Status:      "open",
		Labels:      []string{"at-live3", "thread:1"},
		UpdatedAt:   t0.Format(time.RFC3339),
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	agentsFunc := func() ([]agentSession, error) {
		return []agentSession{{CWD: primary, Status: "busy", PID: &pid, SessionID: "no-such-session"}}, nil
	}

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "1"}
	deps := hungTickDeps{agentsFunc: agentsFunc, wakeSend: wake.send, topicPost: defaultHungTopicPost, transport: ft}

	deps.now = fixedNow(t0.Add(61 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @61m: %v", err)
	}
	deps.now = fixedNow(t0.Add(66 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick @66m: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Errorf("got %d direct alerts across two ticks past 1h, want exactly 1 (alert must fire once per episode, not repeat every tick)", len(ft.calls))
	}
}
