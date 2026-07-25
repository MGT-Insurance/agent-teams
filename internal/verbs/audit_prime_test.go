// audit_prime_test.go: core-path tests for the `ateam audit` assertion that
// `bd prime` against the global workspace stays small and memory-free
// (agent-teams-e81h.4).
//
// Every test here builds a THROWAWAY workspace under t.TempDir() and a fake
// BDRunner. Nothing in this file reads, writes, or resolves the real
// ~/.agent-teams — that workspace is shared live state.
package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// auditPrimeWorkspace creates a throwaway workspace directory containing a
// .beads subdirectory (so workspace.Initialized reports true) with PRIME.md
// installed — i.e. a correctly set-up workspace. Tests exercising a missing or
// empty PRIME.md mutate it afterwards via auditPrimeMDPath.
func auditPrimeWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(auditPrimeMDPath(home), []byte(auditPrimeSuppressed), 0o644); err != nil {
		t.Fatalf("write PRIME.md: %v", err)
	}
	return home
}

// auditPrimeMDPath is the installed-suppression-file path the audit asserts on.
func auditPrimeMDPath(home string) string {
	return filepath.Join(home, ".beads", "PRIME.md")
}

// auditPrimeBD returns a fakeBD whose `prime` call yields out. Any other
// subcommand (the audit verb's `list --all --json`) yields an empty array.
func auditPrimeBD(out string, calls *[]string) *fakeBD {
	return &fakeBD{
		runFn: func(args ...string) (string, error) {
			if calls != nil {
				*calls = append(*calls, strings.Join(args, " "))
			}
			return out, nil
		},
		runJSONFn: func(dst any, args ...string) error {
			if calls != nil {
				*calls = append(*calls, strings.Join(args, " "))
			}
			return nil
		},
	}
}

// auditPrimeSuppressed is what `bd prime` emits on beads v1.1.0 when
// .beads/PRIME.md is present: the file's contents and nothing else. Captured
// verbatim from a throwaway workspace (269 bytes).
const auditPrimeSuppressed = `# Beads (global agent-teams workspace)

This workspace holds ONLY initiative-tracking beads + role memories.
Reach it ONLY via the ` + "`ateam`" + ` CLI. Never ` + "`bd -C ~/.agent-teams`" + `.

Memories are NOT injected here: use ` + "`ateam learnings <role>`" + `.
`

// auditPrimeMemoryDump is the memory-store section bd appends when prime is
// NOT suppressed, sized past the budget so it trips (a) as well as (b).
var auditPrimeMemoryDump = "\n## Persistent Memories (473)\n\n" +
	strings.Repeat("- implementer:hot:some-slug — a memory body that nobody asked for.\n", 400)

