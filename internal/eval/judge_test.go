package eval

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- synthetic git repo helpers --------------------------------------------
//
// These build a real, throwaway git repo per test so gitDiff and
// runBuildCheck exercise real git/shell exec, not a mocked seam — only the
// LLM call is faked (see judger.llm), per grft.5's "no real LLM calls in
// unit tests" instruction.

func newSyntheticRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "judge-test@example.com")
	runGit(t, dir, "config", "user.name", "Judge Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	writeFiles(t, dir, files)
	commitAll(t, dir, "initial")
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
}

func tagHEAD(t *testing.T, dir, tag string) {
	t.Helper()
	runGit(t, dir, "tag", tag)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// ---- Judge: layered grading (objective floor + fake LLM) -------------------

func TestJudge_CorrectFix_FloorPassHighCorrectness(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"status.txt": "buggy\n"})
	tagHEAD(t, dir, "fixture-start")
	writeFiles(t, dir, map[string]string{"status.txt": "fixed\n"})
	commitAll(t, dir, "fix the bug")

	const fakeResp = `{"criteriaResults":[{"criterion":"status.txt contains fixed","met":true,"note":"it does"}],"correctnessScore":0.95,"rationale":"the fix was applied correctly"}`
	j := judger{llm: func(prompt string) (string, error) {
		if !strings.Contains(prompt, "status.txt contains fixed") {
			t.Fatalf("prompt missing acceptance criterion, got: %s", prompt)
		}
		if !strings.Contains(prompt, "+fixed") {
			t.Fatalf("prompt missing diff content, got: %s", prompt)
		}
		return fakeResp, nil
	}}

	m := RunManifest{WorktreePath: dir}
	task := TaskSpec{
		FixtureRef:         "fixture-start",
		AcceptanceCriteria: []string{"status.txt contains fixed"},
		BuildCheck:         "grep -q fixed status.txt",
	}

	got, err := j.judge(m, task)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if !got.ObjectiveFloorPass {
		t.Fatalf("ObjectiveFloorPass = false, want true")
	}
	if got.CorrectnessScore != 0.95 {
		t.Fatalf("CorrectnessScore = %v, want 0.95", got.CorrectnessScore)
	}
	if len(got.CriteriaResults) != 1 || !got.CriteriaResults[0].Met {
		t.Fatalf("CriteriaResults = %+v, want one Met=true entry", got.CriteriaResults)
	}
}

func TestJudge_UnfixedBuggy_FloorFailLowCorrectness(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"status.txt": "buggy\n"})
	tagHEAD(t, dir, "fixture-start")
	// No further commit: HEAD == fixture-start, i.e. the run produced no fix.

	const fakeResp = `{"criteriaResults":[{"criterion":"status.txt contains fixed","met":false,"note":"still says buggy"}],"correctnessScore":0,"rationale":"no changes were made"}`
	j := judger{llm: func(prompt string) (string, error) { return fakeResp, nil }}

	m := RunManifest{WorktreePath: dir}
	task := TaskSpec{
		FixtureRef:         "fixture-start",
		AcceptanceCriteria: []string{"status.txt contains fixed"},
		BuildCheck:         "grep -q fixed status.txt",
	}

	got, err := j.judge(m, task)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if got.ObjectiveFloorPass {
		t.Fatalf("ObjectiveFloorPass = true, want false")
	}
	if got.CorrectnessScore != 0 {
		t.Fatalf("CorrectnessScore = %v, want 0", got.CorrectnessScore)
	}
	if len(got.CriteriaResults) != 1 || got.CriteriaResults[0].Met {
		t.Fatalf("CriteriaResults = %+v, want one Met=false entry", got.CriteriaResults)
	}
}

func TestJudge_MalformedLLMResponse_ReturnsErrorNotZero(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"status.txt": "buggy\n"})
	tagHEAD(t, dir, "fixture-start")

	j := judger{llm: func(prompt string) (string, error) { return "this is not json at all", nil }}

	m := RunManifest{WorktreePath: dir}
	task := TaskSpec{FixtureRef: "fixture-start", AcceptanceCriteria: []string{"anything"}, BuildCheck: "true"}

	got, err := j.judge(m, task)
	if err == nil {
		t.Fatalf("judge: want error for malformed LLM response, got nil result=%+v", got)
	}
	if !reflect.DeepEqual(got, JudgeResult{}) {
		t.Fatalf("judge: want zero-valued JudgeResult alongside the error, got %+v", got)
	}
}

