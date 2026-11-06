package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeCountingHelper replaces the fake outbox-append with one that appends
// each stdin line to $sup/lines and exits 2 from call number failAt on
// (0 = never).
func writeCountingHelper(t *testing.T, sup string, failAt int) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
n=$(cat "%[1]s/count" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "%[1]s/count"
if [ %[2]d -gt 0 ] && [ $n -ge %[2]d ]; then echo boom >&2; exit 2; fi
cat >> "%[1]s/lines"
`, sup, failAt)
	if err := os.WriteFile(filepath.Join(sup, "bin", "outbox-append"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (e *runInboxEnv) putMany(n int) {
	for i := 1; i <= n; i++ {
		e.putInbox(strconv.Itoa(i), fmt.Sprintf(`{"seq":%d,"author":"a","text":"t","at":"x"}`, i))
	}
}

func (e *runInboxEnv) lines() []string {
	data, _ := os.ReadFile(filepath.Join(e.sup, "lines"))
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestInboxRun_AckChunksAt100(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	writeCountingHelper(t, e.sup, 0)
	e.putMany(101)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	lines := e.lines()
	if len(lines) != 2 {
		t.Fatalf("got %d ack lines, want 2: %v", len(lines), lines)
	}
	if !strings.HasSuffix(lines[1], `"seqs":[101]}`) || strings.Count(lines[0], ",") != 100 { // 99 between seqs plus one after "ack"
		t.Errorf("chunking wrong: first has %d commas, second %q", strings.Count(lines[0], ","), lines[1])
	}
	if n := len(listInboxNames(e.sup)); n != 0 {
		t.Errorf("%d files left in inbox", n)
	}
}

func TestInboxRun_SecondChunkFailureMovesOnlyFirst100(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	writeCountingHelper(t, e.sup, 2)
	e.putMany(101)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	if !strings.Contains(e.stderr.String(), "ack failed") {
		t.Errorf("stderr = %q", e.stderr.String())
	}
	left := listInboxNames(e.sup)
	if len(left) != 1 || left[0] != "101.json" {
		t.Errorf("files left = %v, want only 101.json", left)
	}
	if !e.exists("inbox/read/100.json") || e.exists("inbox/read/101.json") {
		t.Error("read/ should hold 1..100 and not 101")
	}
}

func TestInboxRun_OnlyRepeatsPrintsNoReminder(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.putRead("5")
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stdout.String(); got != "no unread mail\n" {
		t.Errorf("stdout = %q, want no reminder", got)
	}
	if got, want := e.helperStdin(), `{"op":"ack","seqs":[5]}`+"\n"; got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
	if e.exists("inbox/5.json") {
		t.Error("repeat copy must be removed")
	}
}

func TestCloudBootInboxRingFailureIsSoft(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\n"
	e.common.IdlePolls = 1
	e.leadSeq("sid-1", "idle")
	// A regular file where the bin dir must go makes writeInboxRing fail.
	if err := os.WriteFile(filepath.Join(e.sup, "bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantExit(t, e.start(), cloudBootParkExit)
	if len(e.tmux) == 0 {
		t.Error("boot did not continue to the tmux launch")
	}
	if !strings.Contains(e.ctx.Stderr.(interface{ String() string }).String(), "inbox-ring setup failed") {
		t.Errorf("no warning on stderr")
	}
}
