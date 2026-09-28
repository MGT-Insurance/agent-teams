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
// the failure case, so it discriminates without being brittle. Unchanged by
// the prime.max-memories/prime.max-memory-chars capping mechanism below: a
// correctly capped prime (measured 3,033,776 -> 2,234 bytes on the real
// workspace) stays comfortably under it too.
const primeBudgetBytes = 10 * 1024

// primeMemoriesHeading is what bd prints immediately before the memory
// section ("## Persistent Memories (showing 1 of 473, alphabetical)").
// Matched as a substring so the exact counts are irrelevant.
const primeMemoriesHeading = "## Persistent Memories"

// checkGlobalPrimeBudget asserts that `bd prime`, resolved against the global
// workspace, stays under primeBudgetBytes, and that any memory section it
// emits is confirmed capped. It reports whether the assertion held.
//
// WHY this is a standing assertion and not just a fix: on bd v1.1.0 a custom
// PRIME.md was a TOTAL override of `bd prime`'s output, so installing one was
// the whole fix. Upstream beads reversed that (GH#3941): on bd v1.3.0+ a
// custom PRIME.md replaces only the workflow text, and every persistent
// memory is re-appended after it, unbounded — `--no-memories` would suppress
// that section outright, but has no config-key fallback, so a caller that
// can't pass flags (the beads plugin's flag-less SessionStart/PreCompact
// `bd prime` hooks) can't reach it. The reachable lever is the
// prime.max-memories/prime.max-memory-chars config keys (installPrimeMemoryCaps
// in steward.go sets both to 1): bd always emits at least one memory plus an
// elision banner, so a capped prime never gets fully silent, but it holds the
// section to roughly one memory's worth instead of the whole store. A beads
// upgrade that changes this mechanism again — or a workspace where
// `ateam steward init` never ran — silently un-caps it: no error, no warning,
// the whole memory store simply returns to every session's context on every
// PreCompact. Nothing else in the system can witness that regression.
//
// Fail-soft by construction: this runs on every DRI preflight on the machine,
// so a false failure here would break every DRI. Anything short of a confirmed
// oversized-or-uncapped prime — no workspace, unreadable workspace, bd prime
// or bd config get failing for any reason at all — is a silent no-op: no
// output, no side effects, exit 0.
func checkGlobalPrimeBudget(ctx *cli.Context) bool {
	if !workspace.Initialized(ctx.Home) {
		return true
	}
	if !checkPrimeMDInstalled(ctx) {
		// The workflow-text override itself is gone. Reporting prime's size on
		// top of that would only bury the one actionable fact.
		return false
	}

	out, err := ctx.BD.Run("prime")
	if err != nil {
		return true
	}
	size := len(out)
	hasMemories := strings.Contains(out, primeMemoriesHeading)

	// The caps only matter when there's a memory section to cap — an empty
	// memory store emits no heading regardless of what the keys are set to,
	// and checking them would just manufacture a false positive.
	var maxMemories, maxChars string
	capsOK := true
	if hasMemories {
		var errMemories, errChars error
		maxMemories, errMemories = ctx.BD.Run("config", "get", primeMaxMemoriesKey)
		maxChars, errChars = ctx.BD.Run("config", "get", primeMaxMemoryCharsKey)
		if errMemories != nil || errChars != nil {
			return true
		}
		capsOK = maxMemories == primeMemoryCapValue && maxChars == primeMemoryCapValue
	}

	if size <= primeBudgetBytes && capsOK {
		if hasMemories {
			fmt.Fprintf(ctx.Stdout, "audit: bd prime clean — %d bytes, memories capped (%s=%s, %s=%s) (budget %d)\n",
				size, primeMaxMemoriesKey, maxMemories, primeMaxMemoryCharsKey, maxChars, primeBudgetBytes)
		} else {
			fmt.Fprintf(ctx.Stdout, "audit: bd prime clean — %d bytes, no memories in store (budget %d)\n", size, primeBudgetBytes)
		}
		return true
	}

	reportPrimeBudgetFailure(ctx, size, hasMemories, capsOK, maxMemories, maxChars)
	return false
}

