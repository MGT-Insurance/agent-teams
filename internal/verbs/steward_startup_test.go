package verbs

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

const startupLearningKey = "steward:hot:ledger-rule"

// startupCtx is a hosted ledger and learnings store holding one ledger record
// and one steward learning whose value is body.
func startupCtx(t *testing.T, body string) (*memBD, func() (string, string, error)) {
	t.Helper()
	m := newMemBD()
	m.mem[startupLearningKey] = body
	run := func() (string, string, error) {
		ctx, out, errOut := hostedLedgerCtx(t, m)
		err := (&stewardStartupKong{}).Run(ctx)
		return out.String(), errOut.String(), err
	}
	return m, run
}

func TestStewardStartup_OrderAndByteIdenticalLearnings(t *testing.T) {
	m, run := startupCtx(t, "RULE: keep it whole.")
	ctx, _, _ := hostedLedgerCtx(t, m)
	recordLedger(t, ctx, "merge-approval", "at-1", "accepted", "")

	got, _, err := run()
	if err != nil {
		t.Fatal(err)
	}

	var stats, learnings bytes.Buffer
	sctx, _, _ := hostedLedgerCtx(t, m)
	sctx.Stdout = &stats
	if err := (&stewardLedgerStatsKong{}).Run(sctx); err != nil {
		t.Fatal(err)
	}
	lctx, _, _ := hostedLedgerCtx(t, m)
	lctx.Stdout = &learnings
	if err := runLearnings(lctx, "steward"); err != nil {
		t.Fatal(err)
	}

	want := "## Ledger rules\n" + stewardRulesIntro + "\n\n" + stewardSharedLedgerRules +
		"\n## Outsider rules\n" + stewardOutsiderRules +
		"\n## Autonomy rules\n" + stewardAutonomyRules +
		"\n## Ledger stats\n" + stats.String() +
		"\n## Steward learnings\n" + learnings.String()
	if got != want {
		t.Errorf("startup output:\n got %q\nwant %q", got, want)
	}
	rules, st, le := strings.Index(got, "## Ledger rules"), strings.Index(got, "## Ledger stats"), strings.Index(got, "## Steward learnings")
	if !(rules >= 0 && rules < st && st < le) {
		t.Errorf("sections out of order: %d %d %d", rules, st, le)
	}
	if !strings.HasSuffix(got, learnings.String()) {
		t.Error("learnings are not the exact tail of the output")
	}
}

// startupSize returns the output size when the learning body is n bytes.
func startupSize(t *testing.T, n int) (int, error) {
	t.Helper()
	_, run := startupCtx(t, strings.Repeat("x", n))
	out, _, err := run()
	return len(out), err
}

func TestStewardStartup_LimitIsInclusive(t *testing.T) {
	// Find the body size that makes the output exactly the limit. The learnings
	// header repeats the payload size, so its digit count shifts the total.
	n := stewardStartupLimit - 20000
	for i := 0; i < 5; i++ {
		size, err := startupSize(t, n)
		if err != nil {
			t.Fatalf("size probe: %v", err)
		}
		if size == stewardStartupLimit {
			break
		}
		n += stewardStartupLimit - size
	}

	_, run := startupCtx(t, strings.Repeat("x", n))
	out, _, err := run()
	if err != nil || len(out) != stewardStartupLimit {
		t.Fatalf("exactly at the limit: len %d, err %v; want %d bytes and no error", len(out), err, stewardStartupLimit)
	}

	_, run = startupCtx(t, strings.Repeat("x", n+1))
	out, errOut, err := run()
	if err == nil || out != "" {
		t.Fatalf("one byte over: stdout %d bytes, err %v; want empty stdout and an error", len(out), err)
	}
	want := "ateam steward startup: output is 61441 bytes, over the 61440-byte limit; condense steward learnings"
	if err.Error() != want || errOut != "" {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestStewardStartup_OverLimitFixturePrintsNothing(t *testing.T) {
	_, run := startupCtx(t, strings.Repeat("y", 2*stewardStartupLimit))
	out, _, err := run()
	if err == nil || out != "" {
		t.Fatalf("stdout %d bytes, err %v; want empty stdout and an error", len(out), err)
	}
	if !strings.Contains(err.Error(), "over the 61440-byte limit; condense steward learnings") {
		t.Errorf("error = %v", err)
	}
}

func TestStewardStartup_RulesIntroIsPinned(t *testing.T) {
	const want = "These ledger rules are shared by the local and cloud stewards. Section numbers, step numbers, and § references come from the local skill; a cloud steward can ignore them, and the record step below is the steward-reply handler they point to."
	if stewardRulesIntro != want {
		t.Errorf("stewardRulesIntro = %q, want %q", stewardRulesIntro, want)
	}
	_, run := startupCtx(t, "RULE: x.")
	out, _, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "## Ledger rules\n"+want+"\n\n") {
		t.Errorf("startup output does not lead with the intro:\n%.300s", out)
	}
	if !strings.Contains(stewardSharedLedgerRules, "3. Record the verdict:") {
		t.Error("the intro points at a record step the shared rules no longer contain")
	}
}

func TestStewardStartup_UnreachableStorePrintsNothing(t *testing.T) {
	m, run := startupCtx(t, "RULE: keep it whole.")
	m.err = errors.New(unreachableMsg)
	out, _, err := run()
	if err == nil || out != "" {
		t.Fatalf("stdout %q, err %v; want empty stdout and an error", out, err)
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %v", err)
	}
}

func TestStewardStartup_LearningsNoticeIsAnError(t *testing.T) {
	// The ledger and learnings share one client in these fixtures, so a
	// failing client stops at the ledger read. Call the learnings capture
	// directly to cover the "continuing WITHOUT" path.
	m := newMemBD()
	m.err = errors.New(unreachableMsg)
	ctx, out, _ := remoteLedgerCtx(t, m)
	text, err := captureStewardLearnings(ctx)
	if err == nil || text != "" || out.String() != "" {
		t.Fatalf("text %q, stdout %q, err %v; want an error and no text", text, out.String(), err)
	}
	if !strings.Contains(err.Error(), "learnings store unreachable") {
		t.Errorf("error = %v", err)
	}
}

func TestStewardStartup_LearningBodyMentioningNoticeStillStarts(t *testing.T) {
	body := "RULE: when the store says [learnings store unreachable: " + learnContinuingWithout + " steward learnings], escalate."
	_, run := startupCtx(t, body)
	out, _, err := run()
	if err != nil {
		t.Fatalf("startup failed on a learning that quotes the notice: %v", err)
	}
	if !strings.Contains(out, body) {
		t.Errorf("startup output does not contain the learning body:\n%s", out)
	}
}

func TestStewardSharedLedgerIsSingleSourced(t *testing.T) {
	const dir = "../../promptsrc/agent-teams/skills/steward/"
	skill, err := os.ReadFile("../../plugins/agent-teams/skills/steward/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var joined []string
	for _, name := range []string{"recall", "pending", "record", "discipline", "graduation"} {
		slice, err := os.ReadFile(dir + "shared-ledger-" + name + ".md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(skill), string(slice)) {
			t.Errorf("steward SKILL.md does not contain shared-ledger-%s.md verbatim", name)
		}
		joined = append(joined, string(slice))
	}
	// Slices are joined by one blank line; each ends in a newline.
	if want := strings.Join(joined, "\n"); stewardSharedLedgerRules != want {
		t.Errorf("embedded ledger rules differ from the promptsrc slices; run prompt-sync write\n got %q\nwant %q", stewardSharedLedgerRules, want)
	}
}
