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
	assertCondenseSharedOperationalPart(t, skill)
	assertCodexCondenseLockFlow(t, skill)
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
		"ateam learn <role> hot:<slug> --file <file>",
		"Run it HERE, once the hot set has been written.",
		"ateam fresh-drain <role>",
	}, "\n")
	if err := condenseOrderingError(fixture); err != nil {
		t.Fatalf("control fixture rejected: %v", err)
	}

	mutated := strings.Replace(fixture, "ateam learn <role> hot:<slug> --file <file>\nRun it HERE, once the hot set has been written.\nateam fresh-drain <role>", "ateam fresh-drain <role>\nRun it HERE, once the hot set has been written.\nateam learn <role> hot:<slug> --file <file>", 1)
	if err := condenseOrderingError(mutated); err == nil {
		t.Fatal("drain-before-batch mutation was accepted")
	}
}

func TestCodexCondenseStructuralGuardRejectsMissingAllRoleLockOrRelease(t *testing.T) {
	entries, err := LoadManifests(filepath.Join("..", ".."), nil)
	if err != nil {
		t.Fatalf("load prompt manifests: %v", err)
	}
	skill := promptEntry(t, entries, "skill.condense")
	if err := condenseSharedOperationalPartsError(skill); err != nil {
		t.Fatalf("control manifest rejected: %v", err)
	}
	if err := codexCondenseLockFlowError(skill); err != nil {
		t.Fatalf("control lock flow rejected: %v", err)
	}

	for _, removed := range []string{"shared-all-role-lock", "shared-all-role-release"} {
		mutated := skill
		mutated.Outputs = append([]Output(nil), skill.Outputs...)
		for i := range mutated.Outputs {
			if !strings.Contains(mutated.Outputs[i].Path, "agent-teams-codex/") {
				continue
			}
			mutated.Outputs[i].Parts = removePart(mutated.Outputs[i].Parts, removed)
		}
		if err := condenseSharedOperationalPartsError(mutated); err == nil {
			t.Fatalf("removing %q from the disposable Codex manifest was accepted", removed)
		}
		if err := codexCondenseLockFlowError(mutated); err == nil {
			t.Fatalf("removing %q from the disposable Codex manifest kept a valid lock flow", removed)
		}
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
		"always processes exactly that role after acquiring the lock",
		"runs one all-role, gate-controlled sweep",
		"ateam condense-lock acquire",
		"code 5",
		"Same code-5 handling as **Step 0** in the single-role form above",
		"If acquisition succeeds, proceed and ensure the lock is released in every exit path",
		"ateam condense-lock release",
		"Release in ALL exit paths (success and error)",
		"Release on error paths too — do not leave the lock held.",
		"The lock window covers all role processing and any `ateam sync` at the end.",
		"that sync must also occur within the lock window, before release.",
		"ateam condense-check",
		"That single read-only call enumerates every learning role",
		"Defer to the printed verdict. Do NOT recompute it.",
		"skipping `user` and `applied` unconditionally",
		"user",
		"applied",
		"PURE READ",
		"do not create any `<role>:hot:*` key until the full hot set is decided",
		"Run it HERE, once the hot set has been written",
		"If the role is still over budget, **iterate — do not accept-and-report**",
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
	if got := strings.Count(body, "ateam condense-lock acquire"); got != 2 {
		t.Errorf("%s has %d lock acquisitions, want one for each invocation form", label, got)
	}
	if got := strings.Count(body, "ateam condense-lock release"); got != 2 {
		t.Errorf("%s has %d lock releases, want one for each acquired invocation form", label, got)
	}
}

func condenseOrderingError(body string) error {
	packet := strings.Index(body, "ateam condense <role>")
	batch := strings.Index(body, "ateam learn <role> hot:<slug>")
	drain := strings.LastIndex(body, "ateam fresh-drain <role>")
	if packet < 0 || batch < 0 || drain < 0 {
		return fmt.Errorf("missing packet, batch-write, or fresh-drain anchor")
	}
	if packet >= batch || batch >= drain {
		return fmt.Errorf("want packet before full batch write before drain (positions %d, %d, %d)", packet, batch, drain)
	}
	if !strings.Contains(body, "Run it HERE, once the hot set has been written") {
		return fmt.Errorf("missing explicit drain-after-batch rule")
	}
	return nil
}

func assertCondenseSharedOperationalPart(t *testing.T, entry Entry) {
	t.Helper()
	if err := condenseSharedOperationalPartsError(entry); err != nil {
		t.Error(err)
	}
}

func condenseSharedOperationalPartsError(entry Entry) error {
	inputs := make(map[string]Input, len(entry.Inputs))
	for _, input := range entry.Inputs {
		inputs[input.ID] = input
	}
	claude := outputForRuntimeForError(entry, "claude")
	codex := outputForRuntimeForError(entry, "codex")
	shared := make(map[string]bool)
	for _, part := range claude.Parts {
		if strings.HasPrefix(part, "shared-") {
			shared[part] = true
		}
	}
	for _, part := range []string{
		"shared-single-role",
		"shared-all-role-lock",
		"shared-gate",
		"shared-skip",
		"shared-packet-order",
		"shared-packet-example",
		"shared-packet-semantics",
		"shared-all-role-release",
		"shared-design-order",
		"shared-promotion",
		"shared-apply",
		"shared-apply-maintenance",
		"shared-drain",
		"shared-verify",
	} {
		if !shared[part] || !containsPart(codex.Parts, part) {
			return fmt.Errorf("Claude and Codex condense outputs must both consume shared operational part %q", part)
		}
		input := inputs[part]
		if !strings.Contains(input.Path, "/shared-") {
			return fmt.Errorf("shared condense part %q has non-shared source %q", part, input.Path)
		}
	}
	return nil
}

func assertCodexCondenseLockFlow(t *testing.T, entry Entry) {
	t.Helper()
	if err := codexCondenseLockFlowError(entry); err != nil {
		t.Error(err)
	}
}

func codexCondenseLockFlowError(entry Entry) error {
	codex := outputForRuntimeForError(entry, "codex")
	if codex.Path == "" {
		return fmt.Errorf("Codex condense output missing")
	}
	for _, part := range []string{"shared-single-role", "shared-all-role-lock", "shared-all-role-release"} {
		if countPart(codex.Parts, part) != 1 {
			return fmt.Errorf("Codex condense must include exactly one %s, got %d", part, countPart(codex.Parts, part))
		}
	}

	positions := make(map[string]int, len(codex.Parts))
	for index, part := range codex.Parts {
		positions[part] = index
	}
	for _, pair := range [][2]string{
		{"shared-all-role-lock", "shared-gate"},
		{"shared-gate", "shared-packet-order"},
		{"shared-packet-order", "shared-design-order"},
		{"shared-design-order", "shared-apply"},
		{"shared-apply", "shared-drain"},
		{"shared-drain", "shared-verify"},
		{"shared-verify", "shared-all-role-release"},
	} {
		before, after := positions[pair[0]], positions[pair[1]]
		if before >= after {
			return fmt.Errorf("Codex all-role flow must order %s before %s", pair[0], pair[1])
		}
	}
	return nil
}

func outputForRuntimeForError(entry Entry, runtime string) Output {
	for _, output := range entry.Outputs {
		if strings.HasPrefix(output.Path, "plugins/agent-teams-"+runtime+"/") {
			return output
		}
		if runtime == "claude" && strings.HasPrefix(output.Path, "plugins/agent-teams/") {
			return output
		}
	}
	return Output{}
}

func countPart(parts []string, want string) int {
	count := 0
	for _, part := range parts {
		if part == want {
			count++
		}
	}
	return count
}

func removePart(parts []string, removed string) []string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != removed {
			kept = append(kept, part)
		}
	}
	return kept
}

