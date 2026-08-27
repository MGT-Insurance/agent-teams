package promptsync

import (
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
