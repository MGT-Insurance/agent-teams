package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// judgeModel is the fixed model used for LLM grading, independent of
// cfg.DRIModel (the run configuration under test) so the judge is a stable
// yardstick across configs. Hardcoded per grft.5 v1 scope; uses the same
// alias convention as internal/verbs/dispatch.go rather than a pinned model
// ID, for consistency with the rest of the codebase.
const judgeModel = "opus"

// llmTimeout bounds the judge's LLM subprocess so a hung `claude -p` call
// cannot stall `eval collect` indefinitely.
const llmTimeout = 3 * time.Minute

// judgeResponseSchema is passed to `claude -p --json-schema` so the model's
// reply is API-enforced structured output, not free-text JSON the CLI might
// wrap in prose or markdown fences. parseJudgeResponse still strict-parses
// defensively (see its doc comment) rather than trusting this unconditionally.
const judgeResponseSchema = `{"type":"object","properties":{"criteriaResults":{"type":"array","items":{"type":"object","properties":{"criterion":{"type":"string"},"met":{"type":"boolean"},"note":{"type":"string"}},"required":["criterion","met","note"],"additionalProperties":false}},"correctnessScore":{"type":"number"},"rationale":{"type":"string"}},"required":["criteriaResults","correctnessScore","rationale"],"additionalProperties":false}`

// judgePromptTemplate grades the diff ARTIFACT against authored acceptance
// criteria on outcome only. Per GRADING DISCIPLINE (agent-teams-grft.5): the
// judge is never shown tool-call sequences, turn counts, or any process
// trace — Judge's signature structurally can't see Metrics — so there is
// nothing here to prompt it away from grading process instead of outcome.
const judgePromptTemplate = `You are grading a code change (a git diff) against a fixed set of acceptance criteria for a software task. Grade strictly on OUTCOME: does the diff satisfy each criterion? You have not been shown how the diff was produced (no tool calls, no step count) and that information is irrelevant to correctness — grade the artifact, not the process.

ACCEPTANCE CRITERIA:
%s

DIFF:
%s

Respond with a JSON object matching the required schema:
- criteriaResults: exactly one entry per criterion listed above, in the same order, with "criterion" copied exactly from the input.
- correctnessScore: a float between 0 and 1 reflecting the overall fraction/quality of criteria met, not a simple count.
- rationale: a 2-3 sentence overall rationale.`

// llmFunc sends prompt to an LLM and returns its raw text reply. Judge's
// production path wires callClaudeP; tests inject a fake so no real LLM call
// happens in the unit test suite.
type llmFunc func(prompt string) (string, error)

// judger holds Judge's injectable LLM seam. Mirrors the injectable-ExecFunc
// pattern in internal/gitutil (Runner.exec) and internal/verbs/dispatch.go
// (launchFunc): one unexported struct field, swapped via a package-private
// constructor in tests, so Judge's frozen signature stays untouched.
type judger struct {
	llm llmFunc
}

// Judge runs task.BuildCheck in the run's produced worktree for
// ObjectiveFloorPass, then asks an LLM to grade the produced diff against
// task.AcceptanceCriteria for CorrectnessScore + per-criterion results. Both
// layers always run and are independent signals (see JudgeResult): a buggy
// fix is expected to score ObjectiveFloorPass=false AND low correctness, not
// short-circuit the LLM call.
func Judge(m RunManifest, task TaskSpec) (JudgeResult, error) {
	return judger{llm: callClaudeP}.judge(m, task)
}

func (j judger) judge(m RunManifest, task TaskSpec) (JudgeResult, error) {
	floorPass, err := runBuildCheck(m.WorktreePath, task.BuildCheck)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("eval: judge %s: objective floor: %w", task.ID, err)
	}

	diff, err := gitDiff(m.WorktreePath, task.FixtureRef)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("eval: judge %s: git diff: %w", task.ID, err)
	}

	prompt := buildJudgePrompt(diff, task.AcceptanceCriteria)
	raw, err := j.llm(prompt)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("eval: judge %s: llm call: %w", task.ID, err)
	}

	resp, err := parseJudgeResponse(raw)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("eval: judge %s: %w", task.ID, err)
	}

	return JudgeResult{
		ObjectiveFloorPass: floorPass,
		CorrectnessScore:   resp.CorrectnessScore,
		CriteriaResults:    resp.CriteriaResults,
		Rationale:          resp.Rationale,
	}, nil
}

