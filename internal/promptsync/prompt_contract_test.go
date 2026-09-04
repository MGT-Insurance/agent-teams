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
