package verbs

import (
	"fmt"
	"os"
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

	// Seed an anchor whose StuckSince is already past the 15m threshold so
	// the very first tick observes Hung=true (scanHung only sets a fresh
	// StuckSince when no anchor exists yet).
	t0 := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	seedStuckSince := t0.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
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
	seedStuckSince := t0.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
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
	seedStuckSince := t0.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
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
	seedStuckSince := t0.Add(-20 * time.Minute).UTC().Format(time.RFC3339)
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

	// Re-entry: idle/STUCK again, but freshly stuck (elapsed well under the
	// 15m threshold) -> not yet Hung, ladder must not fire at all.
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

	// Advance past the threshold on the NEW episode -> ladder restarts at
	// attempt 1, proving the previous episode's count was not reused.
	deps.now = fixedNow(t0.Add(2*time.Minute + 16*time.Minute))
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
