package verbs

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
	"github.com/pelletier/go-toml/v2"
)

//go:embed codex_agents/*.toml
var codexAgentDefinitions embed.FS

const (
	codexCompactionKey     = "model_auto_compact_token_limit"
	codexCompactionDefault = codexCompactionKey + " = 300000\n"
)

type codexConfigPlan struct {
	path      string
	writePath string
	body      []byte
	mode      os.FileMode
	install   bool
}

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
	configPlan, err := planCodexConfig(home)
	if err != nil {
		return fmt.Errorf("ateam setup codex: %w", err)
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
	if configPlan.install {
		if err := writeFileAtomic(configPlan.writePath, configPlan.body, configPlan.mode); err != nil {
			return fmt.Errorf("ateam setup codex: write %s: %w", configPlan.path, err)
		}
		fmt.Fprintf(ctx.Stdout, "installed default: %s\n", configPlan.path)
	} else {
		fmt.Fprintf(ctx.Stdout, "preserved override: %s\n", configPlan.path)
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

func planCodexConfig(home string) (codexConfigPlan, error) {
	path := filepath.Join(home, "config.toml")
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexConfigPlan{
				path:      path,
				writePath: path,
				body:      []byte(codexCompactionDefault),
				mode:      0o600,
				install:   true,
			}, nil
		}
		return codexConfigPlan{}, fmt.Errorf("read %s: %w", path, err)
	}

	writePath := path
	if info.Mode()&os.ModeSymlink != 0 {
		writePath, err = filepath.EvalSymlinks(path)
		if err != nil {
			return codexConfigPlan{}, fmt.Errorf("resolve %s: %w", path, err)
		}
	}

	file, err := os.Open(writePath)
	if err != nil {
		return codexConfigPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	info, err = file.Stat()
	if err != nil {
		return codexConfigPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		if writePath != path {
			return codexConfigPlan{}, fmt.Errorf("read %s: resolved target %s is not a regular file", path, writePath)
		}
		return codexConfigPlan{}, fmt.Errorf("read %s: not a regular file", path)
	}
	body, err := io.ReadAll(file)
	if err != nil {
		return codexConfigPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	var document map[string]any
	if err := toml.Unmarshal(body, &document); err != nil {
		return codexConfigPlan{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if _, exists := document[codexCompactionKey]; exists {
		return codexConfigPlan{path: path, writePath: writePath}, nil
	}

	merged := make([]byte, 0, len(codexCompactionDefault)+len(body))
	merged = append(merged, codexCompactionDefault...)
	merged = append(merged, body...)
	return codexConfigPlan{
		path:      path,
		writePath: writePath,
		body:      merged,
		mode:      info.Mode().Perm(),
		install:   true,
	}, nil
}

func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
