4a. **Plan gate with no page.** A plan-approval gate (its decision starts "Approve plan", or it plainly asks him to approve a plan) whose ask has no `https://` URL and no line starting "no plan page:": still run steps 1-4, then in the same turn send the DRI this text (`<ref>` is the initiative id) with `ateam mail send <initiative-id> --file <f> --sender steward`:

   > steward: note (not Eric's words): plan gate <ref> arrived without a plan-page URL. Publish the plan page and raise the gate again with the URL as the first line of --context-file. This does not answer the gate.

   The re-raised gate arrives as an ordinary steward-gate, URL included.
5. **`kind:live-test-review`**: read the N attachment lines after the header (`<photo|document><TAB><path>` — frozen, never guess: references/envelopes.md / `internal/verbs/steward_seams.go`). Fan out one send per attachment, then the body — never an album/`sendMediaGroup`:
   ```bash
   ateam notify <id> --image    <photo-path>    --file <basename-file>  # per photo
   ateam notify <id> --document <document-path> --file <basename-file>  # per document
   ateam notify <id> --file     <body-file>                             # body, last
   ```
   `--file` is required on every call and becomes that send's caption, so each attachment gets its own tiny caption file (just its basename) — not the proof body repeated N times. No enrichment, recommendation, or ledger entry. Testers and the DRI never call Telegram directly — you're the only relay. No-steward case: waits like any other gate (references/envelopes.md).

### steward-reply (`<<<steward-reply initiative:<id>>>>`)

The human replied in a topic.

1. Interpret against the pending recommendation — took it, took the alternative, or something else?
2. Act on the DRI:
   ```bash
   ateam mail send <initiative-id> --file <answer-file> --sender steward
   ```
   Unblocks the DRI. Clearing the gate is the DRI's own job — never call `ateam clear-gate`.
