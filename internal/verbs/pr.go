// This file is owned by Track P (PR identity and routing, agent-teams-ssib.7).
// pr.go — `ateam pr add`, the sanctioned write path onto the "pr" rail
// (docs/multi-pr-contract.md §2b). Deliberately its own file, own
// RegisterPRKong, matching the established per-verb pattern (worktree_setup.go,
// spawncheck.go, preflight.go, ...) rather than growing kong_converted.go.
package verbs

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterPRKong registers the `pr` parent verb (currently just `pr add`) onto p.
func RegisterPRKong(p *cli.Parser) {
	p.AddVerb("pr", "Manage an initiative's recorded GitHub PR(s).", &prCmd{})
}

// prCmd is the kong parent struct for `ateam pr <subcommand>`. No Run method:
// kong runs every node with a Run method from the selected leaf up through its
// parents, so a Run here would fire on every subcommand (see mail_register.go).
type prCmd struct {
	Add prAddKong `cmd:"" name:"add" help:"Record a GitHub PR URL on an initiative's pr rail."`
}

// prAddKong implements `ateam pr add <initiative-id> <pr-url>` — the
// sanctioned write path onto the "pr" rail going forward
// (docs/multi-pr-contract.md §2b), via initiative.WithPR. Append-only and
// idempotent on a repeat URL: calling it again for a second, then a third PR
// is how those get recorded on one initiative.
type prAddKong struct {
	InitiativeID string  `arg:"" name:"initiative-id" help:"Initiative ID to record the PR on."`
	URL          string  `arg:"" name:"pr-url" help:"Full GitHub PR URL, e.g. https://github.com/owner/repo/pull/3."`
	Workstream   *string `name:"workstream" help:"Project Bead ID whose workstream owns this PR."`

	newProjectBD func(string) projectBDRunner `kong:"-"`
}

type projectBDRunner interface {
	Run(args ...string) (string, error)
}

// initiativeMutationLock serializes whole-description read/modify/write
// operations for one initiative across ateam processes. The lock file remains
// on disk after Close; only the kernel flock represents ownership, so a
// crashed process cannot leave a stale claim and release never races a new
// acquirer by unlinking the inode it locked.
type initiativeMutationLock struct {
	file *os.File
}

func initiativeMutationLockPath(home, initiativeID string) string {
	digest := sha256.Sum256([]byte(initiativeID))
	return filepath.Join(home, ".locks", "initiatives", fmt.Sprintf("%x.lock", digest))
}

func acquireInitiativeMutationLock(home, initiativeID string) (*initiativeMutationLock, error) {
	if home == "" {
		return nil, fmt.Errorf("agent-teams home is empty")
	}
	path := initiativeMutationLockPath(home, initiativeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create initiative lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open initiative lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("set initiative lock permissions: %w", err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("flock initiative lock: %w", err)
	}
	return &initiativeMutationLock{file: file}, nil
}

func (l *initiativeMutationLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock initiative lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close initiative lock: %w", closeErr)
	}
	return nil
}

