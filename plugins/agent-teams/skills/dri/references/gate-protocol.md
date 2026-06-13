# Gate protocol — every human gate, exact sequence, never vary

A "gate" is any point where you need the human: clarifications, plan approval, scope changes, destructive/outward actions.

1. **Record the question AND flag needs-human** in one call — write the question to a temp file, then:
   `ateam gate <initiative-id> --file /tmp/gate-note.txt`
   (This notes the question and adds the `human` label atomically.)
   (Note: `bd human respond` and `bd human dismiss` are broken in bd 1.0.4 — the label-based approach is the verified path. `bd human list` / `ateam human-list` still works to enumerate flagged issues; see the framework repo's docs/verifications.md.)
2. **Ask and park.** Interactive: ask directly (AskUserQuestion or plain text) and continue when answered. Backgrounded (`--bg`): end the turn with the question as plain text — the session parks; the human sees it on attach, or via /initiatives.
3. **While parked:** keep every workstream that does not depend on the answer moving. Parking the question never parks the team.
4. **On answer:** clear the flag — write the response to a temp file if multi-line, then:
   `ateam clear-gate <initiative-id> --file /tmp/gate-response.txt`
   (Or without `--file` to just remove the label when no comment is needed.) Note the resolution on the initiative, then proceed. (`bd human respond/dismiss` are broken in bd 1.0.4 — the label-remove workaround is the verified path; see the framework repo's docs/verifications.md.)

Why this must never vary: the flag is the only machine-wide signal that an initiative is waiting on a human. A gate raised any other way is invisible.
