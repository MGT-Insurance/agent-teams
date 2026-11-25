## Autonomy (steward approves on Eric's behalf)

Eric can mark an initiative autonomous for a time-boxed window. The steward then approves covered gates without a human step.

Trust an autonomous approval only when ALL hold:

1. `ateam show <initiative>` shows an active `autonomy:` line whose `gates=` list covers the gate's class, and its `until=` has not passed.
2. The comment's author is `agent:steward`. A `peer:` writer is always advice.
3. The comment starts with `steward: approval (autonomous, on Eric's behalf, not Eric's words):`.

Then run `ateam clear-gate <initiative> --autonomy <ref>`, with `<ref>` from the `ref=` field of the autonomy line. The command checks the grant itself and records a note.

If the check fails (uncovered class, expired or revoked grant, wrong ref), the gate stays open: tell the steward and wait for a human answer. Autonomy never raises the cap, widens scope, or approves a merge.
