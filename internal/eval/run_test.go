package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubGitClone replaces runGitClone with fn for the duration of t, restoring
// the real implementation on cleanup.
func stubGitClone(t *testing.T, fn func(repo, dest string) error) {
	t.Helper()
	orig := runGitClone
	runGitClone = fn
	t.Cleanup(func() { runGitClone = orig })
}

// stubDispatch replaces runDispatch with fn for the duration of t, restoring
// the real implementation on cleanup.
func stubDispatch(t *testing.T, fn func(args []string) (string, error)) {
	t.Helper()
	orig := runDispatch
	runDispatch = fn
	t.Cleanup(func() { runDispatch = orig })
}

func assertContainsPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("argv %v missing %q %q", args, flag, value)
}

// argValue returns the value following flag in args, mirroring how a real
// `ateam dispatch --slug X` echoes X verbatim in its "slug: X" stdout line.
func argValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func TestDispatchArgs_WithAdvisor(t *testing.T) {
	task := TaskSpec{FixtureRef: "v1-buggy", Problem: "fix it"}
	cfg := ConfigFingerprint{DRIModel: "sonnet", Advisor: "opus"}
	args := dispatchArgs("/tmp/repo", task, cfg, "task1-abc123-1700000000")

	assertContainsPair(t, args, "--repo", "/tmp/repo")
	assertContainsPair(t, args, "--base-branch", "v1-buggy")
	assertContainsPair(t, args, "--problem", "fix it")
	assertContainsPair(t, args, "--slug", "task1-abc123-1700000000")
	assertContainsPair(t, args, "--model", "sonnet")
	assertContainsPair(t, args, "--advisor", "opus")
	assertContainsPair(t, args, "--launch-prompt", "/dri {id}")
}

func TestDispatchArgs_NoAdvisorOmitsFlag(t *testing.T) {
	task := TaskSpec{FixtureRef: "v1-buggy", Problem: "fix it"}
	cfg := ConfigFingerprint{DRIModel: "opus", Advisor: ""}
	args := dispatchArgs("/tmp/repo", task, cfg, "task1-def456-1700000001")

	for i, a := range args {
		if a == "--advisor" {
			t.Fatalf("did not expect --advisor in argv when cfg.Advisor is empty, got %v at index %d", args, i)
		}
	}
	assertContainsPair(t, args, "--slug", "task1-def456-1700000001")
	assertContainsPair(t, args, "--model", "opus")
	assertContainsPair(t, args, "--launch-prompt", "/dri {id}")
}

// TestDispatchArgs_SlugDiffersAcrossConfigsForSameTask guards the collision
// this package exists to avoid: without an explicit --slug, ateam dispatch
// derives the worktree/branch slug purely from --problem (dispatch.go:
// gitutil.Slugify(c.Problem)), so two configs run against the SAME task
// would collide on the same worktree and the second dispatch call would
// hard-fail on the existing-worktree check (agent-teams-grft.14).
func TestDispatchArgs_SlugDiffersAcrossConfigsForSameTask(t *testing.T) {
	task := TaskSpec{FixtureRef: "v1-buggy", Problem: "fix it"}
	cfgA := ConfigFingerprint{Name: "opus-no-advisor", DRIModel: "opus"}
	cfgB := ConfigFingerprint{Name: "sonnet-advisor", DRIModel: "sonnet", Advisor: "opus"}

	runIDA := "task1-" + cfgA.Hash() + "-1700000000"
	runIDB := "task1-" + cfgB.Hash() + "-1700000000"
	if runIDA == runIDB {
		t.Fatalf("expected distinct runIDs for distinct configs, both = %q", runIDA)
	}

	argsA := dispatchArgs("/tmp/repo", task, cfgA, runIDA)
	argsB := dispatchArgs("/tmp/repo", task, cfgB, runIDB)
	assertContainsPair(t, argsA, "--slug", runIDA)
	assertContainsPair(t, argsB, "--slug", runIDB)
}

func TestParseDispatchOutput(t *testing.T) {
	stdout := "initiative_id: agent-teams-abcd\n" +
		"worktree: /Users/erlloyd/.agent-teams-worktrees/some-slug\n" +
		"slug: some-slug\n" +
		"base_branch: v1-buggy\n" +
		"team: agent-teams-some-slug\n" +
		"\nBackground session launched: some-slug\n"

	id, worktree, slug := parseDispatchOutput(stdout)
	if id != "agent-teams-abcd" {
		t.Errorf("initiativeID = %q, want %q", id, "agent-teams-abcd")
	}
	if worktree != "/Users/erlloyd/.agent-teams-worktrees/some-slug" {
		t.Errorf("worktree = %q", worktree)
	}
	if slug != "some-slug" {
		t.Errorf("slug = %q", slug)
	}
}

