// This file is owned by Track C (write verbs).
package verbs

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// condenseLockPath returns the advisory lock file path for condense operations.
func condenseLockPath(home string) string {
	return home + "/.condense.lock"
}

// condenseLockHeldCode is the distinct exit code returned when condense-lock
// acquire finds a lock held by another process that has not yet gone stale.
// Skills can branch on this specific code to skip the condense run cleanly.
// Exit code 3 is already taken by *DepError; 5 is reserved here for "lock held".
const condenseLockHeldCode = 5

// condenseLockStaleAge is the threshold after which an existing lock is treated
// as stale and stolen by a new acquire. Exported only for unit-test injection.
const condenseLockStaleAge = 600 * time.Second

// condenseLockCmd implements `ateam condense-lock acquire|release`.
//
// acquire: atomically creates the lock file (O_CREATE|O_EXCL). If the file
// already exists, reads its timestamp. Locks older than condenseLockStaleAge
// are stolen (overwritten). Locks within the threshold cause the verb to exit
// with condenseLockHeldCode (a *cli.SilentError) so the calling skill can
// branch without treating it as a hard failure.
//
// release: removes the lock file if it exists (idempotent).
//
// The lock is advisory — only processes that call this verb respect it.
// No cgo, no syscall.Flock; uses only os primitives.
type condenseLockCmd struct {
	// nowFn is injected in tests to control the current time. nil => time.Now.
	nowFn func() time.Time
}

func (c *condenseLockCmd) Name() string { return "condense-lock" }

func (c *condenseLockCmd) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c *condenseLockCmd) Run(ctx *cli.Context, args []string) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense-lock: no context")
	}
	if len(args) == 0 || args[0] == "" {
		return cli.Usagef("ateam condense-lock: missing acquire|release")
	}
	action := args[0]
	path := condenseLockPath(ctx.Home)
	switch action {
	case "acquire":
		return c.acquire(ctx, path)
	case "release":
		return c.release(path)
	default:
		return cli.Usagef("ateam condense-lock: unknown action %q (want acquire or release)", action)
	}
}

// acquire tries to create the lock file atomically. On collision it reads the
// existing timestamp; if stale, it steals the lock; otherwise it returns a
// SilentError with condenseLockHeldCode so callers can detect the held state.
func (c *condenseLockCmd) acquire(ctx *cli.Context, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		return c.writeLock(f)
	}
	if !os.IsExist(err) {
		return fmt.Errorf("ateam condense-lock acquire: %w", err)
	}

	// File exists — check timestamp.
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		// Unreadable lock → treat as stale, steal it.
		return c.overwrite(path)
	}
	ts, parseErr := parseLockTimestamp(string(data))
	if parseErr != nil {
		// Unparseable → treat as stale.
		return c.overwrite(path)
	}
	age := c.now().Sub(time.Unix(ts, 0))
	if age >= condenseLockStaleAge {
		return c.overwrite(path)
	}

	// Lock is held and fresh.
	fmt.Fprintf(ctx.Stderr, "ateam condense-lock: lock held (age %s)\n", age.Round(time.Second))
	return cli.Silent(condenseLockHeldCode)
}

// writeLock writes pid and unix timestamp to an open (just-created) lock file.
func (c *condenseLockCmd) writeLock(f *os.File) error {
	defer f.Close()
	ts := c.now().Unix()
	_, err := fmt.Fprintf(f, "pid\n%d\n", ts)
	return err
}

// overwrite replaces the lock file non-atomically (write-truncate). Used only
// after confirming the existing lock is stale or unreadable.
func (c *condenseLockCmd) overwrite(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("ateam condense-lock acquire (steal): %w", err)
	}
	return c.writeLock(f)
}

// release removes the lock file. Idempotent: missing file is not an error.
func (c *condenseLockCmd) release(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ateam condense-lock release: %w", err)
	}
	return nil
}

// parseLockTimestamp extracts the unix timestamp from lock file content.
// Expected format: "pid\n<unix-ts>\n"
func parseLockTimestamp(content string) (int64, error) {
	lines := strings.SplitN(strings.TrimSpace(content), "\n", 3)
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected lock format")
	}
	return strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
}
