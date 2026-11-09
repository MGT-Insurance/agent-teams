package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// fakeWatch records the watcher seams. By default resolve answers "at-init"
// and a held lock never happens.
type fakeWatch struct {
	resolved  []string
	resolveFn func(path string) (string, error)
	spawns    [][2]string // beadID, initiative
	spawnErr  error
	sends     []sentMail
	sendFn    func(n int) error // n is the 1-based send attempt
	attempts  int
	lockHeld  bool
}

type sentMail struct{ initiative, body, key string }

func newFakeWatch() *fakeWatch { return &fakeWatch{} }

func (f *fakeWatch) deps() *uiWatchDeps {
	return &uiWatchDeps{
		resolve: func(_ *cli.Context, path string) (string, error) {
			f.resolved = append(f.resolved, path)
			if f.resolveFn != nil {
				return f.resolveFn(path)
			}
			return "at-init", nil
		},
		spawn: func(_ *cli.Context, id, initiative string) error {
			f.spawns = append(f.spawns, [2]string{id, initiative})
			return f.spawnErr
		},
		send: func(_ *cli.Context, initiative, body, key string) error {
			f.attempts++
			if f.sendFn != nil {
				if err := f.sendFn(f.attempts); err != nil {
					return err
				}
			}
			f.sends = append(f.sends, sentMail{initiative, body, key})
			return nil
		},
		lock: func(path string) (func(), bool, error) {
			if f.lockHeld {
				return nil, false, nil
			}
			return followTryLock(path)
		},
	}
}

// watchState is the bead and comments one tick reads.
type watchState struct {
	bead     uiTestBead
	comments []uiTestComment
}

func tester(text string) uiTestComment {
	return uiTestComment{Author: "agent:tester", Text: text, CreatedAt: "2026-01-01T00:00:00Z"}
}

// watchRig serves one state per tick (the last repeats) and runs the child loop.
type watchRig struct {
	h      *uiHarness
	fw     *fakeWatch
	states []watchState
	cur    int
	ticks  int
	failRd func(tick int) error
}

func newWatchRig(t *testing.T, states ...watchState) *watchRig {
	t.Helper()
	r := &watchRig{h: newUIHarness(t, uiFullConfig), fw: newFakeWatch(), states: states}
	r.h.deps.watch = r.fw.deps()
	r.h.deps.bdRead = func(_ string, args ...string) ([]byte, error) {
		if args[0] == "show" {
			r.ticks++
			if r.failRd != nil {
				if err := r.failRd(r.ticks); err != nil {
					return nil, err
				}
			}
			if r.ticks > 1 && r.cur < len(r.states)-1 {
				r.cur++
			}
			return json.Marshal([]uiTestBead{r.states[r.cur].bead})
		}
		return json.Marshal(r.states[r.cur].comments)
	}
	r.h.deps.sleep = func(d time.Duration) {
		if len(r.h.slept) > 5000 {
			t.Fatal("loop did not stop")
		}
		r.h.slept = append(r.h.slept, d)
		r.h.clock = r.h.clock.Add(d)
	}
	return r
}

func (r *watchRig) runChild() error {
	return (&uiTestWatchKong{ID: "dsp-ui1", Initiative: "at-init", Child: true, deps: r.h.deps, watch: r.fw.deps()}).Run(r.h.ctx)
}

func (r *watchRig) bodies() []string {
	var b []string
	for _, m := range r.fw.sends {
		b = append(b, m.body)
	}
	return b
}

func blockedQ(q string) watchState {
	return watchState{uiBead("blocked", "type:ui-test"), []uiTestComment{tester(q)}}
}