func TestResolveFixtureClone_LocalDirUsedInPlace(t *testing.T) {
	local := t.TempDir()
	stubGitClone(t, func(repo, dest string) error {
		t.Fatal("runGitClone should not be called for an already-local fixture dir")
		return nil
	})

	got, err := resolveFixtureClone(local, t.TempDir())
	if err != nil {
		t.Fatalf("resolveFixtureClone: %v", err)
	}
	if got != local {
		t.Errorf("got %q, want %q", got, local)
	}
}

func TestResolveFixtureClone_ClonesOnFirstUse(t *testing.T) {
	cacheDir := t.TempDir()
	var gotRepo, gotDest string
	stubGitClone(t, func(repo, dest string) error {
		gotRepo, gotDest = repo, dest
		return os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	})

	cloneDir, err := resolveFixtureClone("https://example.com/fixtures/webapp-medium.git", cacheDir)
	if err != nil {
		t.Fatalf("resolveFixtureClone: %v", err)
	}
	if gotRepo != "https://example.com/fixtures/webapp-medium.git" {
		t.Errorf("cloned wrong repo: %q", gotRepo)
	}
	if gotDest != cloneDir {
		t.Errorf("clone dest %q != returned clone dir %q", gotDest, cloneDir)
	}
	wantDir := filepath.Join(cacheDir, "clones", "webapp-medium")
	if cloneDir != wantDir {
		t.Errorf("cloneDir = %q, want %q", cloneDir, wantDir)
	}
}

