package verbs

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── nextHungLadderAction (pure, no I/O) ──────────────────────────────────────

// TestNextHungLadderAction drives the ladder generically off
// hungWakeAttemptsBeforeDirectAlert (rather than a hardcoded attempt count)
// so a future change to the named constant is exercised by this test, not
// silently bypassed by it.
func TestNextHungLadderAction(t *testing.T) {
	const now = "2026-07-21T10:00:00Z"
	anchor := hungAnchor{StuckSince: "2026-07-21T09:00:00Z"}

	for i := 1; i <= hungWakeAttemptsBeforeDirectAlert; i++ {
		var action hungLadderAction
		anchor, action = nextHungLadderAction(anchor, now)
		if action != hungActionWake {
			t.Fatalf("attempt %d: action = %v, want hungActionWake", i, action)
		}
		if anchor.WakeAttempts != i {
			t.Fatalf("attempt %d: WakeAttempts = %d, want %d", i, anchor.WakeAttempts, i)
		}
		if anchor.LastWakeAt != now {
			t.Fatalf("attempt %d: LastWakeAt = %q, want %q", i, anchor.LastWakeAt, now)
		}
	}

	// Ladder exhausted -> canned alert, exactly once.
	var action hungLadderAction
	anchor, action = nextHungLadderAction(anchor, now)
	if action != hungActionAlert {
		t.Fatalf("post-ladder: action = %v, want hungActionAlert", action)
	}
	if anchor.AlertedAt != now {
		t.Fatalf("post-ladder: AlertedAt = %q, want %q", anchor.AlertedAt, now)
	}

	// Already alerted -> none, anchor unchanged (dedup within the episode).
	before := anchor
	anchor, action = nextHungLadderAction(anchor, "2026-07-21T11:00:00Z")
	if action != hungActionNone {
		t.Fatalf("already alerted: action = %v, want hungActionNone", action)
	}
	if anchor != before {
		t.Fatalf("already alerted: anchor changed, got %+v, want unchanged %+v", anchor, before)
	}
}

// ── doHungTick (integration-level, fakes for send + topic-post + transport) ──

// fakeHungWakeSend is an injectable hungWakeSendFunc that records the
// envelope body written to each temp file it's handed (so tests can assert
// on wake body content) and returns a configured error.
type fakeHungWakeSend struct {
	bodies []string
	err    error
}

func (f *fakeHungWakeSend) send(_ *cli.Context, file string) error {
	data, readErr := os.ReadFile(file)
	if readErr != nil {
		return fmt.Errorf("fakeHungWakeSend: read temp file: %w", readErr)
	}
	f.bodies = append(f.bodies, string(data))
	return f.err
}