func TestUITestStopEvent_DispatcherVectors(t *testing.T) {
	// Vectors from the Dispatcher's contract.test.ts (uiTestStopEvent).
	cases := []struct{ id, kind, q, want string }{
		{"dsp-1", "blocked", "", "ui-test dsp-1 blocked\nnext: ateam ui-test wait dsp-1 --out <dir>"},
		{"dsp-1", "done", "", "ui-test dsp-1 done\nnext: ateam ui-test wait dsp-1 --out <dir>"},
		{"dsp-1", "failed", "", "ui-test dsp-1 failed\nnext: ateam ui-test wait dsp-1 --out <dir>"},
		{"dsp-1", "question", "  Which user?  ",
			"ui-test dsp-1 question\nWhich user?\nnext: ateam ui-test wait dsp-1 --out <dir> | ateam ui-test reply dsp-1 --file <answer.md>"},
	}
	for _, c := range cases {
		if got := uiTestStopEvent(c.id, c.kind, c.q); got != c.want {
			t.Errorf("%s: got %q, want %q", c.kind, got, c.want)
		}
	}
	lines := strings.Split(uiTestStopEvent("b", "question", strings.Repeat("x", 2500)), "\n")
	if lines[1] != strings.Repeat("x", 2000) {
		t.Errorf("question not cut to 2000: len %d", len(lines[1]))
	}
	if lines[0] != "ui-test b question" {
		t.Errorf("line 1 = %q", lines[0])
	}
}

func TestUITestWatch_QuestionAnswerQuestionDoneSendsThreeThenExits(t *testing.T) {
	q := "question: which user?"
	r := newWatchRig(t,
		watchState{bead: uiBead("open", "type:ui-test")},
		blockedQ(q),
		blockedQ(q), // repeat tick: no new mail
		watchState{bead: uiBead("in_progress", "type:ui-test"), comments: []uiTestComment{tester(q), {Author: "eric@example.com", Text: "answer: alice"}}},
		blockedQ(q), // same text again after the answer
		watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed", CloseReason: "passed"}},
	)
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	qBody := uiTestStopEvent("dsp-ui1", "question", q)
	want := []string{qBody, qBody, uiTestStopEvent("dsp-ui1", "done", "")}
	if got := r.bodies(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bodies = %q, want %q", got, want)
	}
	if r.fw.sends[0].initiative != "at-init" {
		t.Errorf("initiative = %q", r.fw.sends[0].initiative)
	}
	if k := r.fw.sends[0].key; !strings.HasPrefix(k, "ui-test:dsp-ui1:question:") || len(k) != len("ui-test:dsp-ui1:question:")+12 {
		t.Errorf("dedup key = %q", k)
	}
}

func TestUITestWatch_BlockedWithoutQuestionSendsOnce(t *testing.T) {
	r := newWatchRig(t,
		watchState{bead: uiBead("blocked", "type:ui-test"), comments: []uiTestComment{tester("note: just a note")}},
		watchState{bead: uiBead("blocked", "type:ui-test"), comments: []uiTestComment{tester("note: just a note")}},
		watchState{bead: uiBead("blocked", "type:ui-test"), comments: []uiTestComment{tester("note: just a note")}},
		watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed"}},
	)
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	want := []string{uiTestStopEvent("dsp-ui1", "blocked", ""), uiTestStopEvent("dsp-ui1", "done", "")}
	if got := r.bodies(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bodies = %q, want %q", got, want)
	}
}

func TestUITestWatch_ClosedFailedSendsFailedAndExits(t *testing.T) {
	for name, bead := range map[string]uiTestBead{
		"close reason": {ID: "dsp-ui1", Status: "closed", CloseReason: "failed: boom"},
		"label":        {ID: "dsp-ui1", Status: "closed", CloseReason: "done", Labels: []string{"failed"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newWatchRig(t, watchState{bead: bead})
			if err := r.runChild(); err != nil {
				t.Fatal(err)
			}
			if got := r.bodies(); len(got) != 1 || got[0] != uiTestStopEvent("dsp-ui1", "failed", "") {
				t.Fatalf("bodies = %q", got)
			}
		})
	}
}