// runBuildCheck runs task.BuildCheck as a shell command in dir. A nonzero
// exit is a normal, expected outcome (the floor failed) and is reported as
// (false, nil) — not a Go error. A Go error means the check could not be run
// at all (e.g. dir does not exist, shell missing), which is an infra failure
// distinct from "the build failed." An empty buildCheck runs `sh -c ""`,
// which exits 0 (vacuous pass); v1 tasks always set a real check.
func runBuildCheck(dir, buildCheck string) (bool, error) {
	cmd := exec.Command("sh", "-c", buildCheck)
	cmd.Dir = dir
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	msg := strings.TrimSpace(errOut.String())
	if msg == "" {
		msg = err.Error()
	}
	return false, fmt.Errorf("run buildCheck %q in %s: %s", buildCheck, dir, msg)
}

// gitDiff returns the diff of worktreePath's HEAD against fixtureRef —
// the produced changes, i.e. everything the run committed. Diffing against
// HEAD (not the working tree) is deliberate: the DRI's wind-down leaves work
// committed on the run's branch (see CONTRACT bead's COMPLETION SIGNAL), so
// a working-tree diff would miss it.
func gitDiff(worktreePath, fixtureRef string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "diff", fixtureRef, "HEAD")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git diff %s HEAD: %s", fixtureRef, msg)
	}
	return out.String(), nil
}

// buildJudgePrompt renders judgePromptTemplate with a numbered acceptance
// criteria list and the diff.
func buildJudgePrompt(diff string, criteria []string) string {
	var criteriaList strings.Builder
	for i, c := range criteria {
		if i > 0 {
			criteriaList.WriteByte('\n')
		}
		criteriaList.WriteString(strconv.Itoa(i + 1))
		criteriaList.WriteString(". ")
		criteriaList.WriteString(c)
	}
	return fmt.Sprintf(judgePromptTemplate, criteriaList.String(), diff)
}

// llmJudgeResponse is the LLM's correctness verdict, everything JudgeResult
// needs except ObjectiveFloorPass (computed deterministically, never by the
// LLM).
type llmJudgeResponse struct {
	CriteriaResults  []CriterionResult `json:"criteriaResults"`
	CorrectnessScore float64           `json:"correctnessScore"`
	Rationale        string            `json:"rationale"`
}

// jsonFenceRE strips a single ```json ... ``` or ``` ... ``` wrapper some
// models add despite instructions not to. Applied defensively: production
// calls pass --json-schema (see callClaudeP), which returns unfenced JSON,
// but parseJudgeResponse is also exercised directly by tests via the fake
// llm seam and must not assume its caller enforced a schema.
var jsonFenceRE = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")

// parseJudgeResponse strict-parses raw as a llmJudgeResponse. A malformed
// reply (not valid JSON, wrong shape) returns an error — never a
// zero-valued result — per grft.5's acceptance criterion.
func parseJudgeResponse(raw string) (llmJudgeResponse, error) {
	text := strings.TrimSpace(raw)
	if m := jsonFenceRE.FindStringSubmatch(text); m != nil {
		text = strings.TrimSpace(m[1])
	}
	if text == "" {
		return llmJudgeResponse{}, errors.New("llm judge: empty response")
	}
	var resp llmJudgeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		snippet := raw
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return llmJudgeResponse{}, fmt.Errorf("llm judge: response is not valid JSON: %w (raw: %s)", err, snippet)
	}
	return resp, nil
}

// claudePEnvelope is the subset of `claude -p --output-format json`'s result
// envelope Judge needs. Verified live against the installed claude CLI:
// {"type":"result","is_error":false,"result":"<model text>",...}.
type claudePEnvelope struct {
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// callClaudeP is the production llmFunc: shells `claude -p` in a fully
// isolated, tool-less session. --safe-mode skips CLAUDE.md/skills/plugins/
// hooks (the judge must not be influenced by the target repo's own
// customizations); --tools "" disables all built-in tools (the judge only
// ever needs the diff and criteria already in the prompt); --json-schema
// forces API-level structured output matching judgeResponseSchema so the
// reply needs no fence-stripping in practice. The prompt is piped via stdin
// (not argv) so an arbitrarily large diff never risks ARG_MAX.
func callClaudeP(prompt string) (string, error) {
	if _, err := exec.LookPath("claude"); err != nil {
		return "", fmt.Errorf("claude -p: 'claude' not found in PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--model", judgeModel,
		"--output-format", "json",
		"--tools", "",
		"--safe-mode",
		"--json-schema", judgeResponseSchema,
	)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude -p: %s", msg)
	}

	var env claudePEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		return "", fmt.Errorf("claude -p: unparseable result envelope: %w", err)
	}
	if env.IsError {
		return "", fmt.Errorf("claude -p: %s", env.Result)
	}
	return env.Result, nil
}