func containsPart(parts []string, want string) bool {
	for _, part := range parts {
		if part == want {
			return true
		}
	}
	return false
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
		section := body
		learningsAnchor := "After contributing durable learnings"
		if strings.Contains(path, "wind-down") {
			section = windDownChecklist(body)
			learningsAnchor = "Route any durable learning through `ateam learn`"
		}
		normalized := strings.Join(strings.Fields(section), " ")
		learnings := strings.Index(normalized, learningsAnchor)
		condense := strings.Index(normalized, "agent-teams-codex:condense")
		finalNote := strings.Index(normalized, "final initiative note")
		endTurn := -1
		if finalNote >= 0 {
			if afterFinalNote := strings.Index(strings.ToLower(normalized[finalNote:]), "end the turn"); afterFinalNote >= 0 {
				endTurn = finalNote + afterFinalNote
			}
		}
		if learnings < 0 || condense < 0 || finalNote < 0 || endTurn < 0 {
			t.Errorf("%s must include learnings, no-role condense, final note, and end-turn", path)
			continue
		}
		if !(learnings < condense && condense < finalNote && finalNote < endTurn) {
			t.Errorf("%s must order learnings -> condense -> final note -> end turn", path)
		}
		if strings.Contains(section, "ateam condense <role>") {
			t.Errorf("%s replaces the all-role skill with a direct role condense call", path)
		}
	}
}

