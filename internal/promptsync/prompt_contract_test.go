package promptsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexPromptsIncludeInvestigatorContracts(t *testing.T) {
	root := filepath.Join("..", "..")

	t.Run("DRI delegates bounded evidence without design authority", func(t *testing.T) {
		assertPromptClauses(t, root,
			[]string{
				"promptsrc/agent-teams/skills/dri/codex-runtime.md",
				"plugins/agent-teams-codex/skills/dri/SKILL.md",
			},
			"Delegate bounded, evidence-only questions to an `agent-teams-investigator`",
			"the `agent-teams-planner` retains design authority and owns decomposition",
		)
		assertPromptClauses(t, root,
			[]string{
				"promptsrc/agent-teams/skills/dri/references/execution-codex.md",
				"plugins/agent-teams-codex/skills/dri/references/execution.md",
			},
			"`agent-teams-investigator` with `fork_turns=\"none\"`",
			"Use the investigator only for bounded, evidence-only questions",
			"the planner retains design authority and owns decomposition",
		)
	})

	t.Run("setup describes and verifies all five roles", func(t *testing.T) {
		assertPromptClauses(t, root,
			[]string{
				"promptsrc/agent-teams/skills/setup-agent-teams/codex-header.md",
				"plugins/agent-teams-codex/skills/setup-agent-teams/SKILL.md",
			},
			"planner, implementer, tester, reviewer, and investigator custom agent definitions",
		)
		assertPromptClauses(t, root,
			[]string{
				"promptsrc/agent-teams/skills/setup-agent-teams/codex-runtime.md",
				"plugins/agent-teams-codex/skills/setup-agent-teams/SKILL.md",
			},
			"Verify all five files exist",
			"`agent-teams-investigator.toml`",
		)
	})
}

func TestCodexSetupPreservesOccupiedAteamPath(t *testing.T) {
	root := filepath.Join("..", "..")
	paths := []string{
		"promptsrc/agent-teams/skills/setup-agent-teams/codex-runtime.md",
		"plugins/agent-teams-codex/skills/setup-agent-teams/SKILL.md",
	}

	assertPromptClauses(t, root, paths,
		"If any filesystem entry occupies that path, including a dangling symlink, leave it untouched",
		`if [ -e "$ATEAM_LINK" ] || [ -L "$ATEAM_LINK" ]; then`,
		`ln -s "$PLUGIN_ATEAM" "$ATEAM_LINK"`,
		`"$PLUGIN_ATEAM" ws`,
		"Use the resolved `PLUGIN_ATEAM` wrapper, not the possibly preserved `~/.local/bin/ateam` path",
	)
	assertPromptOmits(t, root, paths, "ln -sf", "ln -sfn")
}

func TestCodexPromptSourcesPreserveSessionStartOnlyMailContract(t *testing.T) {
	root := filepath.Join("..", "..")

	t.Run("setup uses SessionStart only", func(t *testing.T) {
		paths := []string{
			"promptsrc/agent-teams/skills/setup-agent-teams/codex-runtime.md",
			"plugins/agent-teams-codex/skills/setup-agent-teams/SKILL.md",
		}
		assertPromptClauses(t, root, paths,
			"Trust only its current `SessionStart` command-hook definition",
			"Managed app-server delivery is the Codex mail wake path",
			"`SessionStart` binds the session and catches up queued unread mail only on startup or resume",
			"On clear or compact, it binds without an unread-mail query or catch-up context",
		)
		assertPromptOmits(t, root, paths,
			"UserPromptSubmit",
			"`Stop` command-hook",
			"doorbell repair",
			"poll for mail",
			"watch for mail",
		)
	})

	t.Run("DRI treats app-server delivery as authoritative", func(t *testing.T) {
		paths := []string{
			"promptsrc/agent-teams/skills/dri/codex-runtime.md",
			"plugins/agent-teams-codex/skills/dri/SKILL.md",
		}
		assertPromptClauses(t, root, paths,
			"Managed app-server delivery wakes this durable Codex thread when mail arrives",
			"`SessionStart` only binds the session and catches up queued unread mail on startup or resume",
			"On clear or compact, it binds without an unread-mail query or catch-up context",
		)
		assertPromptOmits(t, root, paths,
			"Mail and lifecycle hooks wake this same durable thread",
			"UserPromptSubmit",
			"Stop enforcement",
			"doorbell repair",
			"poll for mail",
			"watch for mail",
		)
	})
}