func TestAuditPrime_PassesWhenSuppressed(t *testing.T) {
	home := auditPrimeWorkspace(t)
	ctx, stdout, stderr := makeCtx(auditPrimeBD(auditPrimeSuppressed, nil), home)

	if err := (&auditKong{}).Run(ctx); err != nil {
		t.Fatalf("audit with suppressed prime: err = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "audit: bd prime clean") {
		t.Errorf("stdout missing the prime-clean line:\n%s", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

// The regression this whole check exists for: PRIME.md is still present and
// still emitted, but beads re-appends the memory store after it (the shape
// upstream restored in GH#3941). The guard must go RED.
func TestAuditPrime_FailsWhenMemoriesReappendedDespitePrimeMD(t *testing.T) {
	home := auditPrimeWorkspace(t)
	out := auditPrimeSuppressed + auditPrimeMemoryDump
	ctx, stdout, stderr := makeCtx(auditPrimeBD(out, nil), home)

	err := (&auditKong{}).Run(ctx)
	if cli.ExitCode(err) != 1 {
		t.Fatalf("post-GH#3941 shape: exit code %d, want 1", cli.ExitCode(err))
	}
	var silent *cli.SilentError
	if !errors.As(err, &silent) {
		t.Errorf("post-GH#3941 shape: err = %T, want *cli.SilentError", err)
	}
	if strings.Contains(stdout.String(), "bd prime clean") {
		t.Errorf("failure path still printed the clean line:\n%s", stdout.String())
	}

	got := stderr.String()
	for _, want := range []string{
		"audit: FAILED",
		"no longer suppressed",
		"GH#3941",
		"PRIME.md",
		"prime.max-memories",
		home,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("failure output missing %q:\n%s", want, got)
		}
	}
	// The measured size must be in the message — a future reader needs to see
	// how far over budget prime actually is, not just that it failed.
	if !strings.Contains(got, "bd prime size: "+strconv.Itoa(len(out))+" bytes") {
		t.Errorf("failure output missing the measured size %d:\n%s", len(out), got)
	}
}

// PRIME.md deleted entirely: bd falls back to its own workflow preamble and
// appends every memory. Also RED.
func TestAuditPrime_FailsWhenPrimeMDMissing(t *testing.T) {
	home := auditPrimeWorkspace(t)
	if err := os.Remove(auditPrimeMDPath(home)); err != nil {
		t.Fatalf("remove PRIME.md: %v", err)
	}
	out := "# Beads Workflow Context\n\n> Run `bd prime` after compaction.\n" + auditPrimeMemoryDump
	ctx, _, stderr := makeCtx(auditPrimeBD(out, nil), home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("missing PRIME.md: want exit 1")
	}
	if !strings.Contains(stderr.String(), "PRIME.md") {
		t.Errorf("failure output should point at PRIME.md:\n%s", stderr.String())
	}
}

// THE fresh-machine case, and the reason the file-existence assertion exists at
// all: PRIME.md never got installed, but the memory store is still empty, so
// prime is small and carries no memory heading. The OUTPUT assertion cannot see
// this — only the existence assertion can. Must be RED.
func TestAuditPrime_FailsWhenPrimeMDMissingOnEmptyWorkspace(t *testing.T) {
	home := auditPrimeWorkspace(t)
	if err := os.Remove(auditPrimeMDPath(home)); err != nil {
		t.Fatalf("remove PRIME.md: %v", err)
	}
	// bd's own preamble on a workspace with zero memories: well under budget,
	// no memory heading. Exactly what the output assertion calls healthy.
	var calls []string
	out := "# Beads Workflow Context\n\n> Run `bd prime` after compaction.\n"
	if len(out) > primeBudgetBytes || strings.Contains(out, primeMemoriesHeading) {
		t.Fatalf("fixture must be the shape the output assertion passes, or this test proves nothing")
	}
	ctx, stdout, stderr := makeCtx(auditPrimeBD(out, &calls), home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("fresh machine with no PRIME.md: want exit 1 — the output assertion alone would have passed this")
	}
	got := stderr.String()
	for _, want := range []string{
		"audit: FAILED",
		"no installed PRIME.md",
		"(missing)",
		"ateam steward init",
		auditPrimeMDPath(home),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing-PRIME.md output missing %q:\n%s", want, got)
		}
	}
	// The absent-file case must NOT drag the reader through override-semantics
	// reasoning that has nothing to do with their problem.
	if strings.Contains(got, "GH#3941") {
		t.Errorf("absent PRIME.md should not print the override-semantics block:\n%s", got)
	}
	if strings.Contains(stdout.String(), "bd prime clean") {
		t.Errorf("missing PRIME.md still printed the clean line:\n%s", stdout.String())
	}
	// Nothing to learn from prime once the suppression file is gone.
	for _, c := range calls {
		if c == "prime" {
			t.Errorf("missing PRIME.md should short-circuit before running bd prime; got %v", calls)
		}
	}
}

// A zero-byte PRIME.md is a hole the output assertion can NEVER see: beads
// v1.1.0 honours it as a total override and emits nothing at all, so prime
// measures 0 bytes with no heading at any memory count.
func TestAuditPrime_FailsWhenPrimeMDEmpty(t *testing.T) {
	home := auditPrimeWorkspace(t)
	if err := os.WriteFile(auditPrimeMDPath(home), nil, 0o644); err != nil {
		t.Fatalf("truncate PRIME.md: %v", err)
	}
	ctx, _, stderr := makeCtx(auditPrimeBD("", nil), home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("empty PRIME.md: want exit 1")
	}
	if !strings.Contains(stderr.String(), "present but empty") {
		t.Errorf("empty PRIME.md should be named as such:\n%s", stderr.String())
	}
}

// The heading check must be load-bearing on its own: a memory dump small
// enough to fit the byte budget still fails.
func TestAuditPrime_FailsOnMemoriesHeadingUnderBudget(t *testing.T) {
	home := auditPrimeWorkspace(t)
	out := auditPrimeSuppressed + "\n## Persistent Memories (2)\n\n- one\n- two\n"
	if len(out) > primeBudgetBytes {
		t.Fatalf("fixture is %d bytes — must stay under the %d budget to isolate the heading check", len(out), primeBudgetBytes)
	}
	ctx, _, stderr := makeCtx(auditPrimeBD(out, nil), home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("memories heading under budget: want exit 1")
	}
	if !strings.Contains(stderr.String(), "memory dump:   PRESENT") {
		t.Errorf("failure output should report the dump as present:\n%s", stderr.String())
	}
}

// Byte budget must be load-bearing on its own too: no heading, but oversized.
func TestAuditPrime_FailsOnOversizedOutputWithoutHeading(t *testing.T) {
	home := auditPrimeWorkspace(t)
	out := strings.Repeat("x", primeBudgetBytes+1)
	ctx, _, stderr := makeCtx(auditPrimeBD(out, nil), home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("oversized prime: want exit 1")
	}
	if !strings.Contains(stderr.String(), "memory dump:   absent") {
		t.Errorf("failure output should report the dump as absent:\n%s", stderr.String())
	}
}

// Exactly at the budget passes; one byte over fails. Pins the boundary so a
// later refactor can't silently turn <= into <.
func TestAuditPrime_BudgetBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		wantOK bool
	}{
		{"at budget", primeBudgetBytes, true},
		{"one over", primeBudgetBytes + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := auditPrimeWorkspace(t)
			ctx, _, _ := makeCtx(auditPrimeBD(strings.Repeat("x", tc.size), nil), home)
			if got := checkGlobalPrimeBudget(ctx); got != tc.wantOK {
				t.Errorf("checkGlobalPrimeBudget at %d bytes = %v, want %v", tc.size, got, tc.wantOK)
			}
		})
	}
}

