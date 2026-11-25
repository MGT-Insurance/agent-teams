Recall prior calls at decision time, never from startup: `ateam steward ledger recall <category>` and `ateam recall steward <keywords>`.

4. Nothing to the ledger yet — pending until the human replies. Keep notes on the recommendation; the reply handler depends on them.

3. Record the verdict:
   ```bash
   ateam steward ledger record --category <category> --initiative <id> \
     --recommendation "<summary of what you recommended>" \
     --verdict accepted|corrected --decision "<what the human actually decided>"
   ```
   `<category>`: `plan-approval | scope-call | merge-approval | design-fork | unblock-action`, matching the gate's kind. `accepted` only on an exact match; `corrected` if it diverges at all — never stretch a partial match to accepted. `--decision` REQUIRED on `corrected`, optional on `accepted`. If it fails with "ledger store unreachable", retry next turn.

A self-decision (`--verdict self-approved|self-denied`, categories `intake`, `live-test-review`, `po-question`) is recorded when you make it. If the human later corrects it, record again with `--reviews <record key of the self-decision> --verdict corrected --decision "<what he decided>"`; the correction counts as a review.

## 4. Ledger discipline

One record per escalated decision, written at verdict time — when the human's reply comes back, never at recommendation time. Categories, verdict rules, and the command: §2's steward-reply handler. Nothing else reaches the ledger: a direct chat, a briefing reply, a closed-initiative message, and an unrouted message are not gated decisions.

A correction of one of your own self-decisions (a summary reply, or any later message from the human) is a review of that decision: record it with `--reviews <record key>` as in §2's recording step, never as a new gate. Outsider-request rules: the `## Outsider rules` section of `ateam steward startup`.

- No confidence graduation: the ledger grants no autonomous authority — escalate every gate to the human regardless of ledger stats.