func TestDoHungTick_WakeThenAlertLadder(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID:          "at-1",
		Title:       "stuck initiative",
		Description: "worktree: " + wt,
		Status:      "open",
		Labels:      []string{"at-1", "thread:555"},
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return idleSessions, nil }

	// Seed an anchor whose StuckSince is already past hungStuckThreshold so
	// the very first tick observes Hung=true (scanHung only sets a fresh
	// StuckSince when no anchor exists yet). Offsets throughout this file
	// are expressed against the threshold var rather than literals, so
	// retuning the default (hung_config.go) can't silently move a seed to
	// the wrong side of it.
	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-1": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "555"}
	deps := hungTickDeps{
		agentsFunc: agentsFunc,
		now:        fixedNow(t0),
		wakeSend:   wake.send,
		topicPost:  defaultHungTopicPost,
		transport:  ft,
	}

	// Tick 1: wake attempt 1, no topic post.
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if len(wake.bodies) != 1 {
		t.Fatalf("tick 1: wake calls = %d, want 1", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Fatalf("tick 1: topic posts = %d, want 0", len(ft.calls))
	}
	anchors := loadHungState(hungStatePath(ctx))
	if anchors["at-1"].WakeAttempts != 1 {
		t.Errorf("tick 1: WakeAttempts = %d, want 1", anchors["at-1"].WakeAttempts)
	}
	if anchors["at-1"].AlertedAt != "" {
		t.Errorf("tick 1: AlertedAt should still be empty, got %q", anchors["at-1"].AlertedAt)
	}

	// Tick 2: wake attempt 2, still no topic post.
	deps.now = fixedNow(t0.Add(5 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Fatalf("tick 2: wake calls = %d, want 2", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Fatalf("tick 2: topic posts = %d, want 0", len(ft.calls))
	}
	anchors = loadHungState(hungStatePath(ctx))
	if anchors["at-1"].WakeAttempts != hungWakeAttemptsBeforeDirectAlert {
		t.Errorf("tick 2: WakeAttempts = %d, want %d", anchors["at-1"].WakeAttempts, hungWakeAttemptsBeforeDirectAlert)
	}

	// Tick 3: ladder exhausted -> canned alert posted exactly once, no
	// further wake attempt.
	deps.now = fixedNow(t0.Add(10 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Errorf("tick 3: wake calls should stay at 2, got %d", len(wake.bodies))
	}
	if len(ft.calls) != 1 {
		t.Fatalf("tick 3: topic posts = %d, want 1", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "555" {
		t.Errorf("tick 3: alert ThreadRef = %q, want 555", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != "at-1" {
		t.Errorf("tick 3: alert InitiativeID = %q, want at-1", ft.calls[0].InitiativeID)
	}
	anchors = loadHungState(hungStatePath(ctx))
	if anchors["at-1"].AlertedAt == "" {
		t.Error("tick 3: expected AlertedAt to be stamped")
	}

	// Tick 4: already alerted this episode -> no further wake, no further alert.
	deps.now = fixedNow(t0.Add(15 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 4: %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Errorf("tick 4: wake calls should stay at 2, got %d", len(wake.bodies))
	}
	if len(ft.calls) != 1 {
		t.Errorf("tick 4: topic posts should stay at 1, got %d", len(ft.calls))
	}
}

// TestDoHungTick_PersistOnChangeGuard proves the persist-on-change guard
// (agent-teams-6rru.17 Finding 2): doHungTick only writes the ladder state
// when the ladder actually advances. scanHung persists once per tick
// unconditionally, so a tick that ADVANCES the ladder writes twice (scanHung +
// doHungTick) while an already-alerted no-op tick writes only once (scanHung
// alone — doHungTick's redundant, byte-identical write is skipped). The write
// count is the observable signal, so this test wraps saveHungState to count.
func TestDoHungTick_PersistOnChangeGuard(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID:          "at-8",
		Title:       "guard initiative",
		Description: "worktree: " + wt,
		Status:      "open",
		Labels:      []string{"at-8", "thread:77"},
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return idleSessions, nil }

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-8": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	realSave := saveHungState
	var saves int
	saveHungState = func(path string, m map[string]hungAnchor) error {
		saves++
		return realSave(path, m)
	}
	defer func() { saveHungState = realSave }()

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "77"}
	deps := hungTickDeps{
		agentsFunc: agentsFunc,
		now:        fixedNow(t0),
		wakeSend:   wake.send,
		topicPost:  defaultHungTopicPost,
		transport:  ft,
	}

	// Advance the ladder to the already-alerted state (2 wakes then 1 alert),
	// asserting each advancing tick writes twice: scanHung + doHungTick.
	for i, offset := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute} {
		deps.now = fixedNow(t0.Add(offset))
		saves = 0
		if err := doHungTick(ctx, deps); err != nil {
			t.Fatalf("advancing tick %d: %v", i+1, err)
		}
		if saves != 2 {
			t.Errorf("advancing tick %d: saveHungState calls = %d, want 2 (scanHung + doHungTick ladder write)", i+1, saves)
		}
	}
	if loadHungState(hungStatePath(ctx))["at-8"].AlertedAt == "" {
		t.Fatal("expected AlertedAt stamped after the ladder was exhausted")
	}

	// Already alerted -> hungActionNone -> ladder unchanged: only scanHung's
	// single write, doHungTick's redundant write skipped by the guard.
	deps.now = fixedNow(t0.Add(15 * time.Minute))
	saves = 0
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("no-op tick: %v", err)
	}
	if saves != 1 {
		t.Errorf("already-alerted no-op tick: saveHungState calls = %d, want 1 (scanHung only; guard skips doHungTick's write)", saves)
	}
}

// TestDoHungTick_WakeSendFailure_LadderStillReachesAlert simulates the
// Steward being unreachable (mail-send fails every attempt): the ladder
// must still count attempts and reach the canned-alert fallback, since a
// failed wake is exactly the scenario the direct-alert escalation exists
// for.
func TestDoHungTick_WakeSendFailure_LadderStillReachesAlert(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID:          "at-2",
		Title:       "stuck initiative 2",
		Description: "worktree: " + wt,
		Status:      "open",
		Labels:      []string{"at-2", "thread:9"},
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	agentsFunc := func() ([]agentSession, error) { return idleSessions, nil }

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-2": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	wake := &fakeHungWakeSend{err: fmt.Errorf("steward mailbox unreachable")}
	ft := &fakeTransport{returnRef: "9"}
	deps := hungTickDeps{
		agentsFunc: agentsFunc,
		now:        fixedNow(t0),
		wakeSend:   wake.send,
		topicPost:  defaultHungTopicPost,
		transport:  ft,
	}

	for i, offset := range []time.Duration{0, 5 * time.Minute} {
		deps.now = fixedNow(t0.Add(offset))
		if err := doHungTick(ctx, deps); err != nil {
			t.Fatalf("tick %d: doHungTick returned error (wake failures must be logged, not fatal): %v", i+1, err)
		}
	}
	if len(wake.bodies) != hungWakeAttemptsBeforeDirectAlert {
		t.Fatalf("wake attempts = %d, want %d despite every send failing", len(wake.bodies), hungWakeAttemptsBeforeDirectAlert)
	}
	if len(ft.calls) != 0 {
		t.Fatalf("no alert expected yet, got %d", len(ft.calls))
	}

	// Next tick: ladder exhausted despite every wake having failed ->
	// canned alert still fires.
	deps.now = fixedNow(t0.Add(10 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected the canned alert to fire once the wake ladder is exhausted, topic posts = %d", len(ft.calls))
	}
}

// TestDoHungTick_EpisodeEnds_LadderResetsOnReentry confirms that once an
// initiative leaves STUCK (scanHung drops its anchor entirely), a
// subsequent STUCK re-entry starts a genuinely fresh ladder — not yet Hung
// while freshly stuck, then wake attempt 1 again (not a continuation of the
// previous episode's count) once it crosses the threshold again.
func TestDoHungTick_EpisodeEnds_LadderResetsOnReentry(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID:          "at-3",
		Title:       "flaky initiative",
		Description: "worktree: " + wt,
		Status:      "open",
		Labels:      []string{"at-3", "thread:1"},
	}}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	busySessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-3": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "1"}
	deps := hungTickDeps{
		agentsFunc: func() ([]agentSession, error) { return idleSessions, nil },
		now:        fixedNow(t0),
		wakeSend:   wake.send,
		topicPost:  defaultHungTopicPost,
		transport:  ft,
	}

	// Tick 1: wake attempt 1 (episode already past threshold from the seed).
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if len(wake.bodies) != 1 {
		t.Fatalf("tick 1: wake calls = %d, want 1", len(wake.bodies))
	}

	// Episode ends: session goes busy (WORKING). scanHung clears the STUCK
	// sub-state (agent-teams-sgr5 D1/D3: an anchor may still persist to
	// carry the durable work-product clock, but StuckSince/WakeAttempts must
	// not survive — see TestScanHung_StuckAnchorLifecycle).
	deps.agentsFunc = func() ([]agentSession, error) { return busySessions, nil }
	deps.now = fixedNow(t0.Add(time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 2 (goes WORKING): %v", err)
	}
	anchors := loadHungState(hungStatePath(ctx))
	if anchor, ok := anchors["at-3"]; ok && anchor.StuckSince != "" {
		t.Fatalf("expected STUCK sub-state cleared once the initiative leaves STUCK, got StuckSince=%q", anchor.StuckSince)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("no additional wake once WORKING, got %d", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Errorf("no alert once WORKING, got %d", len(ft.calls))
	}

	// Re-entry: idle/STUCK again, but freshly stuck (elapsed well under
	// hungStuckThreshold) -> not yet Hung, ladder must not fire at all.
	deps.agentsFunc = func() ([]agentSession, error) { return idleSessions, nil }
	deps.now = fixedNow(t0.Add(2 * time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 3 (fresh STUCK): %v", err)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("freshly-stuck (not yet hung) must not wake, wake calls = %d", len(wake.bodies))
	}
	anchors = loadHungState(hungStatePath(ctx))
	anchor, ok := anchors["at-3"]
	if !ok {
		t.Fatal("expected a fresh anchor recorded for the new STUCK episode")
	}
	if anchor.WakeAttempts != 0 {
		t.Errorf("fresh episode WakeAttempts = %d, want 0 (not carried over from the previous episode)", anchor.WakeAttempts)
	}

	// Advance past the threshold on the NEW episode via realistic
	// tick-interval cadence: silent while still under threshold, crossing
	// it on the final tick -> ladder restarts at attempt 1, proving the
	// previous episode's count was not reused.
	reentryAt := t0.Add(2 * time.Minute)
	for elapsed := time.Duration(0); elapsed+hungTickInterval < hungStuckThreshold; elapsed += hungTickInterval {
		cur := reentryAt.Add(elapsed + hungTickInterval)
		deps.now = fixedNow(cur)
		if err := doHungTick(ctx, deps); err != nil {
			t.Fatalf("intervening tick at %s: %v", cur, err)
		}
		if len(wake.bodies) != 1 {
			t.Fatalf("intervening tick at %s should stay below threshold, wake calls = %d", cur, len(wake.bodies))
		}
	}
	deps.now = fixedNow(reentryAt.Add(hungStuckThreshold + time.Minute))
	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("tick 4 (new episode crosses threshold): %v", err)
	}
	if len(wake.bodies) != 2 {
		t.Fatalf("expected the ladder to restart at attempt 1 on the new episode, wake calls = %d", len(wake.bodies))
	}
	anchors = loadHungState(hungStatePath(ctx))
	if anchors["at-3"].WakeAttempts != 1 {
		t.Errorf("new episode WakeAttempts = %d, want 1 (ladder restarted, not continued)", anchors["at-3"].WakeAttempts)
	}
}

// TestPostHungAlert_NoThreadLabel_ReturnsError confirms postHungAlert
// surfaces a clear error (for the caller to log, non-fatal to the tick
// loop) when the initiative has no known Telegram topic yet, rather than
// posting to an empty ThreadRef.
func TestPostHungAlert_NoThreadLabel_ReturnsError(t *testing.T) {
	issues := []bd.Issue{{ID: "at-4", Title: "no topic yet", Labels: []string{"at-4"}}}
	ctx := makeHungCtx(t, issues)
	ft := &fakeTransport{returnRef: "should-not-be-used"}

	entry := hungScanEntry{ID: "at-4", Title: "no topic yet", Hung: true, StuckSince: "2026-07-21T09:00:00Z"}
	deps := hungTickDeps{topicPost: defaultHungTopicPost, transport: ft}

	err := postHungAlert(ctx, deps, entry, hungAlertBody(entry.ID, entry.Title, entry.StuckSince))
	if err == nil {
		t.Fatal("expected an error when the initiative has no thread label")
	}
	if len(ft.calls) != 0 {
		t.Errorf("transport.Send should not be called when there is no topic to post into, got %d calls", len(ft.calls))
	}
}

// TestPostHungAlert_PostsCannedBodyToKnownTopic confirms postHungAlert
// resolves the "thread:<ref>" label and posts a deterministic body into
// that exact topic.
func TestPostHungAlert_PostsCannedBodyToKnownTopic(t *testing.T) {
	issues := []bd.Issue{{ID: "at-5", Title: "known topic", Labels: []string{"at-5", "thread:42"}}}
	ctx := makeHungCtx(t, issues)
	ft := &fakeTransport{returnRef: "42"}

	entry := hungScanEntry{ID: "at-5", Title: "known topic", Hung: true, StuckSince: "2026-07-21T09:00:00Z"}
	deps := hungTickDeps{topicPost: defaultHungTopicPost, transport: ft}

	if err := postHungAlert(ctx, deps, entry, hungAlertBody(entry.ID, entry.Title, entry.StuckSince)); err != nil {
		t.Fatalf("postHungAlert: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected exactly 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "42" {
		t.Errorf("ThreadRef = %q, want 42", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != "at-5" {
		t.Errorf("InitiativeID = %q, want at-5", ft.calls[0].InitiativeID)
	}
	if ft.calls[0].Body == "" {
		t.Error("expected a non-empty canned alert body")
	}
}

// ── sendHungWakeEnvelope ──────────────────────────────────────────────────────

// TestSendHungWakeEnvelope_WrapsBodyInStewardHungWakeEnvelope confirms the
// wake nudge is folded into BuildStewardHungWakeEnvelope(id, body) — a
// dedicated envelope kind distinct from a real Eric reply (agent-teams-6rru.16
// — steward_seams.go) — so the Steward can recognize the mechanical wake
// without misreading it as an Eric reply.
func TestSendHungWakeEnvelope_WrapsBodyInStewardHungWakeEnvelope(t *testing.T) {
	var gotFile, gotContent string
	send := func(_ *cli.Context, file string) error {
		gotFile = file
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read temp file inside send: %w", err)
		}
		gotContent = string(data)
		return nil
	}
	ctx := makeHungCtx(t, nil)

	if err := sendHungWakeEnvelope(ctx, send, "at-6", "hung-scan wake body"); err != nil {
		t.Fatalf("sendHungWakeEnvelope: %v", err)
	}
	want, _ := BuildStewardHungWakeEnvelope("at-6", "hung-scan wake body")
	if gotContent != want {
		t.Errorf("envelope written = %q, want %q", gotContent, want)
	}
	// Temp file must be cleaned up after send returns.
	if _, err := os.Stat(gotFile); !os.IsNotExist(err) {
		t.Errorf("expected temp file %q to be removed after send, stat err = %v", gotFile, err)
	}
}

// TestSendHungWakeEnvelope_SendFailurePropagates confirms a send failure is
// returned to the caller (doHungTick logs it; this test only checks the
// plumbing, not the logging).
func TestSendHungWakeEnvelope_SendFailurePropagates(t *testing.T) {
	sendErr := fmt.Errorf("mail send failed")
	send := func(_ *cli.Context, file string) error { return sendErr }
	ctx := makeHungCtx(t, nil)

	err := sendHungWakeEnvelope(ctx, send, "at-7", "body")
	if err == nil {
		t.Fatal("expected the send error to propagate")
	}
}

// ── agent-teams-huq7.1 S3/S5: the review-backstop auto-close ────────────────

// fakeHungClose is an injectable hungCloseFunc recording every (id, reason)
// call and returning a configured error.
type fakeHungClose struct {
	calls []struct{ id, reason string }
	err   error
}

func (f *fakeHungClose) close(_ *cli.Context, id, reason string) error {
	f.calls = append(f.calls, struct{ id, reason string }{id, reason})
	return f.err
}

// fakePendingReviewComment is an injectable pendingReviewCommentFunc
// returning a canned (pending, err) and recording every call — so tests
// drive the S3(d)/S4 gate without shelling to gh.
type fakePendingReviewComment struct {
	pending bool
	err     error
	calls   int
}

func (f *fakePendingReviewComment) probe(_ string, _ int) (bool, error) {
	f.calls++
	return f.pending, f.err
}

// reviewIssue builds the bd.Issue fixture shape common to every backstop
// test below: a review-shaped Description (worktree + pr-url) and,
// optionally, a review-posted Notes line.
func reviewIssue(id, wt, prURL string, posted bool) bd.Issue {
	notes := ""
	if posted {
		notes = "review-posted: PR — approved\n"
	}
	return bd.Issue{
		ID:          id,
		Title:       "review initiative",
		Description: "worktree: " + wt + "\npr-url: " + prURL + "\n",
		Notes:       notes,
		Status:      "open",
		Labels:      []string{id, "thread:1"},
	}
}

// TestDoHungTick_ReviewBackstop_ClosesOnDeadNoPendingComment is S3/S5's core
// path: a review-shaped, review-posted initiative with no live session
// (DEAD) and no pending comment is auto-closed — and the DEAD ladder never
// fires (no wake, no alert).
func TestDoHungTick_ReviewBackstop_ClosesOnDeadNoPendingComment(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-2rnv", wt, "https://github.com/acme/widget/pull/12", true)}
	ctx := makeHungCtx(t, issues)

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "1"}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // no live session -> DEAD
		now:                  fixedNow(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            ft,
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if closeFn.calls[0].id != "at-2rnv" {
		t.Errorf("closed id = %q, want at-2rnv", closeFn.calls[0].id)
	}
	if closeFn.calls[0].reason != hungReviewBackstopCloseReason {
		t.Errorf("close reason = %q, want %q", closeFn.calls[0].reason, hungReviewBackstopCloseReason)
	}
	if len(wake.bodies) != 0 {
		t.Errorf("wake calls = %d, want 0 (backstop close must skip the DEAD ladder)", len(wake.bodies))
	}
	if len(ft.calls) != 0 {
		t.Errorf("topic posts = %d, want 0", len(ft.calls))
	}

	journalData, err := os.ReadFile(hungJournalPath(StewardHome(ctx)))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(journalData), `"ladder_action":"close"`) {
		t.Errorf("journal missing the close ladder_action: %s", journalData)
	}
	if !strings.Contains(string(journalData), `"ladder":"review-backstop"`) {
		t.Errorf("journal missing the review-backstop ladder tag: %s", journalData)
	}
}

// TestDoHungTick_ReviewBackstop_ClosesOnStuckHungNoPendingComment is S3(c)'s
// other qualifying classification: a live-but-idle session that has crossed
// hungStuckThreshold (STUCK + Hung) also auto-closes, exactly like DEAD.
func TestDoHungTick_ReviewBackstop_ClosesOnStuckHungNoPendingComment(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-stuck", wt, "https://github.com/acme/widget/pull/13", true)}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-stuck": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return idleSessions, nil },
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if len(wake.bodies) != 0 {
		t.Errorf("wake calls = %d, want 0 (backstop close must skip the STUCK ladder)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_NoCloseWhenPendingComment proves S3(d): a
// pending, unanswered comment blocks the close even though every other gate
// condition holds — and the existing DEAD ladder still fires (wake), exactly
// as if this bead didn't exist.
func TestDoHungTick_ReviewBackstop_NoCloseWhenPendingComment(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-pending", wt, "https://github.com/acme/widget/pull/14", true)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-pending": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: true} // someone is still waiting on us
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (a pending comment must block the close)", len(closeFn.calls))
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_NoCloseWhenWorking proves S3(c): a live,
// actively-working review session is never closed, regardless of the other
// gate conditions — WORKING is excluded by construction.
func TestDoHungTick_ReviewBackstop_NoCloseWhenWorking(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-working", wt, "https://github.com/acme/widget/pull/15", true)}
	ctx := makeHungCtx(t, issues)

	pid := 1
	busySessions := []agentSession{{CWD: wt, Status: "busy", PID: &pid}}
	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return busySessions, nil },
		now:                  fixedNow(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)),
		wakeSend:             (&fakeHungWakeSend{}).send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (WORKING must never auto-close)", len(closeFn.calls))
	}
	if pending.calls != 0 {
		t.Errorf("pending-comment probe calls = %d, want 0 (gate (c) should short-circuit before ever probing)", pending.calls)
	}
}

// TestDoHungTick_ReviewBackstop_NoCloseWhenNeverPosted proves S3(b): a
// review-shaped, DEAD initiative whose review was never posted (no
// review-posted:/comment-replies: note) is NOT auto-closed — it still falls
// through to the existing DEAD ladder, which huq7.7 (not this bead) may one
// day refine.
func TestDoHungTick_ReviewBackstop_NoCloseWhenNeverPosted(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-neverposted", wt, "https://github.com/acme/widget/pull/16", false)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-neverposted": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (never-posted must not auto-close)", len(closeFn.calls))
	}
	if pending.calls != 0 {
		t.Errorf("pending-comment probe calls = %d, want 0 (gate (b) should short-circuit before ever probing)", pending.calls)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire for a never-posted review)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_NonReviewShapedUnaffected proves S1's gate:
// a plain (non-review) DEAD-with-worktree initiative is completely
// unaffected by this bead — no probe is ever consulted, and the existing D4
// ladder fires exactly as it did before huq7.4.
func TestDoHungTick_ReviewBackstop_NonReviewShapedUnaffected(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{{
		ID: "at-plain", Title: "plain initiative", Description: "worktree: " + wt,
		Status: "open", Labels: []string{"at-plain", "thread:1"},
	}}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-plain": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (non-review-shaped must never auto-close)", len(closeFn.calls))
	}
	if pending.calls != 0 {
		t.Errorf("pending-comment probe calls = %d, want 0 (gate (a) should short-circuit before ever probing)", pending.calls)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing D4/DEAD ladder must fire unchanged)", len(wake.bodies))
	}
}

// ── agent-teams-lu02.1: the merged/closed backstop (no review-posted note) ──

// fakePRState is an injectable prStateFunc returning a canned (state, err)
// and recording every call — so tests drive the agent-teams-lu02.1
// merged/closed gate without shelling to gh.
type fakePRState struct {
	state string
	err   error
	calls int
}

func (f *fakePRState) probe(_ string, _ int) (string, error) {
	f.calls++
	return f.state, f.err
}

// TestDoHungTick_ReviewBackstop_ClosesOnMergedNoPostedNote is
// agent-teams-lu02.1's core new path: a review-shaped, DEAD initiative that
// never got a review-posted note (session died first) is still auto-closed
// once the PR's own state proves the episode is over (MERGED), provided
// there's no pending unanswered comment. Uses the NEW close reason, and the
// DEAD ladder never fires.
func TestDoHungTick_ReviewBackstop_ClosesOnMergedNoPostedNote(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-merged", wt, "https://github.com/acme/widget/pull/20", false)}
	ctx := makeHungCtx(t, issues)

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateMerged}
	wake := &fakeHungWakeSend{}
	ft := &fakeTransport{returnRef: "1"}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // no live session -> DEAD
		now:                  fixedNow(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            ft,
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if closeFn.calls[0].id != "at-merged" {
		t.Errorf("closed id = %q, want at-merged", closeFn.calls[0].id)
	}
	if closeFn.calls[0].reason != hungReviewBackstopMergedCloseReason {
		t.Errorf("close reason = %q, want %q", closeFn.calls[0].reason, hungReviewBackstopMergedCloseReason)
	}
	if len(wake.bodies) != 0 {
		t.Errorf("wake calls = %d, want 0 (backstop close must skip the DEAD ladder)", len(wake.bodies))
	}

	journalData, err := os.ReadFile(hungJournalPath(StewardHome(ctx)))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(journalData), `"ladder_action":"close"`) {
		t.Errorf("journal missing the close ladder_action: %s", journalData)
	}
	if !strings.Contains(string(journalData), `"ladder":"review-backstop"`) {
		t.Errorf("journal missing the review-backstop ladder tag: %s", journalData)
	}
}

// TestDoHungTick_ReviewBackstop_ClosesOnClosedNoPostedNote is the CLOSED
// counterpart to the MERGED case above — GitHub's other terminal PR state
// must authorize the same close.
func TestDoHungTick_ReviewBackstop_ClosesOnClosedNoPostedNote(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-closed", wt, "https://github.com/acme/widget/pull/21", false)}
	ctx := makeHungCtx(t, issues)

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateClosed}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)),
		wakeSend:             (&fakeHungWakeSend{}).send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if closeFn.calls[0].reason != hungReviewBackstopMergedCloseReason {
		t.Errorf("close reason = %q, want %q", closeFn.calls[0].reason, hungReviewBackstopMergedCloseReason)
	}
}

