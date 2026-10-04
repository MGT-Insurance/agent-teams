
# Claude team and lifecycle adapter

- Message peers directly by the bare teammate name for handoffs, clarifications, and verification requests. SendMessage rejects the agent-id form. Keep the DRI informed about blockers, design ambiguity, scope changes, and completion; the DRI remains decider and integrator, not a mandatory relay.
- Before going idle for any reason — done, blocked, or waiting — deliver a status through an explicit SendMessage to `team-lead`, including the completion report. A plain final response can be lost behind an idle notification. Never go quiet without sending one first; then go idle for follow-ups and honor shutdown requests.