// reportPrimeBudgetFailure writes the FAILED report for checkGlobalPrimeBudget.
// size/hasMemories/capsOK/maxMemories/maxChars are exactly what
// checkGlobalPrimeBudget already computed — this only formats them.
func reportPrimeBudgetFailure(ctx *cli.Context, size int, hasMemories, capsOK bool, maxMemories, maxChars string) {
	fmt.Fprintln(ctx.Stderr, "audit: FAILED — `bd prime` against the global workspace is not safely capped:")
	fmt.Fprintf(ctx.Stderr, "  workspace:     %s\n", ctx.Home)

	budgetVerdict := "ok"
	if size > primeBudgetBytes {
		budgetVerdict = "OVER"
	}
	fmt.Fprintf(ctx.Stderr, "  bd prime size: %d bytes (budget %d, %s)\n", size, primeBudgetBytes, budgetVerdict)

	switch {
	case !hasMemories:
		fmt.Fprintln(ctx.Stderr, "  memory caps:   n/a (no memories in store)")
	case capsOK:
		fmt.Fprintf(ctx.Stderr, "  memory caps:   confirmed (%s=%s, %s=%s)\n", primeMaxMemoriesKey, maxMemories, primeMaxMemoryCharsKey, maxChars)
	default:
		fmt.Fprintf(ctx.Stderr, "  memory caps:   NOT confirmed (%s=%q, %s=%q, want %q both)\n",
			primeMaxMemoriesKey, maxMemories, primeMaxMemoryCharsKey, maxChars, primeMemoryCapValue)
	}
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "`bd prime` output is injected verbatim into every session that resolves this")
	fmt.Fprintln(ctx.Stderr, "workspace, on every PreCompact. Uncapped, it carries the ENTIRE all-role memory")
	fmt.Fprintln(ctx.Stderr, "store into contexts that never asked for it — when this was first measured,")
	fmt.Fprintln(ctx.Stderr, "memories were 98.5% of the output.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "CAUSE — a beads upgrade changed PRIME.md override semantics (GH#3941).")
	fmt.Fprintln(ctx.Stderr, ".beads/PRIME.md is present and non-empty — checked before this — so the cheap")
	fmt.Fprintln(ctx.Stderr, "explanation is already ruled out. On bd v1.1.0 a custom PRIME.md was a TOTAL")
	fmt.Fprintln(ctx.Stderr, "override: bd emitted the file and appended nothing. Upstream reversed that, so")
	fmt.Fprintln(ctx.Stderr, "on bd v1.3.0+ a custom PRIME.md replaces only the workflow text and every memory")
	fmt.Fprintln(ctx.Stderr, "is re-appended after it, unbounded — and `--no-memories` has no config-key")
	fmt.Fprintln(ctx.Stderr, "fallback, so a hook that always calls flag-less `bd prime` can't reach it either.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "WHAT TO DO — confirm prime.max-memories/prime.max-memory-chars are set:")
	fmt.Fprintf(ctx.Stderr, "  bd -C %s config get %s\n", ctx.Home, primeMaxMemoriesKey)
	fmt.Fprintf(ctx.Stderr, "  bd -C %s config get %s\n", ctx.Home, primeMaxMemoryCharsKey)
	fmt.Fprintln(ctx.Stderr, "Run `ateam steward init` to set both idempotently (installPrimeMemoryCaps in")
	fmt.Fprintln(ctx.Stderr, "internal/verbs/steward.go) — it's safe to re-run and is the fix for both a")
	fmt.Fprintln(ctx.Stderr, "missing/wrong key and a workspace that never had `steward init` run on it.")
	fmt.Fprintln(ctx.Stderr, "If the keys are already confirmed and prime is STILL over budget, the caps")
	fmt.Fprintln(ctx.Stderr, "mechanism itself has changed again upstream — bd always emits at least one full")
	fmt.Fprintln(ctx.Stderr, "memory regardless of the char cap, so a single oversized memory can still blow")
	fmt.Fprintln(ctx.Stderr, "the budget; that needs a different fix (e.g. `bd forget` on that one entry), not")
	fmt.Fprintln(ctx.Stderr, "a change to this check.")
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "  Reproduce with:")
	fmt.Fprintf(ctx.Stderr, "    bd -C %s prime | head -20\n", ctx.Home)
	fmt.Fprintln(ctx.Stderr, "")
	fmt.Fprintln(ctx.Stderr, "Do NOT resolve this by deleting memories or by raising the budget in")
	fmt.Fprintln(ctx.Stderr, "internal/verbs/audit_prime.go — either one throws away the only witness.")
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
// An empty PRIME.md is a hole the output check can't see either, though for a
// different reason on bd v1.3.0+ than it used to be: a zero-byte file is no
// longer a total override (verified empirically — bd still appends the
// memory section after it, just with no workflow-text preamble), so the
// output check would only catch this once the memory dump crosses the
// budget on its own, exactly the lag this function exists to avoid.
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
