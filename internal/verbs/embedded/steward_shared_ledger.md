Recall prior calls at decision time, never from startup: `ateam steward ledger recall <category>` and `ateam recall steward <keywords>`.

4. Nothing to the ledger yet — pending until the human replies. Keep notes on the recommendation; the reply handler depends on them.

3. Record the verdict:
   ```bash
   ateam steward ledger record --category <category> --initiative <id> \
     --recommendation "<summary of what you recommended>" \
     --verdict accepted|corrected --decision "<what the human actually decided>"
   ```
   `<category>`: `plan-approval | scope-call | merge-approval | design-fork | unblock-action`, matching the gate's kind. `accepted` only on an exact match; `corrected` if it diverges at all — never stretch a partial match to accepted. `--decision` REQUIRED on `corrected`, optional on `accepted`. If it fails with "ledger store unreachable", retry next turn.

## 4. Ledger discipline

One record per escalated decision, written at verdict time — when the human's reply comes back, never at recommendation time. Categories, verdict rules, and the command: §2's steward-reply handler. Nothing else reaches the ledger: a direct chat, a briefing reply, a closed-initiative message, and an unrouted message are not gated decisions.

- No confidence graduation: the ledger grants no autonomous authority — escalate every gate to the human regardless of ledger stats.