// ── no-op paths ───────────────────────────────────────────────────────────────
//
// This check runs on every DRI preflight machine-wide, so a false failure here
// breaks every DRI. Each of these calls checkGlobalPrimeBudget DIRECTLY: a
// green end-to-end audit would pass even if the check silently never ran.

func TestAuditPrime_NoOpWhenWorkspaceAbsent(t *testing.T) {
	var calls []string
	// A path that does not exist at all.
	home := filepath.Join(t.TempDir(), "no-such-workspace")
	ctx, stdout, stderr := makeCtx(auditPrimeBD("irrelevant"+auditPrimeMemoryDump, &calls), home)

	if !checkGlobalPrimeBudget(ctx) {
		t.Error("absent workspace: want silent pass")
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("absent workspace must be silent; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if len(calls) != 0 {
		t.Errorf("absent workspace must not shell out to bd; got %v", calls)
	}
}

func TestAuditPrime_NoOpWhenBeadsDirMissing(t *testing.T) {
	var calls []string
	home := t.TempDir() // exists, but has no .beads
	ctx, stdout, stderr := makeCtx(auditPrimeBD("irrelevant"+auditPrimeMemoryDump, &calls), home)

	if !checkGlobalPrimeBudget(ctx) {
		t.Error("uninitialized workspace: want silent pass")
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("uninitialized workspace must be silent; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if len(calls) != 0 {
		t.Errorf("uninitialized workspace must not shell out to bd; got %v", calls)
	}
}

func TestAuditPrime_NoOpWhenPrimeFails(t *testing.T) {
	home := auditPrimeWorkspace(t)
	bdRunner := &fakeBD{
		runFn: func(args ...string) (string, error) {
			return "", errors.New("bd prime: exit status 1")
		},
	}
	ctx, stdout, stderr := makeCtx(bdRunner, home)

	if !checkGlobalPrimeBudget(ctx) {
		t.Error("bd prime failure: want silent pass")
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Errorf("bd prime failure must be silent; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// The whole verb — not just the helper — must stay exit 0 and free of prime
// output when the workspace isn't there.
func TestAuditPrime_VerbNoOpWhenWorkspaceAbsent(t *testing.T) {
	home := filepath.Join(t.TempDir(), "no-such-workspace")
	ctx, stdout, stderr := makeCtx(auditPrimeBD("irrelevant"+auditPrimeMemoryDump, nil), home)

	if err := (&auditKong{}).Run(ctx); err != nil {
		t.Fatalf("audit with absent workspace: err = %v, want nil", err)
	}
	if strings.Contains(stdout.String(), "bd prime") {
		t.Errorf("absent workspace: stdout must carry no prime line:\n%s", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("absent workspace: unexpected stderr %q", stderr.String())
	}
}

// The prime check must not be swallowed by the pre-existing leaked-beads
// branch: a workspace with BOTH problems has to report both and still exit 1.
func TestAuditPrime_ReportedAlongsideLeakedBeads(t *testing.T) {
	home := auditPrimeWorkspace(t)
	bdRunner := &fakeBD{
		runFn: func(args ...string) (string, error) {
			return auditPrimeSuppressed + auditPrimeMemoryDump, nil
		},
		runJSONFn: func(dst any, args ...string) error {
			out, ok := dst.(*[]bd.Issue)
			if !ok {
				return errors.New("unexpected dst")
			}
			*out = []bd.Issue{{ID: "at-bad", Title: "Leaked Bead", Description: "no worktree line"}}
			return nil
		},
	}
	ctx, _, stderr := makeCtx(bdRunner, home)

	if cli.ExitCode((&auditKong{}).Run(ctx)) != 1 {
		t.Fatalf("both failures: want exit 1")
	}
	got := stderr.String()
	if !strings.Contains(got, "LEAKED work beads") {
		t.Errorf("stderr missing the leaked-beads report:\n%s", got)
	}
	if !strings.Contains(got, "GH#3941") {
		t.Errorf("stderr missing the prime report:\n%s", got)
	}
}