func TestResolveFixtureClone_ReusesExistingClone(t *testing.T) {
	cacheDir := t.TempDir()
	cloneDir := filepath.Join(cacheDir, "clones", "webapp-medium")
	if err := os.MkdirAll(filepath.Join(cloneDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubGitClone(t, func(repo, dest string) error {
		t.Fatal("runGitClone should not be called when the clone already exists")
		return nil
	})

	got, err := resolveFixtureClone("https://example.com/fixtures/webapp-medium.git", cacheDir)
	if err != nil {
		t.Fatalf("resolveFixtureClone: %v", err)
	}
	if got != cloneDir {
		t.Errorf("got %q, want %q", got, cloneDir)
	}
}

func TestRun_WritesManifest(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	stubGitClone(t, func(repo, dest string) error {
		t.Fatal("runGitClone should not be called for an already-local fixture dir")
		return nil
	})

	// The fake echoes back the --slug it was passed, exactly as a real
	// `ateam dispatch --slug X` would print "slug: X" verbatim (dispatch.go
	// never re-slugifies an explicit --slug). This lets the assertions below
	// catch a regression to the pre-fix behavior where the slug was a
	// constant, hiding the same-task-different-config collision.
	var gotArgs []string
	stubDispatch(t, func(args []string) (string, error) {
		gotArgs = args
		slug := argValue(args, "--slug")
		return "initiative_id: agent-teams-abcd\n" +
			"worktree: /Users/erlloyd/.agent-teams-worktrees/" + slug + "\n" +
			"slug: " + slug + "\n" +
			"base_branch: v1-buggy\n" +
			"team: agent-teams-" + slug + "\n" +
			"\nBackground session launched: " + slug + "\n", nil
	})

	workDir := t.TempDir()
	t.Chdir(workDir)

	task := TaskSpec{
		ID:                 "sample-task-1",
		Archetype:          "webapp-bugfix",
		RunShape:           "implement",
		FixtureRepo:        repoDir,
		FixtureRef:         "v1-buggy",
		Problem:            "fix it",
		AcceptanceCriteria: []string{"criterion 1"},
		BuildCheck:         "go test ./...",
	}
	cfg := ConfigFingerprint{Name: "sonnet-advisor", DRIModel: "sonnet", Advisor: "opus"}

	before := time.Now()
	manifest, err := Run(task, cfg)
	after := time.Now()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantRunIDPrefix := "sample-task-1-" + cfg.Hash() + "-"
	if !strings.HasPrefix(manifest.RunID, wantRunIDPrefix) {
		t.Errorf("RunID = %q, want prefix %q", manifest.RunID, wantRunIDPrefix)
	}
	if manifest.TaskID != "sample-task-1" {
		t.Errorf("TaskID = %q", manifest.TaskID)
	}
	if manifest.InitiativeID != "agent-teams-abcd" {
		t.Errorf("InitiativeID = %q", manifest.InitiativeID)
	}
	// Branch must equal the RunID we asked dispatch to use as --slug — not a
	// value derived from task.Problem, which would collide across configs
	// run against the same task (agent-teams-grft.14).
	if manifest.Branch != manifest.RunID {
		t.Errorf("Branch = %q, want RunID %q", manifest.Branch, manifest.RunID)
	}
	if manifest.WorktreePath != "/Users/erlloyd/.agent-teams-worktrees/"+manifest.RunID {
		t.Errorf("WorktreePath = %q", manifest.WorktreePath)
	}
	if manifest.StartedAt.Before(before) || manifest.StartedAt.After(after) {
		t.Errorf("StartedAt %v not within [%v, %v]", manifest.StartedAt, before, after)
	}
	if manifest.Config.Hash() != cfg.Hash() {
		t.Errorf("Config in manifest does not match cfg")
	}

	// dispatch argv correctness — the assertion the L6 integrator depends on:
	// without --launch-prompt, --model/--advisor are silently ignored by
	// ateam dispatch's default /dri launch path (agent-teams-grft.14).
	assertContainsPair(t, gotArgs, "--repo", repoDir)
	assertContainsPair(t, gotArgs, "--base-branch", "v1-buggy")
	assertContainsPair(t, gotArgs, "--slug", manifest.RunID)
	assertContainsPair(t, gotArgs, "--model", "sonnet")
	assertContainsPair(t, gotArgs, "--advisor", "opus")
	assertContainsPair(t, gotArgs, "--launch-prompt", "/dri {id}")

	// manifest.json persisted at the frozen relative path.
	manifestPath := filepath.Join(workDir, "eval", "runs", manifest.RunID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json not written: %v", err)
	}
	var onDisk RunManifest
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("manifest.json invalid JSON: %v", err)
	}
	if onDisk.RunID != manifest.RunID {
		t.Errorf("on-disk RunID = %q, want %q", onDisk.RunID, manifest.RunID)
	}
}

// ---- resolveFixtureClone: bare-name form (agent-teams-grft.7 integration gap #1) --

func TestResolveFixtureClone_BareNameResolvesUnderCacheDirDirectly(t *testing.T) {
	cacheDir := t.TempDir()
	fixtureDir := filepath.Join(cacheDir, "webapp-medium")
	if err := os.MkdirAll(filepath.Join(fixtureDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubGitClone(t, func(repo, dest string) error {
		t.Fatal("runGitClone should not be called for a bare-name fixture already present under cacheDir")
		return nil
	})

	got, err := resolveFixtureClone("webapp-medium", cacheDir)
	if err != nil {
		t.Fatalf("resolveFixtureClone: %v", err)
	}
	if got != fixtureDir {
		t.Errorf("got %q, want %q (bare name resolves directly under cacheDir, not cacheDir/clones/<name>)", got, fixtureDir)
	}
}

func TestResolveFixtureClone_BareNameNotYetCached_FallsThroughToClone(t *testing.T) {
	cacheDir := t.TempDir()
	var gotRepo string
	stubGitClone(t, func(repo, dest string) error {
		gotRepo = repo
		return os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	})

	cloneDir, err := resolveFixtureClone("webapp-medium", cacheDir)
	if err != nil {
		t.Fatalf("resolveFixtureClone: %v", err)
	}
	if gotRepo != "webapp-medium" {
		t.Errorf("cloned wrong repo: %q", gotRepo)
	}
	wantDir := filepath.Join(cacheDir, "clones", "webapp-medium")
	if cloneDir != wantDir {
		t.Errorf("cloneDir = %q, want %q", cloneDir, wantDir)
	}
}

// ---- checkRunIDAvailable: same-(task,config) collision guard (integration gap #2) --

func TestCheckRunIDAvailable_ErrorsWhenRunDirExists(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)
	runID := "some-run-id"
	if err := os.MkdirAll(filepath.Join("eval", "runs", runID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkRunIDAvailable(runID); err == nil {
		t.Fatal("checkRunIDAvailable: want error when the run dir already exists, got nil")
	}
}

func TestCheckRunIDAvailable_OKWhenAbsent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := checkRunIDAvailable("fresh-run-id"); err != nil {
		t.Fatalf("checkRunIDAvailable: unexpected error: %v", err)
	}
}

func TestRun_DispatchErrorPropagates(t *testing.T) {
	repoDir := t.TempDir()
	stubGitClone(t, func(repo, dest string) error {
		t.Fatal("runGitClone should not be called for an already-local fixture dir")
		return nil
	})
	stubDispatch(t, func(args []string) (string, error) {
		return "", os.ErrPermission
	})
	t.Chdir(t.TempDir())

	task := TaskSpec{ID: "t1", FixtureRepo: repoDir, FixtureRef: "v1", Problem: "fix it"}
	cfg := ConfigFingerprint{DRIModel: "opus"}

	if _, err := Run(task, cfg); err == nil {
		t.Fatal("expected error when ateam dispatch fails, got nil")
	}
}
