// audit_prime.go: the `ateam audit` assertion that `bd prime`, resolved against
// the GLOBAL workspace, stays small and carries no memory dump. Called from
// auditKong.Run in match.go (agent-teams-e81h.4).
package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// primeBudgetBytes caps `bd prime` output resolved against the global workspace.
//
// Calibration (bd v1.1.0): a workspace suppressed by .beads/PRIME.md emits only
// that file — a few hundred bytes. An unsuppressed one emits bd's own ~4.7 KB
// workflow preamble PLUS every memory in the store; the real global workspace
// measured 392,864 bytes across 473 memories, 98.5% of it memories. 10 KB sits
// an order of magnitude above the suppressed case and nearly two orders below
// the failure case, so it discriminates without being brittle.
const primeBudgetBytes = 10 * 1024

// primeMemoriesHeading is what bd prints immediately before dumping the whole
// memory store ("## Persistent Memories (473)"). Matched as a substring so the
// count in parentheses is irrelevant.
const primeMemoriesHeading = "## Persistent Memories"

// checkGlobalPrimeBudget asserts that `bd prime`, resolved against the global
// workspace, stays under primeBudgetBytes and emits no memory dump. It reports
// whether the assertion held.
//
// WHY this is a standing assertion and not just a fix: the suppression lever is
// <home>/.beads/PRIME.md, and on the installed beads (v1.1.0) a custom PRIME.md
// is a TOTAL override — bd emits the file and appends nothing. Upstream beads
// deliberately reversed that (GH#3941): a custom PRIME.md replaces only the
// workflow text and the memories are re-appended afterwards. The first beads
// release carrying that change silently un-fixes this workspace — no error, no
// warning, the whole memory store simply returns to every session's context on
// every PreCompact. Nothing else in the system can witness that regression.
//
// Fail-soft by construction: this runs on every DRI preflight on the machine,
// so a false failure here would break every DRI. Anything short of a confirmed
// oversized-or-memory-bearing prime — no workspace, unreadable workspace, bd
// prime failing for any reason at all — is a silent no-op: no output, no
// side effects, exit 0.
func checkGlobalPrimeBudget(ctx *cli.Context) bool {
	if !workspace.Initialized(ctx.Home) {
		return true
	}
	if !checkPrimeMDInstalled(ctx) {
		// The suppression file itself is gone. Reporting prime's size on top of
		// that would only bury the one actionable fact.
		return false
	}

	out, err := ctx.BD.Run("prime")
	if err != nil {
		return true
	}

	size := len(out)
	hasMemories := strings.Contains(out, primeMemoriesHeading)
	if size <= primeBudgetBytes && !hasMemories {
		fmt.Fprintf(ctx.Stdout, "audit: bd prime clean — %d bytes, no memory dump (budget %d)\n", size, primeBudgetBytes)
		return true
	}

	dump := "absent"
	if hasMemories {
		dump = "PRESENT"
	}
	fmt.Fprintln(ctx.Stderr, "audit: FAILED — `bd prime` against the global workspace is no longer suppressed:")
	fmt.Fprintf(ctx.Stderr, "  workspace:     %s\n", ctx.Home)
	fmt.Fprintf(ctx.Stderr, "  bd prime size: %d bytes (budget %d)\n", size, primeBudgetBytes)
	fmt.Fprintf(ctx.Stderr, "  memory dump:   %s (%q)\n", dump, primeMemoriesHeading)
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "`bd prime` output is injected verbatim into every session that resolves this")
	fmt.Fprintln(ctx.Stderr, "workspace, on every PreCompact. Unsuppressed it carries the ENTIRE all-role")
	fmt.Fprintln(ctx.Stderr, "memory store into contexts that never asked for it — when this was first")
	fmt.Fprintln(ctx.Stderr, "measured, memories were 98.5% of the output.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "CAUSE — a beads upgrade changed PRIME.md override semantics (GH#3941).")
	fmt.Fprintln(ctx.Stderr, ".beads/PRIME.md is present and non-empty — checked before this — so the cheap")
	fmt.Fprintln(ctx.Stderr, "explanation is already ruled out. On beads v1.1.0 a custom PRIME.md is a TOTAL")
	fmt.Fprintln(ctx.Stderr, "override: bd emits the file and appends nothing. Upstream reversed that, so a")
	fmt.Fprintln(ctx.Stderr, "custom PRIME.md replaces only the workflow text and the memories are re-appended")
	fmt.Fprintln(ctx.Stderr, "after it. Upgrading to a release carrying that change un-fixes this workspace")
	fmt.Fprintln(ctx.Stderr, "silently, which is what has happened here.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "WHAT TO DO — the PRIME.md lever is dead; a new suppression mechanism is needed.")
	fmt.Fprintln(ctx.Stderr, "Do NOT assume the `prime.max-memories` config key covers this: it is an untested")
	fmt.Fprintln(ctx.Stderr, "hedge against then-unreleased beads and fails silently in every direction.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "  Reproduce with:")
	fmt.Fprintf(ctx.Stderr, "    bd -C %s prime | head -20\n", ctx.Home)
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "Do NOT resolve this by deleting memories or by raising the budget in")
	fmt.Fprintln(ctx.Stderr, "internal/verbs/audit_prime.go — either one throws away the only witness.")

	return false
}

// checkPrimeMDInstalled asserts that <home>/.beads/PRIME.md exists and is
// non-empty, reporting whether it does.
//
// WHY this is asserted separately, when the output check above would seem to
// subsume it: the output check can only see the SYMPTOM, and the symptom lags
// the breakage by weeks. A freshly set-up machine whose PRIME.md install
// silently failed has an empty memory store, so its prime is small and
// memory-free and the output check stays GREEN — through exactly the window in
// which the fix is one command. It only trips once enough memories have
// accumulated to cross the budget, by which point the machine has been leaking
// the store into every session for a long time.
//
// An empty PRIME.md is worse still: beads v1.1.0 honours a zero-byte file as a
// total override and emits nothing at all, so the output check can NEVER see
// that case, at any memory count.
func checkPrimeMDInstalled(ctx *cli.Context) bool {
	path := filepath.Join(ctx.Home, ".beads", "PRIME.md")
	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		return true
	}

	reason := "missing"
	if err == nil {
		reason = "present but empty"
	} else if !os.IsNotExist(err) {
		reason = "unreadable: " + err.Error()
	}

	fmt.Fprintln(ctx.Stderr, "audit: FAILED — the global workspace has no installed PRIME.md:")
	fmt.Fprintf(ctx.Stderr, "  %s (%s)\n", path, reason)
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "That file is what stops `bd prime` from appending the ENTIRE all-role memory")
	fmt.Fprintln(ctx.Stderr, "store to every session that resolves this workspace, on every PreCompact.")
	fmt.Fprintln(ctx.Stderr, "This is reported now, while prime is still small enough that nothing looks")
	fmt.Fprintln(ctx.Stderr, "wrong, because by the time the dump is measurably oversized the machine has")
	fmt.Fprintln(ctx.Stderr, "been leaking context for weeks.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "FIX: `ateam steward init` — idempotent, installs the template.")

	return false
}
