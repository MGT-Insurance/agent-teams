package verbs

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

//go:embed codex_agents/*.toml
var codexAgentDefinitions embed.FS

type setupCmd struct {
	Codex setupCodexKong `cmd:"" help:"Install and verify Codex-specific agent-teams components."`
}

type setupCodexKong struct {
	Force bool `name:"force" help:"Replace locally modified agent definitions with the bundled versions."`

	executable string `kong:"-"`
	codexHome  string `kong:"-"`
}

func RegisterSetupKong(p *cli.Parser) {
	p.AddVerb("setup", "Install harness-specific agent-teams components.", &setupCmd{})
}

func (c *setupCodexKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam setup codex: nil context")
	}
	if err := sessionruntime.RequireCompatibleCodex(context.Background(), c.executable); err != nil {
		return fmt.Errorf("ateam setup codex: %w", err)
	}
	home := c.codexHome
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("ateam setup codex: resolve home: %w", err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	targetDir := filepath.Join(home, "agents")
	entries, err := codexAgentDefinitions.ReadDir("codex_agents")
	if err != nil {
		return fmt.Errorf("ateam setup codex: read bundled agents: %w", err)
	}

	type definition struct {
		name string
		body []byte
	}
	definitions := make([]definition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := codexAgentDefinitions.ReadFile(filepath.ToSlash(filepath.Join("codex_agents", entry.Name())))
		if err != nil {
			return fmt.Errorf("ateam setup codex: read %s: %w", entry.Name(), err)
		}
		target := filepath.Join(targetDir, entry.Name())
		current, readErr := os.ReadFile(target)
		if readErr == nil && !bytes.Equal(current, body) && !c.Force {
			return fmt.Errorf("ateam setup codex: %s has local changes; rerun with --force to replace it", target)
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("ateam setup codex: read %s: %w", target, readErr)
		}
		definitions = append(definitions, definition{name: entry.Name(), body: body})
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("ateam setup codex: create agents directory: %w", err)
	}
	for _, definition := range definitions {
		target := filepath.Join(targetDir, definition.name)
		current, _ := os.ReadFile(target)
		if bytes.Equal(current, definition.body) {
			fmt.Fprintf(ctx.Stdout, "up-to-date: %s\n", target)
			continue
		}
		if err := os.WriteFile(target, definition.body, 0o600); err != nil {
			return fmt.Errorf("ateam setup codex: write %s: %w", target, err)
		}
		fmt.Fprintf(ctx.Stdout, "installed: %s\n", target)
	}
	fmt.Fprintln(ctx.Stdout, "in the Codex CLI, open /hooks and trust the agent-teams-codex lifecycle hooks")
	fmt.Fprintln(ctx.Stdout, "start a new Codex session before testing the installed agent types")
	return nil
}
