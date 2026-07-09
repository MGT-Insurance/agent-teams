package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/gitutil"
)

// RunManifest is produced by `eval run`, one JSON per sample.
// Persisted under eval/runs/<RunID>/manifest.json
type RunManifest struct {
	RunID        string            `json:"runId"` // taskID + "-" + config.Hash() + "-" + unix-ts
	TaskID       string            `json:"taskId"`
	Config       ConfigFingerprint `json:"config"`
	InitiativeID string            `json:"initiativeId"` // the ateam-dispatch initiative id (cost/transcript discovery key)
	Branch       string            `json:"branch"`
	WorktreePath string            `json:"worktreePath"`
	StartedAt    time.Time         `json:"startedAt"`
}

// Run resolves task.FixtureRepo to a local clone under EVAL_FIXTURES_DIR
// (clone if a URL / not yet cached), then shells
// `ateam dispatch --repo <resolved-clone> --base-branch <task.FixtureRef>
// --problem <task.Problem> --model <cfg.DRIModel> [--advisor <cfg.Advisor>]`,
// captures the initiative id + branch + worktree, writes manifest.json.
func Run(task TaskSpec, cfg ConfigFingerprint) (RunManifest, error) {
	now := time.Now()
	runID := fmt.Sprintf("%s-%s-%d", task.ID, cfg.Hash(), now.Unix())

	if err := checkRunIDAvailable(runID); err != nil {
		return RunManifest{}, err
	}

	repoDir, err := resolveFixtureClone(task.FixtureRepo, fixturesDir())
	if err != nil {
		return RunManifest{}, fmt.Errorf("eval: resolve fixture %s: %w", task.FixtureRepo, err)
	}

	stdout, err := runDispatch(dispatchArgs(repoDir, task, cfg, runID))
	if err != nil {
		return RunManifest{}, fmt.Errorf("eval: ateam dispatch: %w", err)
	}

	initiativeID, worktree, slug := parseDispatchOutput(stdout)
	if initiativeID == "" {
		return RunManifest{}, fmt.Errorf("eval: ateam dispatch: could not find initiative_id in output:\n%s", stdout)
	}

	manifest := RunManifest{
		RunID:        runID,
		TaskID:       task.ID,
		Config:       cfg,
		InitiativeID: initiativeID,
		Branch:       slug,
		WorktreePath: worktree,
		StartedAt:    now,
	}

	if err := writeManifest(manifest); err != nil {
		return RunManifest{}, err
	}

	return manifest, nil
}

// dispatchArgs builds the `ateam dispatch` argv for launching task under cfg
// against the fixture checkout at repoDir.
//
// --slug runID is required: without it, dispatch derives the worktree/branch
// slug purely from --problem (dispatch.go: gitutil.Slugify(c.Problem)), so
// two configs run against the SAME task (same Problem) collide on the same
// worktree and the second dispatch call hard-fails on the existing-worktree
// check — silently preventing the very A-vs-B comparison this harness exists
// for (see agent-teams-grft.14). runID is already unique per (task, config,
// sample) and slug-safe, and dispatch uses an explicit --slug verbatim (no
// re-slugify, no truncation), so it doubles as the worktree/branch name.
//
// --launch-prompt is required for --model/--advisor to actually reach the
// launched session: ateam dispatch's default /dri path ignores both flags and
// derives model/advisor from CLAUDE_PLUGIN_OPTION_USE_ADVISORS instead, so
// every run would launch under the same ambient config regardless of cfg
// (same discovery bead). The prompt substituted here is byte-identical to
// what the default path sends: "/dri " + the initiative id.
func dispatchArgs(repoDir string, task TaskSpec, cfg ConfigFingerprint, runID string) []string {
	args := []string{
		"dispatch",
		"--repo", repoDir,
		"--base-branch", task.FixtureRef,
		"--problem", task.Problem,
		"--slug", runID,
		"--model", cfg.DRIModel,
	}
	if cfg.Advisor != "" {
		args = append(args, "--advisor", cfg.Advisor)
	}
	// MUST stay byte-identical to "/dri <id>": L3 metrics discovery
	// (cost.Attribute -> discoverSessions) matches state.json intent against
	// exactly this string; a divergent prompt silently zeros all metrics
	// (Attribute returns a zero Report, not an error) instead of failing.
	args = append(args, "--launch-prompt", "/dri {id}")
	return args
}

