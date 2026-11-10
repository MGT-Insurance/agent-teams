// This file is owned by Track C (write verbs).
package verbs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
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

// condenseBDLockStaleAge is the stale threshold for the bd-backed lock. It is
// longer than the file lock's because a hosted condense run spans several
// model calls.
const condenseBDLockStaleAge = 60 * time.Minute

// condenseLockBeadSuffix is appended to the workspace issue prefix to form the
// lock issue id. findOffenders exempts any issue whose id ends with it.
const condenseLockBeadSuffix = "-condense-lock"

// condenseLockHolderPrefix starts every bd lock holder string:
// condense:<who>@<unix>.
const condenseLockHolderPrefix = "condense:"

// condenseLockCmd implements `ateam condense-lock acquire|release`.
//
// Two backends exist. With cloud_home set (learnRemote) or DSP_BEAD non-empty
// the lock is a closed bd issue in the learnings store (see acquireBD); every
// other case uses the file lock described below, unchanged.
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
	// whoFn is injected in tests to control the lock owner name. nil =>
	// DSP_BEAD when set, else the hostname.
	whoFn func() string
}

// who returns the owner name used in bd lock holder strings.
func (c *condenseLockCmd) who() string {
	if c.whoFn != nil {
		return c.whoFn()
	}
	if b := os.Getenv("DSP_BEAD"); b != "" {
		return b
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown-host"
	}
	return h
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

// ── bd-backed lock ────────────────────────────────────────────────────────────

// bdHeldExitCode is the exit status bd update returns when --if-assignee does
// not match the current assignee.
const bdHeldExitCode = 13

// condenseLockUsesBD reports whether the condense lock lives in the learnings
// store: when learnings are routed to a hosted workspace, or inside a
// Dispatcher run (DSP_BEAD set).
func condenseLockUsesBD(ctx *cli.Context) bool {
	return learnRemote(ctx) || os.Getenv("DSP_BEAD") != ""
}

// isBDHeld reports whether err is a bd update refusal from --if-assignee.
func isBDHeld(err error) bool {
	var ee interface{ ExitCode() int }
	return errors.As(err, &ee) && ee.ExitCode() == bdHeldExitCode
}

// lockUnreachable prints the frozen skip line and returns the held exit when
// err is an unreachable hosted store. ok reports whether it handled err.
func lockUnreachable(ctx *cli.Context, err error) (handled error, ok bool) {
	if !learnUnreachable(ctx, err) {
		return nil, false
	}
	fmt.Fprintln(ctx.Stderr, "ateam condense-lock: learnings store unreachable; condense skipped")
	return cli.Silent(condenseLockHeldCode), true
}

// lockBeadID reads the learnings workspace's issue prefix from bd and returns
// the lock issue id. It goes through readConfigValue, the only sanctioned bd
// config read, pointed at the learnings runner.
func lockBeadID(ctx *cli.Context) (string, error) {
	learn := *ctx
	learn.BD = learnBD(ctx)
	prefix, err := readConfigValue(&learn, "issue_prefix")
	if err != nil {
		return "", err
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", fmt.Errorf("bd config issue_prefix is empty")
	}
	return prefix + condenseLockBeadSuffix, nil
}

// holderWho extracts <who> from a holder string condense:<who>@<unix>. ok is
// false when the string is not in that form.
func holderWho(holder string) (who string, ok bool) {
	rest, found := strings.CutPrefix(holder, condenseLockHolderPrefix)
	if !found {
		return "", false
	}
	i := strings.LastIndex(rest, "@")
	if i < 0 {
		return "", false
	}
	return rest[:i], true
}

// holderTimestamp extracts the unix timestamp from a holder string.
func holderTimestamp(holder string) (int64, error) {
	i := strings.LastIndex(holder, "@")
	if i < 0 {
		return 0, fmt.Errorf("unexpected holder format")
	}
	return strconv.ParseInt(holder[i+1:], 10, 64)
}

// currentHolder returns the lock issue's assignee ("" when free).
func currentHolder(ctx *cli.Context, id string) (string, error) {
	iss, err := bd.ShowIssue(learnBD(ctx), id)
	if err != nil {
		return "", err
	}
	return iss.Assignee, nil
}

// ensureLockBead creates the lock issue and closes it, so default bd list never
// shows it. A duplicate-id create fails, which is fine: the issue already
// exists. A failed create surfaces on the retried update.
func ensureLockBead(ctx *cli.Context, id string) {
	r := learnBD(ctx)
	if _, err := r.Run("create", "--id", id, "--title", "condense lock", "-t", "task"); err != nil {
		return
	}
	_, _ = r.Run("close", id, "--reason", "condense lock bead")
}

// acquireBD takes the lock on the closed lock issue with a compare-and-set on
// the assignee. A holder older than condenseBDLockStaleAge is stolen.
func (c *condenseLockCmd) acquireBD(ctx *cli.Context) error {
	id, err := lockBeadID(ctx)
	if err != nil {
		if e, ok := lockUnreachable(ctx, err); ok {
			return e
		}
		return fmt.Errorf("ateam condense-lock acquire: %w", err)
	}
	holder := fmt.Sprintf("%s%s@%d", condenseLockHolderPrefix, c.who(), c.now().Unix())
	r := learnBD(ctx)

	_, err = r.Run("update", id, "--if-assignee", "", "--assignee", holder)
	if err != nil && !isBDHeld(err) && !learnUnreachable(ctx, err) {
		// The lock issue may not exist yet: create it and retry once.
		ensureLockBead(ctx, id)
		_, err = r.Run("update", id, "--if-assignee", "", "--assignee", holder)
	}
	if err == nil {
		return nil
	}
	if e, ok := lockUnreachable(ctx, err); ok {
		return e
	}
	if !isBDHeld(err) {
		return fmt.Errorf("ateam condense-lock acquire: %w", err)
	}

	old, err := currentHolder(ctx, id)
	if err != nil {
		if e, ok := lockUnreachable(ctx, err); ok {
			return e
		}
		return fmt.Errorf("ateam condense-lock acquire: %w", err)
	}
	ts, tsErr := holderTimestamp(old)
	if old != "" && tsErr == nil && c.now().Sub(time.Unix(ts, 0)) < condenseBDLockStaleAge {
		fmt.Fprintf(ctx.Stderr, "ateam condense-lock: lock held by %s\n", old)
		return cli.Silent(condenseLockHeldCode)
	}

	// Free (released between the two calls), stale, or unparseable: take it
	// only if the holder is still the one we read.
	_, err = r.Run("update", id, "--if-assignee", old, "--assignee", holder)
	if err == nil {
		return nil
	}
	if e, ok := lockUnreachable(ctx, err); ok {
		return e
	}
	if !isBDHeld(err) {
		return fmt.Errorf("ateam condense-lock acquire (steal): %w", err)
	}
	// Someone else took it first.
	now, readErr := currentHolder(ctx, id)
	if readErr != nil {
		now = old
	}
	fmt.Fprintf(ctx.Stderr, "ateam condense-lock: lock held by %s\n", now)
	return cli.Silent(condenseLockHeldCode)
}

// releaseBD clears the assignee only when the holder's <who> is ours. A free
// lock or another owner's lock prints the frozen line and exits 0.
func (c *condenseLockCmd) releaseBD(ctx *cli.Context) error {
	who := c.who()
	notHeld := func() error {
		fmt.Fprintf(ctx.Stdout, "ateam condense-lock: not held by %s; nothing released\n", who)
		return nil
	}
	id, err := lockBeadID(ctx)
	if err != nil {
		return fmt.Errorf("ateam condense-lock release: %w", err)
	}
	holder, err := currentHolder(ctx, id)
	if err != nil {
		if learnUnreachable(ctx, err) {
			return fmt.Errorf("ateam condense-lock release: learnings store unreachable (%s)", learnStoreName(ctx))
		}
		if strings.Contains(err.Error(), "not found") {
			return notHeld()
		}
		return fmt.Errorf("ateam condense-lock release: %w", err)
	}
	if owner, ok := holderWho(holder); !ok || owner != who {
		return notHeld()
	}
	if _, err := learnBD(ctx).Run("update", id, "--if-assignee", holder, "--assignee", ""); err != nil {
		if isBDHeld(err) {
			return notHeld()
		}
		return fmt.Errorf("ateam condense-lock release: %w", err)
	}
	return nil
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
	useBD := condenseLockUsesBD(ctx)
	switch c.Action {
	case "acquire":
		if useBD {
			return impl.acquireBD(ctx)
		}
		return impl.acquire(ctx, path)
	case "release":
		if useBD {
			return impl.releaseBD(ctx)
		}
		return impl.release(path)
	default:
		return cli.Usagef("ateam condense-lock: unknown action %q (want acquire or release)", c.Action)
	}
}