// TestDoHungTick_ReviewBackstop_ClosesOnMergedStuckHungNoPostedNote proves
// gate (c)'s other qualifying classification for the merged path too: a
// live-but-idle session past hungStuckThreshold (STUCK + Hung) also
// auto-closes on MERGED, exactly like DEAD.
func TestDoHungTick_ReviewBackstop_ClosesOnMergedStuckHungNoPostedNote(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-mergedstuck", wt, "https://github.com/acme/widget/pull/22", false)}
	ctx := makeHungCtx(t, issues)

	pid := 1
	idleSessions := []agentSession{{CWD: wt, Status: "idle", PID: &pid}}
	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-(hungStuckThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-mergedstuck": {StuckSince: seedStuckSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateMerged}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return idleSessions, nil },
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if closeFn.calls[0].reason != hungReviewBackstopMergedCloseReason {
		t.Errorf("close reason = %q, want %q", closeFn.calls[0].reason, hungReviewBackstopMergedCloseReason)
	}
	if len(wake.bodies) != 0 {
		t.Errorf("wake calls = %d, want 0 (backstop close must skip the STUCK ladder)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPROpen is the guard
// proving an OPEN PR must never authorize this path — the entry falls
// through to the existing DEAD ladder exactly as it did before this bead.
func TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPROpen(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-open", wt, "https://github.com/acme/widget/pull/23", false)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-open": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateOpen}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (an OPEN PR must never authorize a close)", len(closeFn.calls))
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire for an open PR)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPendingComment proves
// gate (d) still applies to this path exactly as it does to the posted-note
// path: a pending unanswered comment blocks the close even though the PR is
// already MERGED.
func TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPendingComment(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-mergedpending", wt, "https://github.com/acme/widget/pull/24", false)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-mergedpending": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: true} // someone is still waiting on us
	prState := &fakePRState{state: ghPRStateMerged}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (a pending comment must block the close)", len(closeFn.calls))
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenProbeErrors proves the
// probe-unavailable guard: a failing PR-state probe means "cannot prove
// merged/closed" — probed=false — which must never authorize a close, even
// though every classification gate holds.
func TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenProbeErrors(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-probeerr", wt, "https://github.com/acme/widget/pull/25", false)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-probeerr": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{err: fmt.Errorf("gh pr view: exit status 1")}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (a failing PR-state probe must never authorize a close)", len(closeFn.calls))
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPreflightFails is the
// preflight variant of the probe-unavailable guard: gh unusable at all this
// tick must degrade the same way as a single failed call.
func TestDoHungTick_ReviewBackstop_MergedGate_NoCloseWhenPreflightFails(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-preflightfail", wt, "https://github.com/acme/widget/pull/26", false)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-preflightfail": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateMerged}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return fmt.Errorf("gh not authenticated") },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 0 {
		t.Fatalf("close calls = %d, want 0 (a failing gh preflight must never authorize a close)", len(closeFn.calls))
	}
	if prState.calls != 0 {
		t.Errorf("raw PR-state probe calls = %d, want 0 (preflight failure must skip the probe entirely)", prState.calls)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (existing DEAD ladder must still fire)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_PostedNotePathUnaffectedByMergedGate is the
// regression proof that the two gates are mutually exclusive by
// construction: a review-posted, DEAD, not-pending initiative still closes
// via the EXISTING (posted-note) reason, and the new merged-gate probe is
// never even consulted — the posted-note branch above always wins for a
// posted note, exactly as it did before this bead.
func TestDoHungTick_ReviewBackstop_PostedNotePathUnaffectedByMergedGate(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-postedstillworks", wt, "https://github.com/acme/widget/pull/27", true)}
	ctx := makeHungCtx(t, issues)

	closeFn := &fakeHungClose{}
	pending := &fakePendingReviewComment{pending: false}
	prState := &fakePRState{state: ghPRStateMerged} // must never be consulted for a posted note
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)),
		wakeSend:             (&fakeHungWakeSend{}).send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
		prState:              prState.probe,
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1", len(closeFn.calls))
	}
	if closeFn.calls[0].reason != hungReviewBackstopCloseReason {
		t.Errorf("close reason = %q, want the EXISTING reason %q (posted-note path must be unchanged)", closeFn.calls[0].reason, hungReviewBackstopCloseReason)
	}
	if prState.calls != 0 {
		t.Errorf("PR-state probe calls = %d, want 0 (a posted note must take the existing path and never consult the merged gate)", prState.calls)
	}
}

// TestDoHungTick_ReviewBackstop_NilCloseFuncFallsThroughToLadder proves the
// nil-seam safe default: with no closeFunc configured, the backstop never
// engages at all (not even a probe call), and the tick behaves exactly as it
// did before this bead existed.
func TestDoHungTick_ReviewBackstop_NilCloseFuncFallsThroughToLadder(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-noclose", wt, "https://github.com/acme/widget/pull/17", true)}
	ctx := makeHungCtx(t, issues)

	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-noclose": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            nil, // not configured
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if pending.calls != 0 {
		t.Errorf("pending-comment probe calls = %d, want 0 (nil closeFunc must short-circuit before probing)", pending.calls)
	}
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (must fall through to the existing DEAD ladder)", len(wake.bodies))
	}
}

// TestDoHungTick_ReviewBackstop_CloseErrorFallsThroughToLadder proves the
// agent-teams-huq7.1 CONTRACT AMENDMENT: when closeFunc returns an error for
// an otherwise gate-eligible entry, doHungTick must NOT journal a successful
// close (misleading — it didn't happen) and must NOT silently drop the
// entry — it must fall through to the existing DEAD ladder so the entry
// still gets a wake/alert, exactly as if the close had never been
// attempted.
func TestDoHungTick_ReviewBackstop_CloseErrorFallsThroughToLadder(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{reviewIssue("at-closeerr", wt, "https://github.com/acme/widget/pull/18", true)}
	ctx := makeHungCtx(t, issues)

	// Seed a DeadSince anchor already past hungDeadWorktreeThreshold, exactly
	// like TestDoHungTick_ReviewBackstop_NoCloseWhenPendingComment, so the
	// existing DEAD ladder (D4, entry.DeadHung) actually has something to
	// fire once this tick falls through to it — a freshly-classified DEAD
	// entry (DeadSince set to "now") wouldn't yet be past the ladder's own
	// elapsed threshold, which would mask this test's real assertion.
	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedDeadSince := t0.Add(-(hungDeadWorktreeThreshold + 5*time.Minute)).UTC().Format(time.RFC3339)
	if err := saveHungState(hungStatePath(ctx), map[string]hungAnchor{
		"at-closeerr": {DeadSince: seedDeadSince},
	}); err != nil {
		t.Fatalf("seed anchor state: %v", err)
	}

	closeFn := &fakeHungClose{err: fmt.Errorf("bd close: exit status 1")}
	pending := &fakePendingReviewComment{pending: false}
	wake := &fakeHungWakeSend{}
	deps := hungTickDeps{
		agentsFunc:           func() ([]agentSession, error) { return nil, nil }, // no live session -> DEAD
		now:                  fixedNow(t0),
		wakeSend:             wake.send,
		topicPost:            defaultHungTopicPost,
		transport:            &fakeTransport{returnRef: "1"},
		closeFunc:            closeFn.close,
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}

	if err := doHungTick(ctx, deps); err != nil {
		t.Fatalf("doHungTick: %v", err)
	}
	if len(closeFn.calls) != 1 {
		t.Fatalf("close calls = %d, want 1 (a failing close must still be attempted)", len(closeFn.calls))
	}

	journalData, err := os.ReadFile(hungJournalPath(StewardHome(ctx)))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	journalStr := string(journalData)
	if strings.Contains(journalStr, `"ladder_action":"close"`) {
		t.Errorf("journal must NOT record a successful close when closeFunc errored: %s", journalStr)
	}
	if !strings.Contains(journalStr, `"ladder_action":"close-failed"`) {
		t.Errorf("journal missing an accurate close-failed marker: %s", journalStr)
	}
	if !strings.Contains(journalStr, `"ladder":"dead"`) {
		t.Errorf("journal missing the fallthrough DEAD ladder entry: %s", journalStr)
	}

	// (b): the entry is NOT silently dropped — the existing DEAD ladder
	// still fires a wake, exactly as if the backstop gate had never held.
	if len(wake.bodies) != 1 {
		t.Errorf("wake calls = %d, want 1 (a failing close must still escalate via the existing DEAD ladder)", len(wake.bodies))
	}
}

// ── defaultHungClose (agent-teams-huq7.1 S5, real bd.Run mechanics) ─────────

// TestDefaultHungClose_WritesNoteThenCloses proves the real production
// closeFunc's mechanics: a `bd note --file=<tmp>` call carrying reason as
// durable prose, followed by a `bd close --reason=<reason>` call — both
// through ctx.BD.Run (the raw bd CLI), never the `ateam close` verb.
//
// The note's temp file is read INSIDE the fake exec call, not after
// defaultHungClose returns: defaultHungClose removes it (defer os.Remove)
// before returning, mirroring writeEnvelopeToTemp's cleanup discipline
// elsewhere in this file.
func TestDefaultHungClose_WritesNoteThenCloses(t *testing.T) {
	var calls []capturedCall
	var noteContent string
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		calls = append(calls, capturedCall{args: stripped})
		if len(stripped) > 0 && stripped[0] == "note" {
			for _, a := range stripped {
				if path, ok := strings.CutPrefix(a, "--file="); ok {
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatalf("read note temp file during call: %v", err)
					}
					noteContent = string(data)
				}
			}
		}
		return nil, nil, nil
	}
	ctx := &cli.Context{
		Home:   t.TempDir(),
		BD:     bd.NewClientWithExec(t.TempDir(), execFn),
		Stdout: &strings.Builder{},
		Stderr: &strings.Builder{},
	}

	if err := defaultHungClose(ctx, "at-1", "some reason"); err != nil {
		t.Fatalf("defaultHungClose: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("bd calls = %d, want 2 (note, close)", len(calls))
	}

	noteCall := calls[0]
	if len(noteCall.args) < 3 || noteCall.args[0] != "note" || noteCall.args[1] != "at-1" || !strings.HasPrefix(noteCall.args[2], "--file=") {
		t.Fatalf("first call = %v, want [note at-1 --file=...]", noteCall.args)
	}
	if !strings.Contains(noteContent, "some reason") {
		t.Errorf("note file content = %q, want it to contain the reason", noteContent)
	}

	closeCall := calls[1]
	wantClose := []string{"close", "at-1", "--reason=some reason"}
	if !reflect.DeepEqual(closeCall.args, wantClose) {
		t.Errorf("close call = %v, want %v", closeCall.args, wantClose)
	}
}

// TestDefaultHungClose_PropagatesNoteFailure proves a bd note failure
// aborts before ever attempting the close.
func TestDefaultHungClose_PropagatesNoteFailure(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{err: fmt.Errorf("bd note failed")}})
	if err := defaultHungClose(ctx, "at-1", "reason"); err == nil {
		t.Fatal("expected an error when bd note fails")
	}
	if len(*calls) != 1 {
		t.Errorf("bd calls = %d, want 1 (close must not be attempted after note fails)", len(*calls))
	}
}