// Validate rejects a malformed pr-url before any bd read/write. WithPR itself
// only enforces the field-line rule's structural constraints (no
// leading/trailing whitespace, no line break) — it deliberately does not
// validate the GitHub-URL shape (docs/multi-pr-contract.md §2, "It
// deliberately does NOT validate that url matches prURLRE"), leaving that to
// a caller with a reason to reject a malformed value. This is that caller.
func (c *prAddKong) Validate() error {
	if c.InitiativeID == "" {
		return cli.Usagef("ateam pr add: initiative-id must not be empty")
	}
	if !initiative.PRURLRE.MatchString(c.URL) {
		return cli.Usagef("ateam pr add: pr-url must be a full GitHub PR URL (https://github.com/<owner>/<repo>/pull/<number>), got %q", c.URL)
	}
	if c.Workstream != nil {
		if *c.Workstream == "" {
			return cli.Usagef("ateam pr add: --workstream must not be empty")
		}
		if strings.IndexFunc(*c.Workstream, unicode.IsSpace) >= 0 {
			return cli.Usagef("ateam pr add: --workstream must be a whitespace-free Bead ID, got %q", *c.Workstream)
		}
	}
	return nil
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *prAddKong) Run(ctx *cli.Context) (runErr error) {
	if ctx == nil {
		return fmt.Errorf("ateam pr add: no context")
	}
	lock, err := acquireInitiativeMutationLock(ctx.Home, c.InitiativeID)
	if err != nil {
		return fmt.Errorf("ateam pr add: acquire initiative %s lock: %w", c.InitiativeID, err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			lockErr := fmt.Errorf("ateam pr add: release initiative %s lock: %w", c.InitiativeID, err)
			if runErr == nil {
				runErr = lockErr
			} else {
				runErr = errors.Join(runErr, lockErr)
			}
		}
	}()

	// This is intentionally the first initiative read: every process re-reads
	// after acquiring the per-initiative lock, then holds that lock through
	// planning, descendant validation, and the final whole-description update.
	issue, err := bd.ShowIssue(ctx.BD, c.InitiativeID)
	if err != nil {
		return fmt.Errorf("ateam pr add: bd show %s: %w", c.InitiativeID, err)
	}

	// Seed the rail from the RESOLVED list — not Of(issue).PRs, the raw rail
	// alone — before appending the requested URL (agent-teams-ssib.23).
	// Of(issue).PRs is empty for the 178 initiatives whose PR was recorded
	// only in Notes (docs/multi-pr-contract.md §2a); seeding from it would
	// leave the rail holding ONLY the newly-added PR, and ResolvedPRs' rail-
	// wins-wholesale rule would then make the legacy Notes-only PR vanish
	// from every consumer the moment this second PR lands — a regression
	// against today, not an improvement. Seeding through WithPR (not a raw
	// append) reuses the same canonicalizing dedup so a resolved PR that
	// happens to already canonically equal c.URL doesn't get written twice.
	seeded := issue
	for _, existing := range initiative.ResolvedPRs(issue) {
		plan, err := initiative.WithPR(seeded, existing)
		if err != nil {
			return fmt.Errorf("ateam pr add: seed existing PR %s: %w", existing, err)
		}
		seeded.Description = plan.Description
	}

	plan, err := initiative.WithPR(seeded, c.URL)
	if err != nil {
		return fmt.Errorf("ateam pr add: %w", err)
	}

	if c.Workstream != nil {
		withPR := issue
		withPR.Description = plan.Description
		associationPlan, err := initiative.WithPRWorkstream(withPR, c.URL, *c.Workstream)
		if err != nil {
			return fmt.Errorf("ateam pr add: %w", err)
		}
		if err := c.validateWorkstream(issue, *c.Workstream); err != nil {
			return err
		}
		plan = associationPlan
	}
	if plan.Description == issue.Description {
		fmt.Fprintf(ctx.Stdout, "pr add: %s already recorded on %s\n", c.URL, c.InitiativeID)
		return nil
	}

	tmpFile, err := os.CreateTemp("", "ateam-pr-add-*.txt")
	if err != nil {
		return fmt.Errorf("ateam pr add: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(plan.Description); err != nil {
		tmpFile.Close()
		return fmt.Errorf("ateam pr add: write temp file: %w", err)
	}
	tmpFile.Close()

	if _, err := ctx.BD.Run("update", c.InitiativeID, "--body-file="+tmpPath); err != nil {
		return fmt.Errorf("ateam pr add: bd update %s: %w", c.InitiativeID, err)
	}
	fmt.Fprintf(ctx.Stdout, "pr add: recorded %s on %s\n", c.URL, c.InitiativeID)
	return nil
}

type projectIssue struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
}

