// i7i5_verify_test.go: core-path verification tests for i7i5.2 changes.
package verbs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── extractEpicID ─────────────────────────────────────────────────────────────

func TestExtractEpicID_Found(t *testing.T) {
	body := "problem: foo\nrepo: /some/path\nepic: at-epic99\nmode: bg\n"
	got := extractEpicID(body)
	if got != "at-epic99" {
		t.Errorf("extractEpicID = %q, want %q", got, "at-epic99")
	}
}

func TestExtractEpicID_Missing(t *testing.T) {
	body := "problem: foo\nrepo: /some/path\nmode: bg\n"
	got := extractEpicID(body)
	if got != "" {
		t.Errorf("extractEpicID = %q, want empty", got)
	}
}

func TestExtractEpicID_TrimsTrailingWhitespace(t *testing.T) {
	body := "epic: at-epic1  \r\n"
	got := extractEpicID(body)
	if got != "at-epic1" {
		t.Errorf("extractEpicID = %q, want %q", got, "at-epic1")
	}
}

// ── appendEpicToBody (new signature) ─────────────────────────────────────────

func TestAppendEpicToBody_ReturnsEpicID(t *testing.T) {
	f := makeTempFile(t, "problem: test\nrepo: /some/repo\n")

	var stdout, stderr strings.Builder
	ctx := &cli.Context{
		Home:   t.TempDir(),
		BD:     &fakeBD{},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	creator := func(_, _ string) (string, error) { return "at-epic42", nil }
	path, epicID, cleanup := appendEpicToBody(ctx, f, "T", creator)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup on success")
	}
	defer cleanup()
	if epicID != "at-epic42" {
		t.Errorf("epicID = %q, want %q", epicID, "at-epic42")
	}
	if path == "" {
		t.Error("path should be non-empty on success")
	}
}

func TestAppendEpicToBody_NoRepoLine_ReturnsEmpty(t *testing.T) {
	f := makeTempFile(t, "problem: test\n")

	var stdout, stderr strings.Builder
	ctx := &cli.Context{
		Home:   t.TempDir(),
		BD:     &fakeBD{},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	creator := func(_, _ string) (string, error) { return "at-epic1", nil }
	path, epicID, cleanup := appendEpicToBody(ctx, f, "T", creator)
	if cleanup != nil {
		t.Error("expected nil cleanup when no repo line")
	}
	if epicID != "" {
		t.Errorf("epicID = %q, want empty", epicID)
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

// ── updateDescriptionKong ─────────────────────────────────────────────────────

func TestUpdateDescription_CallsBDUpdate(t *testing.T) {
	f := makeTempFile(t, "new description")
	ctx, calls := newCtx(t, []fakeResp{{stdout: ""}})
	cmd := &updateDescriptionKong{ID: "at-init1", File: f}
	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
	call := (*calls)[0]
	assertArgs(t, *calls, 0, []string{"update", "at-init1", "--body-file=" + f})
	_ = call
}

func TestUpdateDescription_ParsedByRegisterWriteKong(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	f := makeTempFile(t, "desc")
	_, parseErr := p.Parse([]string{"update-description", "at-1", "--file", f})
	if parseErr != nil {
		t.Fatalf("parse error: %v", parseErr)
	}
}

func TestUpdateDescription_MissingFile(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"update-description", "at-1"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing --file")
	}
}

// ── registerKong with epicID labels epic (fail-soft) ─────────────────────────

func TestRegister_LabelEpicFailSoft(t *testing.T) {
	// Body has a repo: line so appendEpicToBody succeeds.
	bodyFile := makeTempFile(t, "problem: test\nrepo: /some/repo\n")
	issue := bd.Issue{ID: "at-new1", Title: "T"}
	jsonOut, _ := json.Marshal(issue)

	ctx, calls := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})
	cmd := &registerKong{
		Title: "T",
		File:  bodyFile,
		createEpic: func(_, _ string) (string, error) {
			return "at-epic7", nil
		},
	}
	// Label step calls real exec.Command("bd", ...) which will fail since bd
	// is not pointed at a real repo — that's fine, it's fail-soft. The verb
	// must still return nil and print the initiative ID.
	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(stdoutOf(ctx))
	if out != "at-new1" {
		t.Errorf("stdout = %q, want %q", out, "at-new1")
	}
	// BD was called once (the create call). The label step goes through exec directly.
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
}
