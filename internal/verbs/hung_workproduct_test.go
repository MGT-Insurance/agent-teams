package verbs

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// ── computeWorkProductClock (pure) ────────────────────────────────────────────

func TestComputeWorkProductClock_MaxAcrossSignals(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	indexT := now.Add(-40 * time.Minute)
	commitT := now.Add(-10 * time.Minute) // newest git signal
	beadT := now.Add(-90 * time.Minute)

	probes := []gitProbeResult{
		{Available: true, IndexMtime: indexT, CommitTime: commitT, StatusHash: "h1"},
	}
	// Pre-seed prevHash to the SAME combined hash these probes produce, so
	// this call takes the "unchanged since last tick" branch and the hash
	// component contributes no timestamp of its own -- isolating this test
	// to the index/commit/bead max-selection logic alone.
	prevHash := combinedStatusHash(probes)
	lastProgress, _, _ := computeWorkProductClock(probes, beadT, prevHash, time.Time{}, now)
	if !lastProgress.Equal(commitT) {
		t.Errorf("lastProgress = %v, want the newest signal (commit time) %v", lastProgress, commitT)
	}
}

func TestComputeWorkProductClock_BeadUpdateNewerThanGit(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	beadT := now.Add(-5 * time.Minute) // newest signal

	probes := []gitProbeResult{{Available: true, IndexMtime: old, CommitTime: old, StatusHash: "h1"}}
	prevHash := combinedStatusHash(probes) // unchanged since last tick
	lastProgress, _, _ := computeWorkProductClock(probes, beadT, prevHash, old, now)
	if !lastProgress.Equal(beadT) {
		t.Errorf("lastProgress = %v, want bead updated_at %v (newest signal)", lastProgress, beadT)
	}
}

func TestComputeWorkProductClock_UnavailableProbeExcluded(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	probes := []gitProbeResult{
		{Available: false}, // e.g. a track-worktree that no longer exists
	}
	lastProgress, hash, _ := computeWorkProductClock(probes, time.Time{}, "", time.Time{}, now)
	if !lastProgress.IsZero() {
		t.Errorf("lastProgress = %v, want zero (no available signal at all)", lastProgress)
	}
	if hash != "" {
		t.Errorf("hash = %q, want empty when nothing was available to hash", hash)
	}
}

func TestComputeWorkProductClock_HashChangeTracksTimestampNotMtime(t *testing.T) {
	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	probes := func(status string) []gitProbeResult {
		return []gitProbeResult{{Available: true, StatusHash: status}}
	}

	// First-ever observation: no prior hash to compare against, so this
	// component deliberately contributes NOTHING (hashAt stays zero) rather
	// than vacuously stamping "now" -- we cannot tell fresh dirt from
	// long-stale dirt on a single observation. The hash is still persisted
	// for the next tick to compare against.
	lastProgress1, hash1, hashAt1 := computeWorkProductClock(probes("dirty-v1"), time.Time{}, "", time.Time{}, t0)
	if !lastProgress1.IsZero() {
		t.Fatalf("first observation lastProgress = %v, want zero (no comparable prior state yet)", lastProgress1)
	}
	if hash1 == "" || !hashAt1.IsZero() {
		t.Fatalf("first observation hash/hashAt = %q/%v, want non-empty hash and zero hashAt", hash1, hashAt1)
	}

	// Second tick, 20 minutes later, SAME status hash: still unchanged, so
	// hashAt stays zero (carried forward) -- no vacuous timestamp appears
	// just because we've now "seen" the hash twice.
	t1 := t0.Add(20 * time.Minute)
	lastProgress2, hash2, hashAt2 := computeWorkProductClock(probes("dirty-v1"), time.Time{}, hash1, hashAt1, t1)
	if !hashAt2.IsZero() {
		t.Errorf("hashAt on a still-unchanged hash = %v, want zero", hashAt2)
	}
	if !lastProgress2.IsZero() {
		t.Errorf("lastProgress on a still-unchanged hash = %v, want zero (still no comparable prior state)", lastProgress2)
	}
	if hash2 != hash1 {
		t.Errorf("hash changed even though StatusHash input was identical: %q vs %q", hash2, hash1)
	}

	// Third tick: the status hash actually changes (real uncommitted edit)
	// against a REAL prior hash -> this is a genuine detected change, so
	// hashAt/lastProgress must advance to t2.
	t2 := t1.Add(5 * time.Minute)
	lastProgress3, hash3, hashAt3 := computeWorkProductClock(probes("dirty-v2"), time.Time{}, hash2, hashAt2, t2)
	if !hashAt3.Equal(t2) || !lastProgress3.Equal(t2) {
		t.Errorf("hashAt/lastProgress after a real status change = %v/%v, want both %v", hashAt3, lastProgress3, t2)
	}
	if hash3 == hash2 {
		t.Error("expected the combined hash to change once StatusHash input changed")
	}

	// Fourth tick: unchanged again, now against the REAL hashAt established
	// in tick 3 -- this time hashAt/lastProgress correctly carry forward
	// (not zero), since we now have a genuine prior observation to diff
	// against.
	t3 := t2.Add(20 * time.Minute)
	lastProgress4, _, hashAt4 := computeWorkProductClock(probes("dirty-v2"), time.Time{}, hash3, hashAt3, t3)
	if !hashAt4.Equal(t2) || !lastProgress4.Equal(t2) {
		t.Errorf("hashAt/lastProgress on a genuinely-unchanged hash = %v/%v, want both carried forward as %v", hashAt4, lastProgress4, t2)
	}
}