func windDownChecklist(body string) string {
	if start := strings.Index(body, "# Wind-down"); start >= 0 {
		return body[start:]
	}
	return body
}

func TestWorktreeSetupPromptContract(t *testing.T) {
	root := filepath.Join("..", "..")

	t.Run("standalone semantics distinguish no hook from configured failure", func(t *testing.T) {
		assertPromptClauses(t, root,
			[]string{
				"README.md",
				"promptsrc/agent-teams/skills/setup-agent-teams/claude-runtime.md",
				"plugins/agent-teams/skills/setup-agent-teams/SKILL.md",
			},
			"No registered hook is an exit-0 no-op",
			"configured hook that is missing or fails",
			"standalone `ateam worktree-setup` exit 1",
		)
		assertPromptOmits(t, root,
			[]string{
				"README.md",
				"promptsrc/agent-teams/skills/setup-agent-teams/claude-runtime.md",
				"plugins/agent-teams/skills/setup-agent-teams/SKILL.md",
			},
			"with no hook (or a configured script that is missing)",
			"A missing or failing hook is non-fatal.",
		)
	})

	t.Run("setup distinguishes managed fresh worktrees from on-demand usage", func(t *testing.T) {
		paths := []string{
			"README.md",
			"promptsrc/agent-teams/skills/setup-agent-teams/claude-runtime.md",
			"plugins/agent-teams/skills/setup-agent-teams/SKILL.md",
		}
		assertPromptClauses(t, root, paths,
			"Manual usage and pre-existing or resumed worktrees remain on-demand.",
			"every fresh agent-teams-managed primary or delegated worktree gets a mandatory automatic setup attempt before its agent runs Node tooling",
			"a failed attempt is reported and does not block the later managed lifecycle.",
		)
		assertPromptOmits(t, root, paths,
			"It is invoked on-demand, not on every worktree.",
			"Most work doesn't need them.",
			"When a worktree does need live env",
		)
	})

	t.Run("delegated setup captures failure without stopping the lifecycle", func(t *testing.T) {
		paths := []string{
			"promptsrc/agent-teams/skills/dri/references/execution-shared.md",
			"plugins/agent-teams/skills/dri/references/execution.md",
			"plugins/agent-teams-codex/skills/dri/references/execution.md",
		}
		assertPromptClauses(t, root, paths,
			`ateam worktree-setup "$track_worktree" || setup_status=$?`,
			`if [ "$setup_status" -ne 0 ]; then`,
			`path="%s" hook="%s" outcome="%s" lifecycle=continued`,
			`ateam note "$initiative_id" --file "$warning_file" ||`,
			"A note failure is visibly reported but nonblocking.",
			"then record `track-worktree:` and spawn.",
		)
	})
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
