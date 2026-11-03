package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUITestFileBead_SetsJournalEnvAndVerifiesRow(t *testing.T) {
	log := installFakeBD(t, "dsp-ui9", "issue_id\ndsp-ui9\n")
	id, err := defaultUITestFileBead("/dsp", "me@example.com", "ui-test: x", []string{"type:ui-test"}, "body")
	if err != nil {
		t.Fatalf("defaultUITestFileBead: %v", err)
	}
	if id != "dsp-ui9" {
		t.Errorf("id = %q", id)
	}
	got := readLog(t, log)
	for _, want := range []string{"create tls=1 journal=1", "sql tls=1 journal=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("calls %q missing %q", got, want)
		}
	}
}

func TestUITestFileBead_MissingJournalRowExits1NamesBeadAndClosesIt(t *testing.T) {
	log := installFakeBD(t, "dsp-ui5", "")
	h := newUIHarness(t, uiFullConfig)
	h.deps.fileBead = defaultUITestFileBead
	err := h.file("- check it\n")
	if exitOf(err) != 1 {
		t.Fatalf("exit = %d, want 1 (%v)", exitOf(err), err)
	}
	if !strings.Contains(err.Error(), "dsp-ui5") || !strings.Contains(err.Error(), "closed") {
		t.Errorf("err = %v, want it to name the bead and say it was closed", err)
	}
	if got := readLog(t, log); !strings.Contains(got, "close tls=1 journal=1") {
		t.Errorf("calls %q missing the orphan close", got)
	}
	if h.stdout.String() != "" {
		t.Errorf("stdout = %q, want no bead id on failure", h.stdout.String())
	}
}

func TestUITestFileBead_RejectsUnsafeID(t *testing.T) {
	log := installFakeBD(t, "x'; drop table t; --", "")
	if _, err := defaultUITestFileBead("/dsp", "me@example.com", "t", nil, "b"); err == nil {
		t.Fatal("want error for an id that is not a bd id")
	}
	if strings.Contains(readLog(t, log), "sql ") {
		t.Error("an unsafe id must not reach the sql query")
	}
}

func TestUITest_UnreadableItemsFileIsExit1NotUsage(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	cmd := &uiTestFileKong{Worktree: "/wt", Scope: "mithril", ItemsFile: filepath.Join(t.TempDir(), "missing.md"), deps: h.deps}
	err := cmd.Run(h.ctx)
	if exitOf(err) != 1 {
		t.Errorf("exit = %d, want 1 (%v)", exitOf(err), err)
	}
}

const uiSecretArtifactLine = "artifact: 01-home.png https://dispatcher.example/api/runs/r/artifacts/01-home.png?exp=1&sig=SECRETSIG"

func TestUITest_FailureMDDropsSignedArtifactURLs(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("failed")}
	h.comments = []uiTestComment{{Author: "job:ui-test", CreatedAt: "2026-11-02T13:49:40Z",
		Text: "attempt 1: FAILED (exit 1)\nrun: " + uiRunID + "\nscreenshots: 1\n" + uiSecretArtifactLine}}
	out, err := h.wait(time.Minute)
	if exitOf(err) != 2 {
		t.Fatalf("exit = %d, want 2 (%v)", exitOf(err), err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "failure.md"))
	if strings.Contains(string(data), "SECRETSIG") || strings.Contains(string(data), "https://") {
		t.Errorf("failure.md leaks the signed URL:\n%s", data)
	}
	if !strings.Contains(string(data), "artifact: 01-home.png <url redacted>") {
		t.Errorf("failure.md lost the artifact name:\n%s", data)
	}
	if strings.Contains(h.stdout.String(), "SECRETSIG") {
		t.Errorf("stdout leaks the signed URL: %q", h.stdout.String())
	}
}

func TestUITest_QuestionStdoutDropsSignedArtifactURLs(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{{ID: "dsp-ui1", Status: "blocked"}}
	h.comments = []uiTestComment{{Author: "agent:tester", CreatedAt: "2026-11-02T13:59:00Z",
		Text: "question: which user?\n" + uiSecretArtifactLine}}
	if _, err := h.wait(time.Minute); exitOf(err) != 4 {
		t.Fatalf("exit = %d, want 4", exitOf(err))
	}
	if got := h.stdout.String(); strings.Contains(got, "SECRETSIG") || !strings.Contains(got, "question: which user?") {
		t.Errorf("stdout = %q", got)
	}
}

func TestUITest_CloseReasonMatchIsExactFailed(t *testing.T) {
	h := newUIHarness(t, uiFullConfig)
	h.polls = []uiTestBead{closedBead("passed, 0 failed checks")}
	h.fakePNG()
	h.comments = withFinalizer(uiSignedLink("01-home.png", uiFarExp))
	if _, err := h.wait(5 * time.Minute); err != nil {
		t.Errorf("a close reason containing 'failed' mid-text lost the verdict: %v", err)
	}

	for _, reason := range []string{"failed", "Failed", " failed: job exited 1"} {
		h := newUIHarness(t, uiFullConfig)
		h.polls = []uiTestBead{closedBead(reason)}
		h.comments = withFinalizer(uiSignedLink("01-home.png", uiFarExp))
		if _, err := h.wait(5 * time.Minute); exitOf(err) != 2 {
			t.Errorf("close reason %q: exit = %d, want 2", reason, exitOf(err))
		}
	}
}