// ── D1: git probe — index-path cache + timeout/failure degrade (review fix,
// agent-teams-sgr5.6) ──────────────────────────────────────────────────────────

// initGitWorktree creates a minimal real git repo at t.TempDir() with one
// commit, so the probe/resolver below exercise real git subprocesses rather
// than a fake.
func initGitWorktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-m", "initial")
	return dir
}

// resetGitIndexPathCache clears the package-level cache before/after a test
// so tests don't leak cached entries into each other (t.TempDir() paths are
// unique per test anyway, but this keeps the map from growing unbounded
// across the whole test binary run).
func resetGitIndexPathCache(t *testing.T) {
	t.Helper()
	gitIndexPathCacheMu.Lock()
	gitIndexPathCache = map[string]string{}
	gitIndexPathCacheMu.Unlock()
	t.Cleanup(func() {
		gitIndexPathCacheMu.Lock()
		gitIndexPathCache = map[string]string{}
		gitIndexPathCacheMu.Unlock()
	})
}

func TestResolveGitIndexPath_CacheHitAvoidsReinvocation(t *testing.T) {
	resetGitIndexPathCache(t)
	worktree := initGitWorktree(t)
	ctx := context.Background()

	first := resolveGitIndexPath(ctx, worktree)
	if first == "" {
		t.Fatalf("resolveGitIndexPath: expected a resolved index path on a real repo")
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("resolved index path does not exist: %v", err)
	}

	// Break git resolution for a fresh (uncached) call by pointing PATH at an
	// empty directory -- if the second call to the SAME worktree actually
	// re-invoked git instead of hitting the cache, it would now fail/return
	// "" since git can't be found.
	t.Setenv("PATH", t.TempDir())
	second := resolveGitIndexPath(ctx, worktree)
	if second != first {
		t.Errorf("resolveGitIndexPath with git unavailable = %q, want cached %q (cache hit should avoid re-invoking git)", second, first)
	}
}

func TestResolveGitIndexPath_StaleCacheEntryReResolved(t *testing.T) {
	resetGitIndexPathCache(t)
	worktree := initGitWorktree(t)
	ctx := context.Background()

	// Seed the cache with a path that doesn't exist on disk -- simulating a
	// stale entry (worktree moved/removed since the path was cached).
	gitIndexPathCacheMu.Lock()
	gitIndexPathCache[worktree] = filepath.Join(worktree, "definitely-not-the-real-index")
	gitIndexPathCacheMu.Unlock()

	got := resolveGitIndexPath(ctx, worktree)
	if got == "" {
		t.Fatalf("resolveGitIndexPath: expected re-resolution of a stale cache entry, got empty")
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("re-resolved index path does not exist: %v (got %q)", err, got)
	}
	if got == filepath.Join(worktree, "definitely-not-the-real-index") {
		t.Errorf("resolveGitIndexPath returned the stale cached path instead of re-resolving")
	}
}

