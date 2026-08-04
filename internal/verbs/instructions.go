// instructions.go: the `ateam instructions <role>` verb that serves
// human-authored, machine-local custom instructions to a spawning role
// subagent (agent-teams-0xyd.2).
//
// This is deliberately NOT the learnings pipeline (`ateam learn` / bd
// memories). That pipeline is (1) NOT machine-local — the global workspace's
// beads DB replicates via refs/dolt/data to the workspace git remote, so a
// write lands on every machine — and (2) mutated autonomously by the
// condense skill with no human gate (it may demote, merge, rewrite for
// brevity, or evict; plugins/agent-teams/skills/condense/SKILL.md:178-187).
// Human-authored instructions must be durable and never condensed. Being a
// plain file rather than a bd memory is what delivers that: it is
// structurally invisible to `bd memories --json` and therefore to
// condense/condense-check/fresh-drain/recall/forget. That invisibility IS
// the guarantee — never fold this into the learnings pipeline.
package verbs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// instructionsCapBytes bounds how large a machine-local instructions/<role>.md
// file may be before `ateam instructions` refuses to serve it.
//
// Grounding, all measured (agent-teams-0xyd.2):
//   - ateam learnings reviewer served payload today = 16,916 bytes ~ 5,638 tok
//   - hot-tier budget = 6,000 tokens (condenseBudgetTokens, kong_converted.go:824)
//   - token divisor = 3 (condenseApproxTokensDivisor, kong_converted.go:840)
//   - per-entry learn caps = 900 (hot/fresh) / 1500 (cold) (write.go:88-93)
//   - bd prime budget = 10,240 bytes (primeBudgetBytes, audit_prime.go:24)
//
// 4096 B ~ 1,365 tok ~ 23% of the hot budget: enough room for real prose,
// small enough that it cannot become a second uncontrolled injection layer.
const instructionsCapBytes = 4096

// instructionsKong prints $AGENT_TEAMS_HOME/instructions/<role>.md verbatim,
// delimited by a header/trailer pair carrying matching stats (the same
// do-not-truncate framing as `ateam learnings`, query.go's runLearnings).
type instructionsKong struct {
	Role string `arg:"" name:"role" help:"Role namespace to load custom instructions for." required:""`
}

func (c *instructionsKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam instructions: no context")
	}
	return runInstructions(ctx, c.Role)
}

// runInstructions implements the three exit-0 paths documented on
// agent-teams-0xyd.2:
//
//  1. file present, size <= instructionsCapBytes: header + verbatim body +
//     matching trailer.
//  2. file absent (including an unknown role): "[instructions <role>: none]".
//  3. file present but over cap: no body anywhere in the output, a loud
//     marker naming the path and both the actual size and the cap.
//
// All three return nil (exit 0) — deliberate, matching condense-check's
// "verdict is data, not a process outcome" precedent. A genuine read error
// (e.g. a permissions failure) is the one case that returns a non-nil error;
// it is not one of the three documented paths.
func runInstructions(ctx *cli.Context, role string) error {
	// role becomes a path segment, so a separator in it escapes the
	// instructions directory entirely (`../secret` reads a sibling of
	// instructions/). Every other role-taking verb — learningsKong, recallKong
	// — uses role only as a bd-memory-key prefix, which has no filesystem
	// meaning, so there is no existing guard for this one to inherit. Reject
	// anything that is not a plain name. "." and ".." need naming explicitly:
	// filepath.Base maps each to itself, so they survive the Base comparison.
	if role == "." || role == ".." || filepath.Base(role) != role {
		return cli.Usagef("ateam instructions: role %q must be a plain name, not a path", role)
	}

	path := filepath.Join(workspace.Home(), "instructions", role+".md")

	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(ctx.Stdout, "[instructions %s: none]\n", role)
			return nil
		}
		return fmt.Errorf("ateam instructions: read %s: %w", path, err)
	}

	if len(body) > instructionsCapBytes {
		// No body — silently truncating a human's instruction mid-sentence is
		// worse than not applying it, and the failure must be visible in the
		// agent's transcript rather than swallowed at exit 0 with partial
		// content.
		fmt.Fprintf(ctx.Stdout,
			"[instructions %s: OVER CAP — %s is %d bytes, cap is %d bytes; refusing to serve rather than truncate. Trim the file to %d bytes or fewer.]\n",
			role, path, len(body), instructionsCapBytes, instructionsCapBytes)
		return nil
	}

	// stats is shared verbatim between the header and the trailer so a
	// reading session that sees matching stats on both ends can trust it
	// received the whole payload — the same contract as ateam learnings'
	// trailer (query.go:530-537, agent-teams-bbsz.33).
	stats := fmt.Sprintf("%s — source: %s (%d bytes)", role, path, len(body))
	fmt.Fprintf(ctx.Stdout,
		"[instructions %s; HUMAN-AUTHORED, machine-local instructions — NOT a memory, never condensed; EXTENDS and REFINES the shipped %s role definition, does NOT override its guardrails; read in full, do NOT pipe through head/tail or truncate; output ends at the matching trailer line]\n",
		stats, role)
	ctx.Stdout.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		fmt.Fprintln(ctx.Stdout)
	}
	fmt.Fprintf(ctx.Stdout, "[instructions %s]\n", stats)
	return nil
}