func TestJudge_LLMCallError_PropagatesErrorNotZero(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"status.txt": "buggy\n"})
	tagHEAD(t, dir, "fixture-start")

	wantErr := "llm unavailable"
	j := judger{llm: func(prompt string) (string, error) { return "", errors.New(wantErr) }}

	m := RunManifest{WorktreePath: dir}
	task := TaskSpec{FixtureRef: "fixture-start", AcceptanceCriteria: []string{"anything"}, BuildCheck: "true"}

	got, err := j.judge(m, task)
	if err == nil {
		t.Fatalf("judge: want error when llm call fails, got nil result=%+v", got)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("judge: error %q does not wrap underlying llm error %q", err.Error(), wantErr)
	}
	if !reflect.DeepEqual(got, JudgeResult{}) {
		t.Fatalf("judge: want zero-valued JudgeResult alongside the error, got %+v", got)
	}
}

// ---- parseJudgeResponse: strict-parse robustness ---------------------------

func TestParseJudgeResponse_StripsMarkdownFence(t *testing.T) {
	raw := "```json\n{\"criteriaResults\":[],\"correctnessScore\":0.5,\"rationale\":\"ok\"}\n```"
	resp, err := parseJudgeResponse(raw)
	if err != nil {
		t.Fatalf("parseJudgeResponse: %v", err)
	}
	if resp.CorrectnessScore != 0.5 {
		t.Fatalf("CorrectnessScore = %v, want 0.5", resp.CorrectnessScore)
	}
}

func TestParseJudgeResponse_MalformedJSON_Errors(t *testing.T) {
	if _, err := parseJudgeResponse("not json"); err == nil {
		t.Fatalf("parseJudgeResponse: want error for non-JSON input, got nil")
	}
}

func TestParseJudgeResponse_Empty_Errors(t *testing.T) {
	if _, err := parseJudgeResponse("   "); err == nil {
		t.Fatalf("parseJudgeResponse: want error for empty input, got nil")
	}
}

// ---- runBuildCheck: objective floor, real exec -----------------------------

func TestRunBuildCheck_ExitZero_Pass(t *testing.T) {
	pass, err := runBuildCheck(t.TempDir(), "true")
	if err != nil {
		t.Fatalf("runBuildCheck: %v", err)
	}
	if !pass {
		t.Fatalf("pass = false, want true")
	}
}

func TestRunBuildCheck_NonzeroExit_FailNotError(t *testing.T) {
	pass, err := runBuildCheck(t.TempDir(), "false")
	if err != nil {
		t.Fatalf("runBuildCheck: want nil error for a normal nonzero exit, got %v", err)
	}
	if pass {
		t.Fatalf("pass = true, want false")
	}
}

func TestRunBuildCheck_BadDir_ReturnsError(t *testing.T) {
	if _, err := runBuildCheck(filepath.Join(t.TempDir(), "does-not-exist"), "true"); err == nil {
		t.Fatalf("runBuildCheck: want error for nonexistent dir, got nil")
	}
}

// ---- gitDiff -----------------------------------------------------------------

func TestGitDiff_ReturnsChangesSinceRef(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"a.txt": "one\n"})
	tagHEAD(t, dir, "start")
	writeFiles(t, dir, map[string]string{"a.txt": "two\n"})
	commitAll(t, dir, "change a")

	diff, err := gitDiff(dir, "start")
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("diff missing expected change: %s", diff)
	}
}

func TestGitDiff_BadRef_ReturnsError(t *testing.T) {
	dir := newSyntheticRepo(t, map[string]string{"a.txt": "one\n"})
	if _, err := gitDiff(dir, "no-such-ref"); err == nil {
		t.Fatalf("gitDiff: want error for unknown ref, got nil")
	}
}

// ---- buildJudgePrompt --------------------------------------------------------

func TestBuildJudgePrompt_IncludesCriteriaAndDiff(t *testing.T) {
	prompt := buildJudgePrompt("+foo", []string{"criterion one", "criterion two"})
	if !strings.Contains(prompt, "1. criterion one") || !strings.Contains(prompt, "2. criterion two") {
		t.Fatalf("prompt missing numbered criteria: %s", prompt)
	}
	if !strings.Contains(prompt, "+foo") {
		t.Fatalf("prompt missing diff: %s", prompt)
	}
}