func TestDefaultGitWorkProductProbe_NonRepoDegradesToNoSignal(t *testing.T) {
	resetGitIndexPathCache(t)
	// A plain non-git directory: `git status --porcelain` fails immediately,
	// so the whole probe must report Available=false (no fabricated
	// flatline, no fabricated fresh progress) rather than a partial result.
	result := defaultGitWorkProductProbe(t.TempDir())
	if result.Available {
		t.Errorf("defaultGitWorkProductProbe on a non-repo = %+v, want Available=false", result)
	}
	if !result.IndexMtime.IsZero() || !result.CommitTime.IsZero() || result.StatusHash != "" {
		t.Errorf("defaultGitWorkProductProbe on a non-repo should be zero-valued throughout, got %+v", result)
	}
}

func TestResolveGitIndexPath_TimeoutOrCancelDegradesToNoSignal(t *testing.T) {
	resetGitIndexPathCache(t)
	worktree := initGitWorktree(t)

	// An already-canceled context stands in for a timeout: exec.CommandContext
	// fails immediately, exercising the same failure path a real 5s timeout
	// would. resolveGitIndexPath must degrade silently to "" (no signal),
	// never fabricate a path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := resolveGitIndexPath(ctx, worktree)
	if got != "" {
		t.Errorf("resolveGitIndexPath with a canceled context = %q, want empty (degrade to no-signal)", got)
	}

	// The cache must not have been poisoned with an empty/bad entry: a
	// subsequent call with a live context still resolves correctly.
	live := resolveGitIndexPath(context.Background(), worktree)
	if live == "" {
		t.Errorf("resolveGitIndexPath after a prior canceled call = empty, want a real resolution once given a live context")
	}
}

// ── discoverWorktrees (D9) ─────────────────────────────────────────────────────

func TestDiscoverWorktrees_ExplicitTrackLinesUnioned(t *testing.T) {
	primary := "/wt/at-abc-dri"
	explicit := []string{"/wt/at-abc-impl1", "/wt/at-abc-impl2"}
	got := discoverWorktrees(primary, explicit, "at-abc", func(string) ([]string, error) {
		t.Fatal("listDir must not be called when explicit track-worktree lines exist")
		return nil, nil
	})
	want := []string{"/wt/at-abc-dri", "/wt/at-abc-impl1", "/wt/at-abc-impl2"}
	if strings.Join(got, ",") != strings.Join(sortedCopy(want), ",") {
		t.Errorf("discoverWorktrees = %v, want %v", got, want)
	}
}

