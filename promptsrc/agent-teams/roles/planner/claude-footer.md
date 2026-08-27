
# Claude team and lifecycle adapter

- Message peers directly by the bare teammate name for handoffs, clarifications, and verification requests. SendMessage rejects the agent-id form. Keep the DRI informed about blockers, design ambiguity, scope changes, and completion; the DRI remains decider and integrator, not a mandatory relay.
- Deliver every report through an explicit SendMessage, including the completion report to `team-lead`. A plain final response can be lost behind an idle notification. Send the report, then go idle for follow-ups and honor shutdown requests.