// parseDispatchOutput extracts the "key: value" lines `ateam dispatch` prints
// to stdout (see internal/verbs/dispatch.go dispatchKong.Run, step 9 — the
// same format regardless of --launch-prompt).
func parseDispatchOutput(stdout string) (initiativeID, worktree, slug string) {
	for _, line := range strings.Split(stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "initiative_id: "):
			initiativeID = strings.TrimSpace(strings.TrimPrefix(line, "initiative_id: "))
		case strings.HasPrefix(line, "worktree: "):
			worktree = strings.TrimSpace(strings.TrimPrefix(line, "worktree: "))
		case strings.HasPrefix(line, "slug: "):
			slug = strings.TrimSpace(strings.TrimPrefix(line, "slug: "))
		}
	}
	return
}

// checkRunIDAvailable guards the same-(task,config) collision: RunID is
// second-granularity (task.ID + "-" + cfg.Hash() + "-" + unix-ts), so two
// `eval run` invocations for the same task+config within the same second
// would silently overwrite each other's manifest. v1 is invoked serially by
// a human operator (CONTRACT COMPLETION SEAM), so a clear pre-dispatch error
// is enough — no locking or retry. Checked before ateam dispatch runs, not
// after, so a collision never launches a duplicate DRI session.
func checkRunIDAvailable(runID string) error {
	dir := filepath.Join("eval", "runs", runID)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("eval: run %s already exists at %s (RunIDs collide at second granularity — wait a second and retry)", runID, dir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("eval: stat run dir %s: %w", dir, err)
	}
	return nil
}

// writeManifest persists m under the frozen relative path eval/runs/<RunID>/manifest.json.
func writeManifest(m RunManifest) error {
	dir := filepath.Join("eval", "runs", m.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: create run dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal manifest: %w", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("eval: write manifest %s: %w", path, err)
	}
	return nil
}

// fixturesDir returns EVAL_FIXTURES_DIR, defaulting to ~/.agent-teams-eval-fixtures/.
func fixturesDir() string {
	if d := os.Getenv("EVAL_FIXTURES_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".agent-teams-eval-fixtures")
}

// resolveFixtureClone resolves fixtureRepo to a local, non-bare clone under
// cacheDir, cloning on first use and reusing it on every later call. A
// fixtureRepo that is already a local directory (the v1 durability seam: a
// fixture may start life as a local-only repo before a hosted home is
// chosen — see agent-teams-grft.1) is used in place, with no cache copy.
//
// fixtureRepo may also be a bare name (e.g. "webapp-medium", as
// eval/tasks/webapp-bugfix-1.json uses) rather than a path or URL, so task
// specs stay portable across machines/EVAL_FIXTURES_DIR locations. That form
// resolves directly to cacheDir/<name> — NOT cacheDir/clones/<name>, which is
// reserved for repos this function itself clones from a URL.
func resolveFixtureClone(fixtureRepo, cacheDir string) (string, error) {
	if fi, err := os.Stat(fixtureRepo); err == nil && fi.IsDir() {
		return fixtureRepo, nil
	}

	direct := filepath.Join(cacheDir, fixtureRepo)
	if fi, err := os.Stat(filepath.Join(direct, ".git")); err == nil && fi.IsDir() {
		return direct, nil
	}

	name := gitutil.Slugify(strings.TrimSuffix(filepath.Base(fixtureRepo), ".git"))
	if name == "" {
		sum := sha256.Sum256([]byte(fixtureRepo))
		name = "fixture-" + hex.EncodeToString(sum[:4])
	}
	cloneDir := filepath.Join(cacheDir, "clones", name)

	if fi, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil && fi.IsDir() {
		return cloneDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		return "", fmt.Errorf("mkdir fixtures cache dir: %w", err)
	}
	if err := runGitClone(fixtureRepo, cloneDir); err != nil {
		return "", fmt.Errorf("git clone %s: %w", fixtureRepo, err)
	}
	return cloneDir, nil
}

// runGitClone and runDispatch are the two external-process seams Run() shells
// out through. Package-level vars so tests substitute fakes and never clone a
// real repo or launch a real DRI session — a real `ateam dispatch` invocation
// is the L6 integrator's live concern, not this package's test suite.
var runGitClone = execGitClone

var runDispatch = execDispatch

func execGitClone(fixtureRepo, dest string) error {
	cmd := exec.Command("git", "clone", fixtureRepo, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func execDispatch(args []string) (string, error) {
	if _, err := exec.LookPath("ateam"); err != nil {
		return "", fmt.Errorf("'ateam' not found in PATH: %w", err)
	}
	cmd := exec.Command("ateam", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return out.String(), err
}