func sortedCopy(ss []string) []string {
	out := append([]string(nil), ss...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestDiscoverWorktrees_FallbackBySubstringWhenNoExplicitLines(t *testing.T) {
	primary := "/root/at-xyz-dri"
	listDir := func(dir string) ([]string, error) {
		if dir != "/root" {
			t.Fatalf("listDir called with %q, want /root (primary's parent)", dir)
		}
		return []string{"at-xyz-dri", "at-xyz-impl-1", "unrelated-other-initiative"}, nil
	}
	got := discoverWorktrees(primary, nil, "at-xyz", listDir)
	want := []string{"/root/at-xyz-dri", "/root/at-xyz-impl-1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("discoverWorktrees fallback = %v, want %v", got, want)
	}
}

func TestDiscoverWorktrees_NoFallbackWhenExplicitLinesPresent(t *testing.T) {
	// Even if a sibling directory would match the id substring, the fallback
	// must not run once explicit track-worktree lines exist (D9: fallback is
	// for LEGACY sessions only).
	called := false
	listDir := func(string) ([]string, error) {
		called = true
		return []string{"at-xyz-impl-2"}, nil
	}
	got := discoverWorktrees("/root/at-xyz-dri", []string{"/root/at-xyz-impl-1"}, "at-xyz", listDir)
	if called {
		t.Error("fallback listDir must not run when explicit track-worktree lines are present")
	}
	want := []string{"/root/at-xyz-dri", "/root/at-xyz-impl-1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("discoverWorktrees = %v, want %v", got, want)
	}
}

// ── isContentTypedWorkTurn (D2/D7 transcript classification) ──────────────────

func TestIsContentTypedWorkTurn(t *testing.T) {
	tests := []struct {
		name string
		rec  transcriptRecord
		want bool
	}{
		{
			name: "assistant with content counts",
			rec:  mustRecord(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`),
			want: true,
		},
		{
			name: "user with content counts",
			rec:  mustRecord(t, `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`),
			want: true,
		},
		{
			name: "system framing does not count",
			rec:  mustRecord(t, `{"type":"system","message":{"content":[{"type":"text","text":"housekeeping"}]}}`),
			want: false,
		},
		{
			name: "queue-operation framing does not count",
			rec:  mustRecord(t, `{"type":"queue-operation"}`),
			want: false,
		},
		{
			name: "assistant with empty content does not count",
			rec:  mustRecord(t, `{"type":"assistant","message":{"content":[]}}`),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isContentTypedWorkTurn(tc.rec); got != tc.want {
				t.Errorf("isContentTypedWorkTurn = %v, want %v", got, tc.want)
			}
		})
	}
}

func mustRecord(t *testing.T, raw string) transcriptRecord {
	t.Helper()
	var rec transcriptRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return rec
}

// ── defaultTranscriptTail (real file I/O, HOME-scoped to a temp dir) ─────────

func TestDefaultTranscriptTail_RecentWorkAndFailureTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := "/Users/ericlloyd/.agent-teams-worktrees/at-jolk-impl"
	sessionID := "sess-1"
	dir := filepath.Join(home, ".claude", "projects", slugifyForTest(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-5 * time.Minute).Format(time.RFC3339)

	lines := []string{
		`{"type":"system","timestamp":"` + old + `"}`,
		`{"type":"queue-operation","timestamp":"` + old + `"}`,
		`{"type":"assistant","timestamp":"` + recent + `","message":{"content":[{"type":"text","text":"working"}]}}`,
		// D7's failure-token corroborator greps RAW file bytes for exact
		// substrings (not JSON-decoded content), so this fixture line
		// carries the token unescaped rather than nested inside a JSON
		// string value (which would require literal backslash-escaping and
		// so would never byte-match the bare token).
		`diagnostic noise: command status="killed" observed during the stall`,
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	since := now.Add(-30 * time.Minute)
	recentWork, failureTokens, err := defaultTranscriptTail(cwd, sessionID, since)
	if err != nil {
		t.Fatalf("defaultTranscriptTail: %v", err)
	}
	if !recentWork {
		t.Error("expected recentWork=true (assistant/user turns within the window)")
	}
	if !failureTokens {
		t.Error(`expected failureTokens=true (status="killed" present)`)
	}
}

func TestDefaultTranscriptTail_MissingFileIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	recentWork, failureTokens, err := defaultTranscriptTail("/no/such/cwd", "no-such-session", time.Now())
	if err != nil {
		t.Fatalf("expected no error for a missing transcript, got %v", err)
	}
	if recentWork || failureTokens {
		t.Errorf("expected both false for a missing transcript, got recentWork=%v failureTokens=%v", recentWork, failureTokens)
	}
}

// slugifyForTest mirrors cost.SlugifyCWD's contract locally (no import cycle
// concern, but avoids re-deriving the algorithm — this just needs to match
// what defaultTranscriptTail itself computes via cost.SlugifyCWD).
func slugifyForTest(cwd string) string {
	var b strings.Builder
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ── workProductTripEligible (D2 gating, each exemption) ───────────────────────

func TestWorkProductTripEligible(t *testing.T) {
	// Expressed against the threshold var rather than a literal so retuning
	// the default (hung_config.go) can't silently invert these cases.
	flat := hungWorkProductFlatThreshold

	tests := []struct {
		name            string
		mode            string
		labels          []string
		claimed         bool
		flatline        time.Duration
		recentWorkTurns bool
		want            bool
	}{
		{"all conditions met => eligible", "bg", nil, true, flat, false, true},
		{"mode interactive => excluded", "interactive", nil, true, flat, false, false},
		{"mode empty (legacy, unset) => excluded", "", nil, true, flat, false, false},
		{"human+gate label => exempt", "bg", []string{"human", "gate:review"}, true, flat, false, false},
		{"human+gate:question (general gate:*) => exempt", "bg", []string{"human", "gate:question"}, true, flat, false, false},
		{"human label alone (no gate) => still eligible", "bg", []string{"human"}, true, flat, false, true},
		{"no claimed in-progress bead => exempt", "bg", nil, false, flat, false, false},
		{"flatline under threshold => exempt", "bg", nil, true, flat - time.Minute, false, false},
		{"flatline exactly at threshold => eligible", "bg", nil, true, flat, false, true},
		{"recent work turns corroborated => downgraded/held", "bg", nil, true, flat, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := workProductTripEligible(tc.mode, tc.labels, tc.claimed, tc.flatline, tc.recentWorkTurns)
			if got != tc.want {
				t.Errorf("workProductTripEligible = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── nextWorkProductLadderAction (D6 boundaries, pure) ─────────────────────────

func TestNextWorkProductLadderAction_Boundaries(t *testing.T) {
	const now = "2026-07-24T12:00:00Z"

	// Boundaries are expressed against the threshold vars, not literals, so
	// retuning the defaults (hung_config.go) can't silently move a case to
	// the wrong side of the boundary it is meant to pin.
	flat, alert := hungWorkProductFlatThreshold, hungWorkProductAlertThreshold

	// Just under the flat threshold: no action.
	anchor, action := nextWorkProductLadderAction(hungAnchor{}, flat-time.Minute, now)
	if action != hungActionNone {
		t.Fatalf("just under flat: action = %v, want none", action)
	}
	if anchor.WorkProductWakeAttempts != 0 {
		t.Fatalf("just under flat: WakeAttempts = %d, want 0", anchor.WorkProductWakeAttempts)
	}

	// Exactly at the flat threshold: first wake.
	anchor, action = nextWorkProductLadderAction(anchor, flat, now)
	if action != hungActionWake || anchor.WorkProductWakeAttempts != 1 {
		t.Fatalf("at flat: action=%v attempts=%d, want wake/1", action, anchor.WorkProductWakeAttempts)
	}

	// Next tick, still under the alert threshold, attempts<2: second wake.
	anchor, action = nextWorkProductLadderAction(anchor, flat+5*time.Minute, now)
	if action != hungActionWake || anchor.WorkProductWakeAttempts != 2 {
		t.Fatalf("flat+5m: action=%v attempts=%d, want wake/2", action, anchor.WorkProductWakeAttempts)
	}

	// Wakes exhausted (2), still under the alert threshold -> no third wake.
	anchor, action = nextWorkProductLadderAction(anchor, flat+10*time.Minute, now)
	if action != hungActionNone {
		t.Fatalf("flat+10m: action = %v, want none (wakes exhausted, not yet at alert)", action)
	}
	if anchor.WorkProductWakeAttempts != 2 {
		t.Fatalf("flat+10m: attempts = %d, want unchanged 2", anchor.WorkProductWakeAttempts)
	}

	// Exactly at the alert threshold: direct alert fires regardless of wake count.
	anchor, action = nextWorkProductLadderAction(anchor, alert, now)
	if action != hungActionAlert || anchor.WorkProductAlertedAt != now {
		t.Fatalf("at alert: action=%v alertedAt=%q, want alert/%q", action, anchor.WorkProductAlertedAt, now)
	}

	// Already alerted -> none, unchanged, even further past the threshold.
	before := anchor
	anchor, action = nextWorkProductLadderAction(anchor, 2*alert, "2026-07-24T13:00:00Z")
	if action != hungActionNone || anchor != before {
		t.Fatalf("post-alert: action=%v anchor changed (got %+v want %+v)", action, anchor, before)
	}
}

func TestNextWorkProductLadderAction_AlertFiresEvenIfWakesNeverSent(t *testing.T) {
	// The alert-threshold alert is a hard backstop per D6 -- it must not
	// depend on the wake-attempt counter having reached
	// hungWakeAttemptsBeforeDirectAlert (e.g. every wake send failed, or the
	// ladder was only just seeded).
	_, action := nextWorkProductLadderAction(hungAnchor{}, hungWorkProductAlertThreshold, "2026-07-24T12:00:00Z")
	if action != hungActionAlert {
		t.Fatalf("action = %v, want alert even with zero prior wake attempts", action)
	}
}

// ── nextDeadLadderAction (D4, mirrors nextHungLadderAction's shape) ───────────

func TestNextDeadLadderAction(t *testing.T) {
	const now = "2026-07-24T12:00:00Z"
	anchor := hungAnchor{DeadSince: "2026-07-24T11:00:00Z"}

	for i := 1; i <= hungWakeAttemptsBeforeDirectAlert; i++ {
		var action hungLadderAction
		anchor, action = nextDeadLadderAction(anchor, now)
		if action != hungActionWake || anchor.DeadWakeAttempts != i {
			t.Fatalf("attempt %d: action=%v attempts=%d, want wake/%d", i, action, anchor.DeadWakeAttempts, i)
		}
	}

	anchor, action := nextDeadLadderAction(anchor, now)
	if action != hungActionAlert || anchor.DeadAlertedAt != now {
		t.Fatalf("post-ladder: action=%v alertedAt=%q, want alert/%q", action, anchor.DeadAlertedAt, now)
	}

	before := anchor
	anchor, action = nextDeadLadderAction(anchor, "2026-07-24T13:00:00Z")
	if action != hungActionNone || anchor != before {
		t.Fatalf("already alerted: action=%v anchor changed", action)
	}
}

// ── appendHungJournal / rotation (D8) ──────────────────────────────────────────

func TestAppendHungJournal_WritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hung-journal.jsonl")
	entry := hungJournalEntry{Timestamp: "2026-07-24T12:00:00Z", InitiativeID: "at-1", Classification: hungClassStuck, Ladder: "stuck", LadderAction: "wake"}
	if err := appendHungJournal(path, entry); err != nil {
		t.Fatalf("appendHungJournal: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 journal line, got %d: %q", len(lines), data)
	}
	var got hungJournalEntry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal journal line: %v", err)
	}
	if got != entry {
		t.Errorf("journal round-trip = %+v, want %+v", got, entry)
	}
}

func TestAppendHungJournal_RotatesPastCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hung-journal.jsonl")

	origCap := hungJournalMaxBytes
	hungJournalMaxBytes = 10 // trivially small so one write already exceeds it on the next append
	defer func() { hungJournalMaxBytes = origCap }()

	if err := appendHungJournal(path, hungJournalEntry{InitiativeID: "at-1"}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendHungJournal(path, hungJournalEntry{InitiativeID: "at-2"}); err != nil {
		t.Fatalf("second append (should rotate first): %v", err)
	}

	backup := path + ".1"
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected rotated backup %s to exist: %v", backup, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read post-rotation journal: %v", err)
	}
	if !strings.Contains(string(data), "at-2") {
		t.Errorf("expected the post-rotation journal to contain the second entry, got %q", data)
	}
	if strings.Contains(string(data), "at-1") {
		t.Errorf("expected the pre-rotation entry to have moved to the backup, not stayed in the live file: %q", data)
	}
}

// ── scanHung integration: work-product durability across a busy/idle blip ────
//
// This is the core D1/D3 anchor-durability test: a WORKING initiative whose
// git state is flat accumulates flatline time tick over tick; a transient
// classification blip (session goes idle -> STUCK for one tick, then back to
// WORKING) must NOT reset the accumulated flatline, because nothing about
// the underlying git/bead artifacts changed.

func TestScanHung_WorkProduct_DurableAcrossBusyBlip(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "one",
		Description: "worktree: " + wt + "\nmode: bg\n",
		Status:      "open",
	}}
	ctx := makeHungCtx(t, issues)

	origProbe, origClaimed := hungGitProbeFunc, hungProjectClaimedBeadFunc
	defer func() { hungGitProbeFunc, hungProjectClaimedBeadFunc = origProbe, origClaimed }()

	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	gitAnchorTime := t0.Add(-1 * time.Hour) // last REAL git activity, well before this test's window
	hungGitProbeFunc = func(worktree string) gitProbeResult {
		// A constant IndexMtime/CommitTime (never changes across ticks) IS
		// the flatline signal here; StatusHash is likewise constant.
		return gitProbeResult{Available: true, StatusHash: "flat-forever", IndexMtime: gitAnchorTime, CommitTime: gitAnchorTime}
	}
	hungProjectClaimedBeadFunc = func(string) (bool, error) { return true, nil }

	pid := 1
	workingSessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}

	// Tick 1: WORKING, flat git -> work-product clock anchors at t0.
	out, err := scanHung(ctx, func() ([]agentSession, error) { return workingSessions, nil }, fixedNow(t0), true)
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("tick1: classification = %q, want WORKING", out[0].Classification)
	}
	firstProgress := out[0].WorkProductLastProgress
	if firstProgress == "" {
		t.Fatal("tick1: expected a non-empty work-product last-progress timestamp")
	}

	// Tick 2, 20 minutes later: a ONE-TICK blip to STUCK (session idle) --
	// must NOT reset the work-product clock.
	t1 := t0.Add(20 * time.Minute)
	out, err = scanHung(ctx, func() ([]agentSession, error) { return idleSessions, nil }, fixedNow(t1), true)
	if err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if out[0].Classification != hungClassStuck {
		t.Fatalf("tick2: classification = %q, want STUCK (the blip)", out[0].Classification)
	}

	// Tick 3, back to WORKING, past the flat threshold measured from t0 ->
	// the work-product clock must show the SAME last-progress as tick 1
	// (durable across the blip) and a flatline duration reflecting the FULL
	// elapsed time since t0, not reset by the STUCK blip at t1. Expressed
	// against the threshold var so retuning the default (hung_config.go)
	// can't drop this tick back under the eligibility boundary.
	elapsed := hungWorkProductFlatThreshold + 5*time.Minute
	t2 := t0.Add(elapsed)
	out, err = scanHung(ctx, func() ([]agentSession, error) { return workingSessions, nil }, fixedNow(t2), true)
	if err != nil {
		t.Fatalf("tick3: %v", err)
	}
	if out[0].Classification != hungClassWorking {
		t.Fatalf("tick3: classification = %q, want WORKING", out[0].Classification)
	}
	if out[0].WorkProductLastProgress != firstProgress {
		t.Errorf("tick3: work-product last-progress = %q, want unchanged %q (blip must not reset it)", out[0].WorkProductLastProgress, firstProgress)
	}
	if out[0].WorkProductFlatSeconds < int64(elapsed/time.Second) {
		t.Errorf("tick3: wp_flat_seconds = %d, want >= %d (accumulated across the blip, not reset)", out[0].WorkProductFlatSeconds, int64(elapsed/time.Second))
	}
	if !out[0].WorkProductTripEligible {
		t.Errorf("tick3: expected work-product trip eligibility at %s flat, mode:bg, claimed bead, no gate", elapsed)
	}
}

func TestScanHung_WorkProduct_RealChangeResetsClock(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "one",
		Description: "worktree: " + wt + "\nmode: bg\n",
		Status:      "open",
	}}
	ctx := makeHungCtx(t, issues)

	origProbe, origClaimed := hungGitProbeFunc, hungProjectClaimedBeadFunc
	defer func() { hungGitProbeFunc, hungProjectClaimedBeadFunc = origProbe, origClaimed }()
	hash := "v1"
	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	gitAnchorTime := t0.Add(-2 * time.Hour) // constant across both ticks -- isolates the hash-change effect
	hungGitProbeFunc = func(string) gitProbeResult {
		return gitProbeResult{Available: true, StatusHash: hash, IndexMtime: gitAnchorTime}
	}
	hungProjectClaimedBeadFunc = func(string) (bool, error) { return true, nil }

	pid := 1
	sessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return sessions, nil }

	out, err := scanHung(ctx, agentsFunc, fixedNow(t0), true)
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	firstProgress := out[0].WorkProductLastProgress
	if firstProgress == "" {
		t.Fatal("tick1: expected a non-empty last-progress from the constant index mtime signal")
	}

	// A real git change (new hash) at t0+45m must advance last-progress.
	t1 := t0.Add(45 * time.Minute)
	hash = "v2"
	out, err = scanHung(ctx, agentsFunc, fixedNow(t1), true)
	if err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if out[0].WorkProductLastProgress == firstProgress {
		t.Error("expected last-progress to advance once the git status hash actually changed")
	}
	if out[0].WorkProductFlatSeconds != 0 {
		t.Errorf("wp_flat_seconds = %d, want 0 immediately after a real change", out[0].WorkProductFlatSeconds)
	}
	if out[0].WorkProductTripEligible {
		t.Error("must not be trip-eligible immediately after a real work-product change")
	}
}

// ── D4: DEAD-with-worktree-present joins the ladder ───────────────────────────

func TestScanHung_DeadWithWorktree_AnchorsAndHungAtThreshold(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{ID: "at-1", Title: "one", Description: "worktree: " + wt, Status: "open"}}
	ctx := makeHungCtx(t, issues)

	// No live session at all -> DEAD, but the worktree directory exists.
	agentsFunc := func() ([]agentSession, error) { return nil, nil }
	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	out, err := scanHung(ctx, agentsFunc, fixedNow(t0), true)
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if out[0].Classification != hungClassDead || !out[0].CWDPresent {
		t.Fatalf("tick1: classification=%q cwdPresent=%v, want DEAD/true", out[0].Classification, out[0].CWDPresent)
	}
	if out[0].DeadHung {
		t.Error("tick1: must not be dead_hung immediately")
	}
	if out[0].DeadSince == "" {
		t.Fatal("tick1: expected dead_since to be anchored")
	}

	t1 := t0.Add(hungDeadWorktreeThreshold + time.Minute)
	out, err = scanHung(ctx, agentsFunc, fixedNow(t1), true)
	if err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if !out[0].DeadHung {
		t.Errorf("tick2: expected dead_hung=true after crossing the %s threshold", hungDeadWorktreeThreshold)
	}
}

func TestDoHungTick_DeadLadder_WakeThenAlert(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "dead initiative", Description: "worktree: " + wt,
		Status: "open", Labels: []string{"at-1", "thread:99"},
	}}
	ctx := makeHungCtx(t, issues)

	agentsFunc := func() ([]agentSession, error) { return nil, nil } // no live session -> DEAD
	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{"at-1": {DeadSince: seedDeadSince}}); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "99"}
	deps := hungTickDeps{agentsFunc: agentsFunc, now: fixedNow(t0), wakeSend: wake.send, topicPost: defaultHungTopicPost, transport: ft}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if len(wake.bodies) != 1 {
		t.Fatalf("tick1: wake calls = %d, want 1", len(wake.bodies))
	}

	deps.now = fixedNow(t0.Add(5 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Fatalf("tick2: wake calls = %d, want 2", len(wake.bodies))
	}

	deps.now = fixedNow(t0.Add(10 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick3: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("tick3: expected the DEAD ladder's canned alert to fire, topic posts = %d", len(ft.calls))
	}
	if !strings.Contains(ft.calls[0].Body, "DEAD") {
		t.Errorf("alert body = %q, want it to mention DEAD", ft.calls[0].Body)
	}
}

// ── D5: mode:interactive excludes ALL mechanical escalation ───────────────────

func TestDoHungTick_ModeInteractive_NoEscalationButStillJournaled(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-1", Title: "interactive initiative",
		Description: "worktree: " + wt + "\nmode: interactive\n",
		Status:      "open", Labels: []string{"at-1", "thread:1"},
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return idleSessions, nil }

	t0 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{"at-1": {StuckSince: seedStuckSince}}); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "1"}
	deps := hungTickDeps{agentsFunc: agentsFunc, now: fixedNow(t0), wakeSend: wake.send, topicPost: defaultHungTopicPost, transport: ft}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(wake.bodies) != 0 {
		t.Errorf("expected no wake for a mode:interactive initiative even though STUCK/Hung, got %d", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Errorf("expected no alert for a mode:interactive initiative, got %d", len(ft.calls))
	}

	data, err := os.ReadFile(hungJournalPath(StewardHome(ctx)))
	if err != nil {
		t.Fatalf("expected a journal entry even for the excluded interactive initiative: %v", err)
	}
	if !strings.Contains(string(data), `"id":"at-1"`) {
		t.Errorf("journal missing the interactive initiative's entry: %q", data)
	}
}

// ── D9: track-worktree matcher ───────────────────────────────────────────────
//
// The parsing test that lived here is gone with trackWorktreePaths; track
// paths now come from initiative.Of(iss).Tracks, which internal/initiative
// tests. discoverWorktrees' union behaviour is covered above.
