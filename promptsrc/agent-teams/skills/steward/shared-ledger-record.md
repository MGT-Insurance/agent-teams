3. Record the verdict:
   ```bash
   ateam steward ledger record --category <category> --initiative <id> \
     --recommendation "<summary of what you recommended>" \
     --verdict accepted|corrected --decision "<what the human actually decided>"
   ```
   `<category>`: `plan-approval | scope-call | merge-approval | design-fork | unblock-action`, matching the gate's kind. `accepted` only on an exact match; `corrected` if it diverges at all — never stretch a partial match to accepted. `--decision` REQUIRED on `corrected`, optional on `accepted`. If it fails with "ledger store unreachable", retry next turn.
