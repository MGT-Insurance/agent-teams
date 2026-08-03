// This file is owned by Track V (repo opt-in).
package verbs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

// enableRepoGit is the subset of gitutil.Runner enableRepoKong needs,
// extracted so tests can inject a fake RepoRoot without exec'ing a real git
// binary.
type enableRepoGit interface {
	RepoRoot(dir string) (string, error)
}

// enableRepoKong is the kong-native form of enable-repo.
// git is injected via the git field (kong:"-" keeps kong from treating it as
// a flag); when left nil (the registered default), Run falls back to a real
// gitutil.Runner. Tests stub it so they never exec a real git binary.
type enableRepoKong struct {
	Path string `arg:"" name:"path" optional:"" help:"Repo (or subdirectory) to opt in; defaults to cwd."`

	git enableRepoGit `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *enableRepoKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam enable-repo: not implemented")
	}

	dir := c.Path
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("ateam enable-repo: cannot determine cwd: %w", err)
		}
	}

	git := c.git
	if git == nil {
		git = gitutil.New()
	}

	repoRoot, err := git.RepoRoot(dir)
	if err != nil {
		fmt.Fprintln(ctx.Stderr, "ateam enable-repo: not inside a git repo: "+dir)
		return cli.Silent(1)
	}

	result, err := repoconfig.Enable(repoRoot)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "ateam enable-repo: %v\n", err)
		return cli.Silent(1)
	}

	fmt.Fprintf(ctx.Stdout, "enabled: %s %s\n", filepath.Join(repoRoot, repoconfig.FileName), result)
	return nil
}