// ── resolveOurGHLogin (agent-teams-huq7.1 CONTRACT AMENDMENT, S4 guard) ─────

// TestResolveOurGHLogin proves the login-validation guard defaultPending
// ReviewComment relies on: raw `gh api user -q .login` output must trim and
// validate before it's usable as hasPendingCommentThread's "ours" login. An
// empty/whitespace-only string or the literal "null" (both observed from
// GitHub App / installation-token auth that passes `gh auth status` but
// yields no real user login) must degrade to an error — never a silently
// accepted empty/null login — while an ordinary (possibly newline-terminated,
// as `gh api ... -q .login`'s real stdout is) login is accepted and trimmed.
func TestResolveOurGHLogin(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"empty string: rejected", "", "", true},
		{"whitespace/newline only: rejected", "\n", "", true},
		{"literal null: rejected", "null", "", true},
		{"literal null with trailing newline: rejected", "null\n", "", true},
		{"ordinary login: accepted and trimmed", "pr-shepherd-bot\n", "pr-shepherd-bot", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOurGHLogin(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveOurGHLogin(%q) = (%q, nil), want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveOurGHLogin(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("resolveOurGHLogin(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// ── hasPendingCommentThread (agent-teams-huq7.1 S4, pure grouping logic) ────

// TestHasPendingCommentThread mirrors SKILL.md comment-reply step 1's own
// selection rule against a table of thread shapes, with zero I/O (no real
// gh call — defaultPendingReviewComment's two `gh api` calls are the only
// part of that function this does NOT exercise).
func TestHasPendingCommentThread(t *testing.T) {
	const ourLogin = "pr-shepherd-bot"
	t1 := "2026-07-21T10:00:00Z"
	t2 := "2026-07-21T11:00:00Z"
	t3 := "2026-07-21T12:00:00Z"

	tests := []struct {
		name     string
		comments []ghPRComment
		want     bool
	}{
		{
			name:     "no comments at all",
			comments: nil,
			want:     false,
		},
		{
			name: "we replied, nobody after us: not pending",
			comments: []ghPRComment{
				{ID: 1, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{"alice"}},
				{ID: 2, InReplyToID: 1, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{ourLogin}},
			},
			want: false,
		},
		{
			name: "someone replied after us: pending",
			comments: []ghPRComment{
				{ID: 1, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{ourLogin}},
				{ID: 2, InReplyToID: 1, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{"alice"}},
			},
			want: true,
		},
		{
			name: "we're not in the thread at all: not pending",
			comments: []ghPRComment{
				{ID: 1, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{"alice"}},
				{ID: 2, InReplyToID: 1, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{"bob"}},
			},
			want: false,
		},
		{
			name: "root comment (no in_reply_to_id) IS the thread id: someone replies to it after us",
			comments: []ghPRComment{
				{ID: 5, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{ourLogin}}, // root, authored by us
				{ID: 6, InReplyToID: 5, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{"alice"}},
			},
			want: true,
		},
		{
			name: "our reply is the LATEST in the thread even though an earlier other-author comment exists: not pending",
			comments: []ghPRComment{
				{ID: 1, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{"alice"}},
				{ID: 2, InReplyToID: 1, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{ourLogin}},
			},
			want: false,
		},
		{
			name: "multiple threads: one resolved, one pending -> overall pending",
			comments: []ghPRComment{
				// Thread A (root 1): resolved — we spoke last.
				{ID: 1, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{"alice"}},
				{ID: 2, InReplyToID: 1, CreatedAt: t2, User: struct {
					Login string `json:"login"`
				}{ourLogin}},
				// Thread B (root 10): pending — alice spoke last.
				{ID: 10, CreatedAt: t1, User: struct {
					Login string `json:"login"`
				}{ourLogin}},
				{ID: 11, InReplyToID: 10, CreatedAt: t3, User: struct {
					Login string `json:"login"`
				}{"alice"}},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPendingCommentThread(tc.comments, ourLogin); got != tc.want {
				t.Errorf("hasPendingCommentThread() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHasPendingCommentThread_EmptyOurLoginNeverMatches documents the exact
// hazard resolveOurGHLogin's guard exists to prevent (agent-teams-huq7.1
// CONTRACT AMENDMENT): if an empty ourLogin ever reached this function, it
// would match NO comment as "ours" (no comment's User.Login is ever ""),
// so a thread with a genuine unanswered comment — alice posts, then bob
// replies after her, nobody from "our" side ever appears — reports
// not-pending regardless. That is exactly the silent false-negative that
// would wrongly authorize a backstop close; resolveOurGHLogin's job is to
// make sure defaultPendingReviewComment errors out before ever calling this
// function with such a login.
func TestHasPendingCommentThread_EmptyOurLoginNeverMatches(t *testing.T) {
	comments := []ghPRComment{
		{ID: 1, CreatedAt: "2026-07-21T10:00:00Z", User: struct {
			Login string `json:"login"`
		}{"alice"}},
		{ID: 2, InReplyToID: 1, CreatedAt: "2026-07-21T11:00:00Z", User: struct {
			Login string `json:"login"`
		}{"bob"}},
	}
	if got := hasPendingCommentThread(comments, ""); got != false {
		t.Errorf("hasPendingCommentThread(comments, \"\") = %v, want false (empty ourLogin matches no comment — the hazard the login guard prevents)", got)
	}
}

// ── hungPendingCommentProbe: cache + preflight lifecycle (S4) ───────────────

// TestHungPendingCommentProbe_CachesWithinTTL proves the probe is only
// invoked once for repeated evaluate calls on the same PR within
// hungPendingCommentTTL, and re-invoked once the cached entry goes stale.
func TestHungPendingCommentProbe_CachesWithinTTL(t *testing.T) {
	ctx := makeHungCtx(t, nil)
	pending := &fakePendingReviewComment{pending: false}
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	deps := hungTickDeps{
		now:                  fixedNow(now),
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return nil },
	}
	entry := hungScanEntry{ID: "at-1", ReviewPRURL: "https://github.com/acme/widget/pull/1"}

	p := newHungPendingCommentProbe(ctx, deps)
	if _, probed := p.evaluate(entry); !probed {
		t.Fatal("first evaluate: expected probed=true")
	}
	if _, probed := p.evaluate(entry); !probed {
		t.Fatal("second evaluate (same tick): expected probed=true")
	}
	if pending.calls != 1 {
		t.Errorf("raw probe calls = %d, want 1 (second call should hit the freshly-written cache)", pending.calls)
	}

	// Stale cache (TTL elapsed) -> re-probes.
	deps.now = fixedNow(now.Add(hungPendingCommentTTL + time.Minute))
	p2 := newHungPendingCommentProbe(ctx, deps)
	if _, probed := p2.evaluate(entry); !probed {
		t.Fatal("third evaluate (stale cache): expected probed=true")
	}
	if pending.calls != 2 {
		t.Errorf("raw probe calls = %d, want 2 (stale cache should force a fresh probe)", pending.calls)
	}
}

// TestHungPendingCommentProbe_PreflightFailureSkipsProbe proves a failing
// gh preflight degrades to probed=false (never authorizes a close) without
// ever calling the raw probe.
func TestHungPendingCommentProbe_PreflightFailureSkipsProbe(t *testing.T) {
	ctx := makeHungCtx(t, nil)
	pending := &fakePendingReviewComment{pending: false}
	deps := hungTickDeps{
		now:                  fixedNow(time.Now()),
		pendingReviewComment: pending.probe,
		ghPreflight:          func() error { return fmt.Errorf("gh not authenticated") },
	}
	entry := hungScanEntry{ID: "at-1", ReviewPRURL: "https://github.com/acme/widget/pull/1"}

	p := newHungPendingCommentProbe(ctx, deps)
	if _, probed := p.evaluate(entry); probed {
		t.Error("expected probed=false when the gh preflight fails")
	}
	if pending.calls != 0 {
		t.Errorf("raw probe calls = %d, want 0 (preflight failure must skip the probe entirely)", pending.calls)
	}
}
