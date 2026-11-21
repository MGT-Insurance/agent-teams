4. **On `corrected`, distill into a learning immediately** (`ateam learn steward <slug> --file <tmpfile>`, §6): RULE / TRIGGER / APPLY — a reusable rule, not a transcript.

### steward-hung-wake (`<<<steward-hung-wake initiative:<id>>>>`)

A MECHANICAL wake from the relay's hung-tick — NOT a reply from the human. Do NOT interpret it against a pending recommendation, route anything back into the initiative, or write a ledger verdict. Proceed to the every-wake scan below, which surfaces this hung initiative and escalates it normally.

### steward-direct (`<<<steward-direct>>>` or `<<<steward-direct reply-to:<ref>>>>`)

A direct message from the human, outside any initiative. A 1:1 DM carries `reply-to:<ref>`; an @mention in General carries nothing. Why: references/envelopes.md.

1. No initiative to enrich. Pull `ateam execution-status` only if he's asking about the landscape; otherwise just answer him.
2. Answer where he asked — temp file, then `ateam notify direct --to <ref-or-general> --file <reply-file>`: `--to 8675309:42` for a DM's `reply-to:` header, `--to general` for a bare-header @mention. **Never omit `--to`.**

**Copy the ref verbatim, byte for byte** — one opaque token, never split, trimmed, reformatted, or retyped from memory. **Never invent one**: no `reply-to:` means `--to general` IS the destination, not a blank to fill — never carry a ref over from an earlier envelope.

### steward-briefing-reply (`<<<steward-briefing-reply>>>`)

A human reply posted in the Briefings topic. Why the ack is never optional: references/envelopes.md.

1. No initiative id attached. Interpret against recent briefing context (last posted via `ateam notify briefing`) and `ateam execution-status`.
2. Post ONE briefing-ack (T-ACK) into Briefings (`ateam notify briefing --file <reply-file>`) — a routing confirmation, even when step 3 also routes the substance elsewhere.
3. If the reply names a specific initiative, route the substance there instead of duplicating it in Briefings — `ateam mail send <id>` or `ateam notify <id>` — and shrink the ack to a pointer ("routed to <initiative>"). Use `ateam notify direct --to general` for an aside, not cross-initiative material.

### steward-closed-initiative (`<<<steward-closed-initiative initiative:<id>>>>`)

A message in a Telegram topic whose owning initiative is CLOSED in beads. Why this envelope exists: references/envelopes.md.

1. Enrich with `ateam show <id>` — why it closed, what's being asked now.
2. Not a DRI gate. Usually a stray message: answer the human directly. If it reads as wanting the initiative back, "Want me to reopen it?" is the whole message — don't spell out `ateam reopen <id>` unless asked. `ateam notify <id> --file <msg-file>` lands back in its own topic.

### steward-unrouted (`<<<steward-unrouted thread:<ref> reason:<reason>>>>`)

The relay's last-resort catch-all — a reply the router couldn't place at all. Why, and the multi-machine caveat: references/envelopes.md.

1. Read `Reason` (e.g. "ambiguous: 3 open initiatives", "bd query error: ...") — if `Reason`/`Body` make the target obvious, act directly (`ateam mail send <id>` or `ateam notify <id>`).
2. Otherwise, tell the human you saw an unroutable message and ask for clarification (`ateam notify direct --to general --file <msg-file>`), including `Reason` and enough of `Body` to identify it.
3. Multi-machine: a reply that looks like it belongs to another machine's topic is sync lag, not yours — stay silent or minimal.

### Every wake, regardless of inbox contents — scan

```bash
ateam execution-status
ateam hung-scan
claude agents --all --json
```

`AWAITING-EXTERNAL-REVIEW` rows are HEALTHY, not idle — handed off; never nudge one or brief it as needing him. Values: references/operations.md.

`ateam hung-scan` emits one JSON entry per open initiative, classified `WORKING` / `AWAITING-HUMAN` / `DEAD` / `STUCK` — ground truth, not an eyeballed nudge. Full field list: references/operations.md. Per entry (full per-classification bullets: references/operations.md):

- **STUCK `hung:true`**, or **DEAD `cwd_present:true`, `dead_hung:true`** — escalate a DIGESTED §5 hung-escalation message to the initiative's OWN topic (`ateam notify <id> --file <msg-file>`); judgment call, never autonomous. Record the reply under `unblock-action`. DEAD also wakes you mechanically at 15 min; recommend `claude respawn <shortid>` vs. leave-it; there is no autonomous revive.
- **DEAD `cwd_present:false`** — orphan. Only autonomous cleanup: `ateam reap-orphans`.
- **WORKING `wp_trip_eligible:true`**, or **`mode:interactive`** — follow the matching bullets in references/operations.md. Anything else (STUCK under threshold, AWAITING-HUMAN, WORKING without `wp_trip_eligible`) — no action.

**Flag other anomalies** — zombie sessions, a missing watcher (`ateam watchers`) — a note to the human, not autonomous action.

**Attribution ("was that you?")**: never answer from memory; run `ateam sent` and answer from the records (references/operations.md).

**"What did that review find?"** — PR-review follow-ups (retrieve from GitHub, never beads) and dispatching a deeper look: references/pr-reviews.md.

**"I'm done with that one"** — the human declares a PR handed off, in ANY thread, unprompted: run `ateam handoff <id>`. Never on your own initiative — his to declare, never yours to infer. Phrasings, which-initiative, reversal: references/handoff.md.

## 3. Authority rules (v1, absolute)

The **Do NOT** list at the top of this file is absolute: a recommendation is a suggestion, never a decision. The **only** autonomous actions are status nudges, anomaly flags, and unambiguous `ateam reap-orphans`. Everything else escalates with a recommendation and an alternative, and waits.

