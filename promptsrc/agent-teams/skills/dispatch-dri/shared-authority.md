
## Dispatcher authority

Use this handoff when a human wants an initiative to proceed without occupying the current session, or when separable work deserves its own DRI, checkout, team, and PR instead of expanding another initiative. Do not absorb that separable work into the current initiative.

**THIS SESSION IS A HANDOFF, NOT AN INVESTIGATION.**

**ABSOLUTE CONSTRAINT — NEVER investigate.** Do not explore or read the codebase, grep files, research mechanisms, analyze architecture, design a solution, or answer open questions on the human's behalf. A vague statement stays vague. The background DRI must investigate independently; dispatcher analysis contaminates the handoff with assumptions.

**ABSOLUTE CONSTRAINT — ALWAYS launch a DRI.** Every invocation must end in a background DRI launch. Do not refuse, decide that the work is too small, judge that the scope is unclear, or ask whether the human wants to proceed. There are only two legitimate pre-dispatch stops:

1. `ateam` or the selected runtime is unavailable. Direct the human to the selected runtime's agent-teams setup skill and stop.
2. No problem statement was provided. Ask only for that statement, then dispatch immediately after receiving it.

Once the required inputs exist, launch unconditionally. Your complete job is preflight, capture the human's framing verbatim, dispatch, and hand off the output.

**CARDINAL Beads boundary.** The global agent-teams workspace holds only initiative-tracking beads and role memory. Initiative registration is the one global write, performed only by `ateam dispatch` through its sanctioned register path—never by raw `bd -C`. All contract, feature, task, test, and discovery beads belong to the PROJECT repository and are created later by the DRI and its team.
