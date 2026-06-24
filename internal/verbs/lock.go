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

// RegisterCondenseLock registers the condense-lock verb onto p using a native
// kong struct.
func RegisterCondenseLock(p *cli.Parser) {
	p.AddVerb("condense-lock", "Acquire or release the advisory condense lock.", &condenseLockKong{})
}

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

func (c *condenseLockCmd) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
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

// writeLock writes the current process PID and unix timestamp to an open
// (just-created or stolen) lock file. Format: "<pid>\n<unix-ts>\n".
// parseLockTimestamp reads lines[1] for the timestamp; the PID is on lines[0].
func (c *condenseLockCmd) writeLock(f *os.File) error {
	defer f.Close()
	ts := c.now().Unix()
	_, err := fmt.Fprintf(f, "%d\n%d\n", os.Getpid(), ts)
	return err
}

// overwrite replaces the lock file non-atomically (create-or-truncate). Used
// only after confirming the existing lock is stale or unreadable. O_CREATE is
// included so that a lock released between the stale-read and this steal does
// not cause a "no such file" error — we simply re-create it.
func (c *condenseLockCmd) overwrite(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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
// Expected format: "<pid>\n<unix-ts>\n"
func parseLockTimestamp(content string) (int64, error) {
	lines := strings.SplitN(strings.TrimSpace(content), "\n", 3)
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected lock format")
	}
	return strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
}

// ── native kong struct ────────────────────────────────────────────────────────

// condenseLockKong is the kong-converted form of condenseLockCmd.
// Action is a required positional enum: acquire or release.
type condenseLockKong struct {
	Action string `arg:"" name:"action" enum:"acquire,release" help:"Lock action: acquire or release."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *condenseLockKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam condense-lock: no context")
	}
	path := condenseLockPath(ctx.Home)
	impl := &condenseLockCmd{}
	switch c.Action {
	case "acquire":
		return impl.acquire(ctx, path)
	case "release":
		return impl.release(path)
	default:
		return cli.Usagef("ateam condense-lock: unknown action %q (want acquire or release)", c.Action)
	}
}
