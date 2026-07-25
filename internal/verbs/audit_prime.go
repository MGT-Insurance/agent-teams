// audit_prime.go: the `ateam audit` assertion that `bd prime`, resolved against
// the GLOBAL workspace, stays small and carries no memory dump. Called from
// auditKong.Run in match.go (agent-teams-e81h.4).
package verbs

import (
	"fmt"
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
	fmt.Fprintln(ctx.Stderr, "MOST LIKELY CAUSE — a beads upgrade changed PRIME.md override semantics.")
	fmt.Fprintln(ctx.Stderr, "On beads v1.1.0 a custom <workspace>/.beads/PRIME.md is a TOTAL override: bd")
	fmt.Fprintln(ctx.Stderr, "emits the file and appends nothing. Upstream reversed this (GH#3941) so that a")
	fmt.Fprintln(ctx.Stderr, "custom PRIME.md replaces only the workflow text and the memories are")
	fmt.Fprintln(ctx.Stderr, "re-appended. Upgrading to a release carrying that change un-fixes this")
	fmt.Fprintln(ctx.Stderr, "workspace silently.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "WHAT TO DO")
	fmt.Fprintln(ctx.Stderr, "  1. Check whether PRIME.md is simply missing or empty — the cheap cause. If it")
	fmt.Fprintln(ctx.Stderr, "     is, restore it and re-run `ateam audit`:")
	fmt.Fprintf(ctx.Stderr, "       ls -l %s\n", filepath.Join(ctx.Home, ".beads", "PRIME.md"))
	fmt.Fprintln(ctx.Stderr, "  2. If PRIME.md is present and prime STILL dumps memories, the GH#3941 reversal")
	fmt.Fprintln(ctx.Stderr, "     has landed: the PRIME.md lever is dead and a new suppression mechanism is")
	fmt.Fprintln(ctx.Stderr, "     needed. Do NOT assume the `prime.max-memories` config key covers this — it")
	fmt.Fprintln(ctx.Stderr, "     is an untested hedge against then-unreleased beads and fails silently.")
	fmt.Fprintln(ctx.Stderr, "  3. Reproduce with:")
	fmt.Fprintf(ctx.Stderr, "       bd -C %s prime | head -20\n", ctx.Home)
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "Do NOT resolve this by deleting memories or by raising the budget in")
	fmt.Fprintln(ctx.Stderr, "internal/verbs/audit_prime.go — either one throws away the only witness.")

	return false
}