func TestCodexCondenseContract(t *testing.T) {
	root := filepath.Join("..", "..")
	entries, err := LoadManifests(root, nil)
	if err != nil {
		t.Fatalf("load prompt manifests: %v", err)
	}

	condenseIDs := []string{
		"skill.condense",
		"reference.condense.drain-ordering",
		"reference.condense.recall-verification",
		"reference.condense.trigger-design",
	}
	for position, id := range condenseIDs {
		entry := promptEntry(t, entries, id)
		wantKind := KindReference
		if position == 0 {
			wantKind = KindSkill
		}
		if entry.Kind != wantKind {
			t.Errorf("%s kind = %q, want %q", id, entry.Kind, wantKind)
		}
		if entry.Status != StatusPaired {
			t.Errorf("%s status = %q, want %q", id, entry.Status, StatusPaired)
		}
		if len(entry.Inputs) == 0 {
			t.Errorf("%s has no canonical inputs", id)
		}
		assertCounterpartOutputs(t, entry, id, expectedCondenseOutput(id, "claude"), expectedCondenseOutput(id, "codex"))
	}

	skill := promptEntry(t, entries, "skill.condense")
	codexSkill := outputForRuntime(t, skill, "codex")
	if codexSkill.Path == "" {
		// The paired surface is the prerequisite for the remaining rendered and
		// wind-down checks. The classification failures above are the useful
		// diagnostic until the production track supplies it.
		return
	}
	if codexSkill.Path != "plugins/agent-teams-codex/skills/condense/SKILL.md" {
		t.Errorf("Codex condense path = %q", codexSkill.Path)
	}
	if codexSkill.SkillBudget == nil {
		t.Error("Codex condense SKILL.md has no skill budget")
	} else if codexSkill.SkillBudget.MinHeadroom < 4000 {
		t.Errorf("Codex condense min_headroom = %d, want at least 4000", codexSkill.SkillBudget.MinHeadroom)
	}

	canonical := renderParts(t, root, skill, codexSkill)
	rendered := readPrompt(t, root, codexSkill.Path)
	assertCodexCondenseMain(t, "canonical Codex condense source", canonical)
	assertCodexCondenseMain(t, codexSkill.Path, rendered)
	assertPromptOmits(t, root, []string{codexSkill.Path}, "/agent-teams:condense", "/setup-agent-teams", "Claude memory")

	report, err := Check(Config{Root: root})
	if err != nil {
		t.Errorf("prompt-sync check: %v", err)
	} else {
		measurement, ok := skillMeasurement(report, codexSkill.Path)
		if !ok {
			t.Errorf("prompt-sync report has no measurement for %s", codexSkill.Path)
		} else if measurement.Headroom < 4000 {
			t.Errorf("Codex condense headroom = %d, want at least 4000", measurement.Headroom)
		}
	}

	assertCodexDRIWindDown(t, root,
		"promptsrc/agent-teams/skills/dri/codex-runtime.md",
		"plugins/agent-teams-codex/skills/dri/SKILL.md",
	)
	assertCodexDRIWindDown(t, root,
		"promptsrc/agent-teams/skills/dri/references/wind-down-codex.md",
		"plugins/agent-teams-codex/skills/dri/references/wind-down.md",
	)
}

func TestCodexCondenseOrderingGuardRejectsDrainBeforeBatch(t *testing.T) {
	fixture := strings.Join([]string{
		"ateam condense <role>",
		"This emits a JSON packet to stdout.",
		"IMPORTANT ORDERING: do not create any <role>:hot:* key until the full hot set is decided.",
		"ateam learn <role> hot:<slug> --file <file>",
		"After ALL hot entries are written, handle cold cleanup.",
		"### Drain fresh — AFTER the batch write, never before",
		"ateam fresh-drain <role>",
	}, "\n")
	if err := condenseOrderingError(fixture); err != nil {
		t.Fatalf("control fixture rejected: %v", err)
	}

	mutated := strings.Replace(fixture, "After ALL hot entries are written, handle cold cleanup.\n### Drain fresh — AFTER the batch write, never before\nateam fresh-drain <role>", "ateam fresh-drain <role>\nAfter ALL hot entries are written, handle cold cleanup.", 1)
	if err := condenseOrderingError(mutated); err == nil {
		t.Fatal("drain-before-batch mutation was accepted")
	}
}

func promptEntry(t *testing.T, entries []Entry, id string) Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Errorf("manifest has no %s entry", id)
	return Entry{ID: id}
}

func assertCounterpartOutputs(t *testing.T, entry Entry, id, wantClaude, wantCodex string) {
	t.Helper()
	if len(entry.Outputs) != 2 {
		t.Errorf("%s has %d outputs, want Claude and Codex counterparts", id, len(entry.Outputs))
	}
	claude := outputForRuntime(t, entry, "claude")
	codex := outputForRuntime(t, entry, "codex")
	if claude.Path != wantClaude {
		t.Errorf("%s Claude output = %q, want %q", id, claude.Path, wantClaude)
	}
	if codex.Path != wantCodex {
		t.Errorf("%s Codex output = %q, want %q", id, codex.Path, wantCodex)
	}
}

func expectedCondenseOutput(id, runtime string) string {
	base := "plugins/agent-teams"
	if runtime == "codex" {
		base += "-codex"
	}
	path := base + "/skills/condense/"
	switch id {
	case "skill.condense":
		return path + "SKILL.md"
	case "reference.condense.drain-ordering":
		return path + "references/drain-ordering.md"
	case "reference.condense.recall-verification":
		return path + "references/recall-verification.md"
	case "reference.condense.trigger-design":
		return path + "references/trigger-design.md"
	default:
		return ""
	}
}