func TestUITestWatch_SendFailureRetriesNextTick(t *testing.T) {
	r := newWatchRig(t, blockedQ("question: which?"), blockedQ("question: which?"), watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed"}})
	r.fw.sendFn = func(n int) error {
		if n == 1 {
			return errors.New("mail down")
		}
		return nil
	}
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	// Attempt 1 failed; tick 2 re-sends the same question; then done.
	if r.fw.attempts != 3 || len(r.fw.sends) != 2 || !strings.Contains(r.fw.sends[0].body, "question") {
		t.Fatalf("attempts = %d, sends = %q", r.fw.attempts, r.bodies())
	}
	if !strings.Contains(r.h.stdout.String(), "send question failed: mail down") {
		t.Errorf("log = %q", r.h.stdout.String())
	}
}

func TestUITestWatch_ReadErrorLogsAndContinues(t *testing.T) {
	r := newWatchRig(t, watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed"}})
	r.failRd = func(tick int) error {
		if tick == 1 {
			return errors.New("bd exploded")
		}
		return nil
	}
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	if len(r.fw.sends) != 1 || !strings.Contains(r.h.stdout.String(), "read failed") {
		t.Fatalf("sends = %q, log = %q", r.bodies(), r.h.stdout.String())
	}
}

func TestUITestWatch_LockHeldExitsWithoutSending(t *testing.T) {
	r := newWatchRig(t, watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed"}})
	r.fw.lockHeld = true
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	if len(r.fw.sends) != 0 || r.ticks != 0 {
		t.Errorf("sends = %q, ticks = %d", r.bodies(), r.ticks)
	}
}

func TestUITestWatch_ChildCleansLockAndPidfile(t *testing.T) {
	r := newWatchRig(t, watchState{bead: uiTestBead{ID: "dsp-ui1", Status: "closed"}})
	r.fw.sendFn = func(int) error {
		// While the loop runs, the pidfile exists and a second lock fails.
		pid := filepath.Join(r.h.ctx.Home, "uitest", "dsp-ui1.pid")
		if _, err := os.Stat(pid); err != nil {
			t.Errorf("pidfile during run: %v", err)
		}
		if _, ok, _ := followTryLock(filepath.Join(r.h.ctx.Home, "uitest", "dsp-ui1.lock")); ok {
			t.Error("second lock succeeded while the child holds it")
		}
		return nil
	}
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"dsp-ui1.pid", "dsp-ui1.lock"} {
		if _, err := os.Stat(filepath.Join(r.h.ctx.Home, "uitest", n)); !os.IsNotExist(err) {
			t.Errorf("%s left behind (err %v)", n, err)
		}
	}
}

func TestUITestWatch_ExpiryMailsOnceAndExits(t *testing.T) {
	r := newWatchRig(t, watchState{bead: uiBead("in_progress", "type:ui-test")})
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	want := "ui-test dsp-ui1 expired\nnext: ateam ui-test watch dsp-ui1 --initiative at-init | ateam ui-test wait dsp-ui1 --out <dir>"
	if got := r.bodies(); len(got) != 1 || got[0] != want {
		t.Fatalf("bodies = %q", got)
	}
	if r.fw.sends[0].key != "ui-test:dsp-ui1:expired" {
		t.Errorf("key = %q", r.fw.sends[0].key)
	}
	if slept := time.Duration(len(r.h.slept)) * uiWatchTick; slept < uiWatchExpiry || slept > uiWatchExpiry+uiWatchTick {
		t.Errorf("exited after %s, want 24h", slept)
	}
}

func TestUITestWatch_ExpiredMailRetriesThenGivesUp(t *testing.T) {
	r := newWatchRig(t, watchState{bead: uiBead("in_progress", "type:ui-test")})
	r.fw.sendFn = func(int) error { return errors.New("mail down") }
	if err := r.runChild(); err != nil {
		t.Fatal(err)
	}
	if r.fw.attempts != uiWatchRetries {
		t.Errorf("attempts = %d, want %d", r.fw.attempts, uiWatchRetries)
	}
}

func TestUITestWatch_InRunSpawnsNothing(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_API_URL", "http://127.0.0.1:1")
	err := (&uiTestWatchKong{ID: "dsp-ui1", deps: h.deps, watch: fw.deps()}).Run(h.ctx)
	if err != nil || len(fw.spawns) != 0 || len(fw.resolved) != 0 {
		t.Fatalf("err = %v, spawns = %v, resolved = %v", err, fw.spawns, fw.resolved)
	}
	if !strings.Contains(h.stderr.String(), "a cloud run gets stop events from the Dispatcher") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

func TestUITestWatch_NotConfiguredExits5(t *testing.T) {
	h := newUIHarness(t, "")
	fw := newFakeWatch()
	err := (&uiTestWatchKong{ID: "dsp-ui1", deps: h.deps, watch: fw.deps()}).Run(h.ctx)
	if exitOf(err) != 5 || len(fw.spawns) != 0 {
		t.Errorf("exit = %d, spawns = %v", exitOf(err), fw.spawns)
	}
}

func TestUITestWatch_NoInitiativeExits1(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	fw.resolveFn = func(string) (string, error) { return "", nil }
	err := (&uiTestWatchKong{ID: "dsp-ui1", deps: h.deps, watch: fw.deps()}).Run(h.ctx)
	if exitOf(err) != 1 || err == nil || !strings.Contains(err.Error(), "no open initiative owns") || !strings.Contains(err.Error(), "pass --initiative") {
		t.Errorf("err = %v (exit %d)", err, exitOf(err))
	}
	if len(fw.spawns) != 0 {
		t.Errorf("spawned: %v", fw.spawns)
	}
}

func TestUITestWatch_SpawnsWithResolvedOrGivenInitiative(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	cwd, _ := os.Getwd()
	if err := (&uiTestWatchKong{ID: "dsp-ui1", deps: h.deps, watch: fw.deps()}).Run(h.ctx); err != nil {
		t.Fatal(err)
	}
	if len(fw.resolved) != 1 || fw.resolved[0] != cwd || fw.spawns[0] != [2]string{"dsp-ui1", "at-init"} {
		t.Errorf("resolved = %v, spawns = %v", fw.resolved, fw.spawns)
	}
	if h.stdout.String() != "watching dsp-ui1 for at-init\n" {
		t.Errorf("stdout = %q", h.stdout.String())
	}
	fw2 := newFakeWatch()
	if err := (&uiTestWatchKong{ID: "dsp-ui1", Initiative: "at-given", deps: h.deps, watch: fw2.deps()}).Run(h.ctx); err != nil {
		t.Fatal(err)
	}
	if len(fw2.resolved) != 0 || fw2.spawns[0][1] != "at-given" {
		t.Errorf("resolved = %v, spawns = %v", fw2.resolved, fw2.spawns)
	}
}

func TestUITestFile_StartsWatcherWithResolvedInitiative(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	h.deps.watch = fw.deps()
	if err := h.file("- check it"); err != nil {
		t.Fatal(err)
	}
	if h.stdout.String() != "dsp-ui1\n" || len(fw.spawns) != 1 || fw.spawns[0] != [2]string{"dsp-ui1", "at-init"} {
		t.Errorf("stdout = %q, spawns = %v", h.stdout.String(), fw.spawns)
	}
	if fw.resolved[0] != "/wt" {
		t.Errorf("resolved %v, want the worktree first", fw.resolved)
	}
}

func TestUITestFile_InitiativeBeatsWorktreeBeatsCwd(t *testing.T) {
	cwd, _ := os.Getwd()
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	h.deps.watch = fw.deps()
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "m", ItemsFile: h.itemsFile("x"), Initiative: "at-flag", deps: h.deps}
	if err := cmd.Run(h.ctx); err != nil {
		t.Fatal(err)
	}
	if len(fw.resolved) != 0 || fw.spawns[0][1] != "at-flag" {
		t.Errorf("flag: resolved = %v, spawns = %v", fw.resolved, fw.spawns)
	}

	// No flag: the worktree wins when it resolves.
	fw = newFakeWatch()
	h.deps.watch = fw.deps()
	fw.resolveFn = func(p string) (string, error) {
		if p == "/wt" {
			return "at-wt", nil
		}
		return "at-cwd", nil
	}
	if err := h.file("x"); err != nil {
		t.Fatal(err)
	}
	if fw.spawns[0][1] != "at-wt" || len(fw.resolved) != 1 {
		t.Errorf("worktree: resolved = %v, spawns = %v", fw.resolved, fw.spawns)
	}

	// The worktree owns nothing: the cwd is next.
	fw = newFakeWatch()
	h.deps.watch = fw.deps()
	fw.resolveFn = func(p string) (string, error) {
		if p == cwd {
			return "at-cwd", nil
		}
		return "", nil
	}
	if err := h.file("x"); err != nil {
		t.Fatal(err)
	}
	if fw.spawns[0][1] != "at-cwd" || fw.resolved[0] != "/wt" || fw.resolved[1] != cwd {
		t.Errorf("cwd: resolved = %v, spawns = %v", fw.resolved, fw.spawns)
	}
}

func TestUITestFile_WatcherFailureKeepsExit0AndID(t *testing.T) {
	cases := map[string]func(*fakeWatch){
		"no initiative": func(f *fakeWatch) { f.resolveFn = func(string) (string, error) { return "", nil } },
		"resolve error": func(f *fakeWatch) { f.resolveFn = func(string) (string, error) { return "", errors.New("bd down") } },
		"spawn error":   func(f *fakeWatch) { f.spawnErr = errors.New("fork failed") },
	}
	for name, arm := range cases {
		t.Run(name, func(t *testing.T) {
			h := newUIHarness(t, uiFullConfig)
			fw := newFakeWatch()
			arm(fw)
			h.deps.watch = fw.deps()
			if err := h.file("x"); err != nil {
				t.Fatalf("file failed: %v", err)
			}
			if h.stdout.String() != "dsp-ui1\n" {
				t.Errorf("stdout = %q", h.stdout.String())
			}
			if !strings.Contains(h.stderr.String(), "ui-test file: no stop watcher (") || !strings.Contains(h.stderr.String(), "); run: ateam ui-test watch dsp-ui1\n") {
				t.Errorf("stderr = %q", h.stderr.String())
			}
		})
	}
}

func TestUITestFile_InRunNeverStartsWatcher(t *testing.T) {
	// In-run filing returns before the local path; watch deps must stay untouched.
	h := newUIHarness(t, uiFullConfig)
	fw := newFakeWatch()
	h.deps.watch = fw.deps()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_API_URL", "http://127.0.0.1:1")
	_ = h.file("x") // the run-tools call fails against the dead port; only the watcher matters
	if len(fw.spawns) != 0 || len(fw.resolved) != 0 {
		t.Errorf("spawns = %v, resolved = %v", fw.spawns, fw.resolved)
	}
}

func TestUITestWatch_DefaultResolveUsesResolveInitiativeRule(t *testing.T) {
	wt := t.TempDir()
	issues := []bd.Issue{
		{ID: "at-other", Description: "worktree: /nowhere\n"},
		{ID: "at-mine", Description: "worktree: " + wt + "\n"},
	}
	if err := os.Mkdir(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, _, _ := makeCtx(&fakeBD{runJSONFn: func(dst any, args ...string) error {
		b, _ := json.Marshal(issues)
		return json.Unmarshal(b, dst)
	}}, t.TempDir())
	if got, err := resolveUIWatchInitiative(ctx, filepath.Join(wt, "sub")); err != nil || got != "at-mine" {
		t.Errorf("got %q, %v; want at-mine", got, err)
	}
	if got, err := resolveUIWatchInitiative(ctx, "/elsewhere"); err != nil || got != "" {
		t.Errorf("got %q, %v; want none", got, err)
	}
}

func TestUITestWatch_RegisteredWithFrozenFlags(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterUITestKong(parser)
	kctx, err := parser.Parse([]string{"ui-test", "watch", "dsp-1", "--initiative", "at-x", "--child"})
	if err != nil {
		t.Fatal(err)
	}
	if got := kctx.Command(); got != "ui-test watch <bead-id>" {
		t.Errorf("command = %q", got)
	}
	if _, err := parser.Parse([]string{"ui-test", "file", "--worktree", "/wt", "--scope", "m", "--items-file", "/i", "--initiative", "at-x"}); err != nil {
		t.Errorf("file --initiative: %v", err)
	}
}