func showProjectIssue(runner projectBDRunner, id string) (projectIssue, error) {
	out, err := runner.Run("show", id, "--json")
	if err != nil {
		return projectIssue{}, err
	}
	var issues []projectIssue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return projectIssue{}, fmt.Errorf("decode bd show %s: %w", id, err)
	}
	if len(issues) == 0 || issues[0].ID == "" {
		return projectIssue{}, fmt.Errorf("bd show %s: not found", id)
	}
	return issues[0], nil
}

func (c *prAddKong) validateWorkstream(initiativeIssue bd.Issue, workstream string) error {
	fields := initiative.Of(initiativeIssue)
	if fields.Repo == "" {
		return fmt.Errorf("ateam pr add: --workstream requires initiative %s to have a non-empty repo field", c.InitiativeID)
	}
	if fields.Epic == "" {
		return fmt.Errorf("ateam pr add: --workstream requires initiative %s to have a non-empty epic field", c.InitiativeID)
	}
	if workstream == fields.Epic {
		return fmt.Errorf("ateam pr add: workstream %s is the initiative epic itself, not a descendant", workstream)
	}

	newProjectBD := c.newProjectBD
	if newProjectBD == nil {
		newProjectBD = func(repo string) projectBDRunner { return bd.NewClient(repo) }
	}
	projectBD := newProjectBD(fields.Repo)
	if projectBD == nil {
		return fmt.Errorf("ateam pr add: inspect project %s: no bd client", fields.Repo)
	}

	seen := make(map[string]struct{})
	current := workstream
	for {
		if _, duplicate := seen[current]; duplicate {
			return fmt.Errorf("ateam pr add: workstream %s parent chain contains a cycle at %s", workstream, current)
		}
		seen[current] = struct{}{}

		projectIssue, err := showProjectIssue(projectBD, current)
		if err != nil {
			return fmt.Errorf("ateam pr add: inspect workstream %s in project %s: %w", current, fields.Repo, err)
		}
		if projectIssue.Parent == fields.Epic {
			return nil
		}
		if projectIssue.Parent == "" {
			return fmt.Errorf("ateam pr add: workstream %s is not a descendant of epic %s", workstream, fields.Epic)
		}
		current = projectIssue.Parent
	}
}

// resolvePR canonicalizes pr and requires it to identify one of id's actual
// resolved PRs (initiative.ResolvedPRs), not just look like a well-formed
// GitHub PR URL — the shared --pr resolver for gate/clear-gate/handoff
// (agent-teams-ssib.25). Returns the canonical form (initiative.
// CanonicalPRURL) to embed in a per-PR label, so a label built from any
// spelling of a PR always lines up byte-for-byte with the rail and with
// every other per-PR label for that same PR. Also returns the bd.Issue read
// to resolve against, so a caller that needs the current label set too
// (e.g. clear-gate's bare-gate guard, agent-teams-ssib.30) reuses this one
// read instead of issuing a second `bd show`.
//
// verb names the caller (e.g. "ateam gate") for the error message.
//
// Fails loudly — a rejected command — rather than minting a label for a PR
// the initiative doesn't actually have: an orphaned per-PR label can never
// be paired (gate cannot find a matching handoff) or reliably cleared by
// --pr again, which is strictly worse than making the human re-run the
// command with the right URL.
func resolvePR(ctx *cli.Context, verb, id, pr string) (string, bd.Issue, error) {
	canon, ok := initiative.CanonicalPRURL(pr)
	if !ok {
		return "", bd.Issue{}, cli.Usagef("%s: --pr must be a full GitHub PR URL (https://github.com/<owner>/<repo>/pull/<number>), got %q", verb, pr)
	}
	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return "", bd.Issue{}, fmt.Errorf("%s: bd show %s: %w", verb, id, err)
	}
	resolved := initiative.ResolvedPRs(issue)
	for _, r := range resolved {
		if r == canon {
			return canon, issue, nil
		}
	}
	return "", bd.Issue{}, cli.Usagef("%s: --pr %s is not a PR recorded on %s (recorded PRs: %v)", verb, pr, id, resolved)
}