func outputForRuntime(t *testing.T, entry Entry, runtime string) Output {
	t.Helper()
	for _, output := range entry.Outputs {
		if strings.HasPrefix(output.Path, "plugins/agent-teams-"+runtime+"/") {
			return output
		}
		if runtime == "claude" && strings.HasPrefix(output.Path, "plugins/agent-teams/") {
			return output
		}
	}
	t.Errorf("%s has no %s counterpart output", entry.ID, runtime)
	return Output{}
}

func renderParts(t *testing.T, root string, entry Entry, output Output) string {
	t.Helper()
	inputs := make(map[string]Input, len(entry.Inputs))
	for _, input := range entry.Inputs {
		inputs[input.ID] = input
	}
	var body strings.Builder
	for _, part := range output.Parts {
		input, ok := inputs[part]
		if !ok {
			t.Errorf("%s Codex output references missing part %q", entry.ID, part)
			continue
		}
		body.WriteString(readPrompt(t, root, input.Path))
	}
	return body.String()
}

func readPrompt(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Errorf("read %s: %v", path, err)
		return ""
	}
	return string(body)
}

func assertCodexCondenseMain(t *testing.T, label, body string) {
	t.Helper()
	normalized := strings.Join(strings.Fields(body), " ")
	for _, clause := range []string{
		"name: condense",
		"agent-teams-codex:condense <role>",
		"agent-teams-codex:condense",
		"agent-teams-codex:setup-agent-teams",
		"Single-role form",
		"All-roles sweep",
		"ateam condense-lock acquire",
		"code 5",
		"ateam condense-lock release",
		"Release on error paths",
		"Gate every role with ONE call",
		"ateam condense-check",
		"Do NOT recompute it",
		"verdict is data, not an exit status",
		"The verb writes nothing",
		"skipping `user` and `applied` unconditionally",
		"user",
		"applied",
		"PURE READ",
		"then write all hot keys as a batch",
		"After ALL hot entries are written",
		"iterate — do not accept-and-report",
		"<role>: promoted N / merged M / evicted K / hot now X tokens / hot∪fresh Y tokens",
		"MEMORY.md",
		"Codex harness memory",
	} {
		normalizedClause := strings.Join(strings.Fields(clause), " ")
		if !strings.Contains(normalized, normalizedClause) {
			t.Errorf("%s does not contain %q", label, clause)
		}
	}
	if err := condenseOrderingError(body); err != nil {
		t.Errorf("%s ordering: %v", label, err)
	}
}

func condenseOrderingError(body string) error {
	packet := strings.Index(body, "This emits a JSON packet to stdout.")
	batch := strings.Index(body, "ateam learn <role> hot:<slug>")
	drain := strings.LastIndex(body, "ateam fresh-drain <role>")
	if packet < 0 || batch < 0 || drain < 0 {
		return fmt.Errorf("missing packet, batch-write, or fresh-drain anchor")
	}
	if packet >= batch || batch >= drain {
		return fmt.Errorf("want packet before full batch write before drain (positions %d, %d, %d)", packet, batch, drain)
	}
	if !strings.Contains(body, "Drain fresh — AFTER the batch write, never before") {
		return fmt.Errorf("missing explicit drain-after-batch rule")
	}
	return nil
}

func skillMeasurement(report Report, path string) (SkillMeasurement, bool) {
	for _, measurement := range report.Measurements {
		if measurement.Path == path {
			return measurement, true
		}
	}
	return SkillMeasurement{}, false
}

func assertCodexDRIWindDown(t *testing.T, root string, canonicalPath, renderedPath string) {
	t.Helper()
	for _, path := range []string{canonicalPath, renderedPath} {
		body := readPrompt(t, root, path)
		learnings := strings.Index(body, "ateam learn")
		condense := strings.Index(body, "agent-teams-codex:condense")
		finalNote := strings.Index(body, "final initiative note")
		endTurn := strings.Index(body, "End the turn")
		if learnings < 0 || condense < 0 || finalNote < 0 || endTurn < 0 {
			t.Errorf("%s must include learnings, no-role condense, final note, and end-turn", path)
			continue
		}
		if !(learnings < condense && condense < finalNote && finalNote < endTurn) {
			t.Errorf("%s must order learnings -> condense -> final note -> end turn", path)
		}
		if strings.Contains(body, "ateam condense <role>") {
			t.Errorf("%s replaces the all-role skill with a direct role condense call", path)
		}
	}
}

func assertPromptClauses(t *testing.T, root string, paths []string, clauses ...string) {
	t.Helper()
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		normalizedBody := strings.Join(strings.Fields(string(body)), " ")
		for _, clause := range clauses {
			normalizedClause := strings.Join(strings.Fields(clause), " ")
			if !strings.Contains(normalizedBody, normalizedClause) {
				t.Errorf("%s does not contain %q", path, clause)
			}
		}
	}
}

func assertPromptOmits(t *testing.T, root string, paths []string, clauses ...string) {
	t.Helper()
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, clause := range clauses {
			if strings.Contains(string(body), clause) {
				t.Errorf("%s unexpectedly contains %q", path, clause)
			}
		}
	}
}
