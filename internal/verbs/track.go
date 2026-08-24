package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterTrackKong registers the `track` parent verb onto p.
func RegisterTrackKong(p *cli.Parser) {
	p.AddVerb("track", "Manage an initiative's recorded track worktrees.", &trackCmd{})
}

// trackCmd is the kong parent struct for `ateam track <subcommand>`.
type trackCmd struct {
	Add trackAddKong `cmd:"" name:"add" help:"Record an absolute track-worktree path on an initiative."`
}

// trackAddKong implements the sanctioned, append-only
// `ateam track add <initiative-id> <absolute-path>` mutation.
type trackAddKong struct {
	InitiativeID string `arg:"" name:"initiative-id" help:"Initiative ID to record the track worktree on."`
	Path         string `arg:"" name:"absolute-path" help:"Absolute path of the track worktree to record."`
}

func (c *trackAddKong) Validate() error {
	if c.InitiativeID == "" {
		return cli.Usagef("ateam track add: initiative-id must not be empty")
	}
	if c.Path == "" {
		return cli.Usagef("ateam track add: absolute-path must not be empty")
	}
	if !filepath.IsAbs(c.Path) {
		return cli.Usagef("ateam track add: absolute-path must be absolute, got %q", c.Path)
	}
	if _, err := initiative.WithTrack(bd.Issue{}, c.Path); err != nil {
		return cli.Usagef("ateam track add: %v", err)
	}
	return nil
}

// Run acquires the same per-initiative cross-process lock used by pr add and
// session ties. The initiative is first read only after lock acquisition, and
// the whole-description update completes before the lock is released.
func (c *trackAddKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam track add: no context")
	}
	lock, err := acquireInitiativeMutationLock(ctx.Home, c.InitiativeID)
	if err != nil {
		return fmt.Errorf("ateam track add: acquire initiative %s lock: %w", c.InitiativeID, err)
	}

	alreadyRecorded, runErr := c.runLocked(ctx)
	releaseErr := lock.Close()
	if releaseErr != nil {
		releaseErr = fmt.Errorf("ateam track add: release initiative %s lock: %w", c.InitiativeID, releaseErr)
	}
	if runErr != nil || releaseErr != nil {
		return errors.Join(runErr, releaseErr)
	}

	if alreadyRecorded {
		fmt.Fprintf(ctx.Stdout, "track add: %s already recorded on %s\n", c.Path, c.InitiativeID)
	} else {
		fmt.Fprintf(ctx.Stdout, "track add: recorded %s on %s\n", c.Path, c.InitiativeID)
	}
	return nil
}

func (c *trackAddKong) runLocked(ctx *cli.Context) (bool, error) {
	// This must remain the first initiative read. Input validation is pure and
	// Run acquires the lock before entering this helper.
	issue, err := bd.ShowIssue(ctx.BD, c.InitiativeID)
	if err != nil {
		return false, fmt.Errorf("ateam track add: bd show %s: %w", c.InitiativeID, err)
	}
	plan, err := initiative.WithTrack(issue, c.Path)
	if err != nil {
		return false, fmt.Errorf("ateam track add: %w", err)
	}
	if plan.Description == issue.Description {
		return true, nil
	}

	tmpFile, err := os.CreateTemp("", "ateam-track-add-*.txt")
	if err != nil {
		return false, fmt.Errorf("ateam track add: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(plan.Description); err != nil {
		closeErr := tmpFile.Close()
		writeErr := fmt.Errorf("ateam track add: write temp file: %w", err)
		if closeErr != nil {
			return false, errors.Join(writeErr, fmt.Errorf("ateam track add: close temp file after write failure: %w", closeErr))
		}
		return false, writeErr
	}
	if err := tmpFile.Close(); err != nil {
		return false, fmt.Errorf("ateam track add: close temp file: %w", err)
	}

	if _, err := ctx.BD.Run("update", c.InitiativeID, "--body-file="+tmpPath); err != nil {
		return false, fmt.Errorf("ateam track add: bd update %s: %w", c.InitiativeID, err)
	}
	return false, nil
}
