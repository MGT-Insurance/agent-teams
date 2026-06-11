# Gate protocol — every human gate, exact sequence, never vary

A "gate" is any point where you need the human: clarifications, plan approval, scope changes, destructive/outward actions.

1. **Record the question** as a note on the initiative issue in the global workspace (batch all currently-pending questions into one note; include your recommended default per question):
   `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" note <initiative-id> --file=/tmp/gate-note.txt`
2. **Flag needs-human:** `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" label add <initiative-id> human`
   (Note: `bd human respond` and `bd human dismiss` are broken in bd 1.0.4 — the label-based approach is the verified path. `bd human list` still works to enumerate flagged issues; see the framework repo's docs/verifications.md.)
3. **Ask and park.** Interactive: ask directly (AskUserQuestion or plain text) and continue when answered. Backgrounded (`--bg`): end the turn with the question as plain text — the session parks; the human sees it on attach, or via /initiatives.
4. **While parked:** keep every workstream that does not depend on the answer moving. Parking the question never parks the team.
5. **On answer:** clear the flag using the verified sequence: record the resolution as a comment (`bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" comment <initiative-id> "RESPONSE: <answer>"` — write via `--file` if multi-line), then remove the label (`bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" label remove <initiative-id> human`). Note the resolution on the initiative, then proceed. (`bd human respond/dismiss` are broken in bd 1.0.4 — the label-remove workaround is the verified path; see the framework repo's docs/verifications.md.)

Why this must never vary: the flag is the only machine-wide signal that an initiative is waiting on a human. A gate raised any other way is invisible.
