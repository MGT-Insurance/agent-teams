---
name: preflight
description: Verify an agent-teams install by spawning one real teammate and witnessing what it actually got. Use when asked to "run preflight", "check agent-teams is working", "verify the install", or when invoked as /agent-teams:preflight. This is the primary check — directly invocable inside an already-dispatched session as a first-class standalone mode, not a fallback. `ateam preflight` is a launcher on top of this skill for use OUTSIDE a dispatched session; it does not replace it.
---

You verify that role teammates spawned from this session actually work, by spawning one real `agent-teams-implementer`, confirming it responds, asking it to echo back a one-time token the launching verb planted in its own prompt, and shutting it down. `ateam preflight` (a separate Go verb) exists to launch this skill from a session that has no role agents of its own yet, and it is also the process that mints the token and makes the final PASS/FAIL judgement on it — this skill asks the question and reports what came back, but never learns the correct answer (see Step 3).

Contract: `agent-teams-25s3.2` (frozen) defines the status vocabulary, the check-result and top-level JSON shapes, and exit-code handling. This skill implements the JSON side of that contract; it does not restate the reasoning behind it.

**OUTPUT DISCIPLINE — read this before Step 1.** You print exactly ONE JSON object in this entire run: the top-level envelope (contract shape (4)), as the bare, final assistant message — no prose before or after it, no code fence around it, nothing printed earlier in the run. Every individual check object (contract shape (3)) that this document shows you is a VALUE YOU ASSEMBLE INTERNALLY and place into that envelope's `checks` array — it is never something you print on its own, at any point, even momentarily. Where this document shows a check object in a fenced ` ```json ` block, that fence is for THIS document's readability only; it does not mean "print this." The only fenced blocks below that represent an actual final message are labelled "entire final message" — everything else is a value to hold and assemble.

## Checks this skill owns

| check id | witnesses |
|---|---|
| `role-types-available` | `agent-teams-implementer` is a spawnable agent type in this session |
| `teammate-spawns` | the Agent tool call actually returns a response from the probe |
| `role-prose-in-context` | the probe echoes back a one-time token the launching verb planted in its assembled prompt — proof the prompt it actually received carried the role's instructions, not just that some agent replied; see Step 3 |
| `learnings-loaded` | the probe runs `ateam learnings implementer` in its own session and reports the result; the verb cross-checks the entry count against the store's own, so it witnesses a real in-session fetch rather than the probe's word; see Step 3b |

## Step 1 — role types available

Check the "Available agent types for the Agent tool" listing already present in this session's context for `agent-teams-implementer`.

**If present:** continue to Step 2. Nothing is printed yet.

**If absent:** this session has no agent-teams role agents at all — it was not launched by `ateam dispatch`/`ateam resume` (or by `ateam preflight`, which launches one for exactly this reason). That is a normal, expected way to invoke `/agent-teams:preflight` directly, not a broken run — report it accurately and stop. Do not spawn anything, and do not go to Step 2. Build one check object:

`{"check":"role-types-available","status":"FAIL","detail":"agent-teams-implementer is not in this session's available agent types","witness":"available agent types listing in session context","remediation":"run \"ateam preflight\" to launch a dispatched probe session, or invoke this skill from inside a session already launched by \"ateam dispatch\"/\"ateam resume\""}`

Embed it as the sole element of the envelope and end your turn on this — the entire final message, bare, no fence, nothing before or after it:

```json
{"checks":[{"check":"role-types-available","status":"FAIL","detail":"agent-teams-implementer is not in this session's available agent types","witness":"available agent types listing in session context","remediation":"run \"ateam preflight\" to launch a dispatched probe session, or invoke this skill from inside a session already launched by \"ateam dispatch\"/\"ateam resume\""}],"pass":0,"fail":1,"skip":0,"session_id":"<$CLAUDE_CODE_SESSION_ID>"}
```

## Step 2 — spawn the probe teammate

`role-types-available` passed. Hold this check object for Step 5's envelope — do not print it: `{"check":"role-types-available","status":"PASS","detail":"agent-teams-implementer is in this session's available agent types","witness":"available agent types listing in session context","remediation":""}`.

Spawn exactly one teammate via the Agent tool:

- `subagent_type: "agent-teams-implementer"` — the hyphen key. Never the colon form; it no longer exists.
- `name: "preflight-probe"`
- `mode: "bypassPermissions"`
- **No `model` argument.** This is load-bearing (agent-teams-25s3.2 amendment): the sidecar's resolved model is only meaningful evidence on the Go side of this initiative when the caller passed none.

This spawn exists to exercise the spawn mechanism itself — its purpose is the sidecar record it produces (read by the GO track's `spawn-record-present` and `role-type-registered` checks — `role-model-attached` and `spawn-permission-mode` were retired, agent-teams-25s3.24: this launch mode's sidecar never carries a model or permissionMode field to check), not any answer the probe gives. The prompt should not name the role or quote role prose; it only needs to elicit a response. For example:

> This is a one-shot preflight connectivity check, unrelated to any task. Reply with a brief acknowledgement, then take no further action.

Hold the outcome for Step 5 — nothing is printed here either way:

- If the Agent call errors, hangs with no response, or otherwise produces nothing, hold `{"check":"teammate-spawns","status":"FAIL","detail":"<what happened — error text or \"no response\">","witness":"live probe","remediation":"confirm claude is on PATH and the roles directory resolves; retry the spawn"}`.
- If it responds at all — with any content — hold `{"check":"teammate-spawns","status":"PASS","detail":"probe returned a response","witness":"live probe","remediation":""}`.

This check is the WEAKER witness of "a teammate spawned." The authority is `spawn-record-present` (agent-teams-25s3.3) — the harness-written sidecar, verb-side — and if the two ever disagree, the sidecar wins.

Either way, continue to Step 3. If the spawn FAILed and there is no probe to ask, Step 3 reports that plainly rather than skipping — see its `PROBE-NO-ANSWER` case.

## Step 3 — role-prose-in-context (token probe)

FROZEN mechanism (agent-teams-25s3.4 step 3). The VERB — never this skill — minted a fresh random token before launch and appended a section titled "Preflight self-check" to the probed role's prompt inside the `--agents` payload, carrying a line `PREFLIGHT-TOKEN: <value>`. This skill never learns that value; ground truth lives only in the process that never talks to the probe.

Earlier shapes for this check (counting occurrences of a phrase, matching a line position, or asking for a verbatim or paraphrased reproduction of role text) each either passed a role-less agent or failed a healthy one — because they compared the probe's answer against the role FILE, when the probe only ever sees the assembled PROMPT (which also carries harness-injected text no role file contains, and strips frontmatter so file line numbers don't match). A random token sidesteps all of that: it has no textual relationship to any file content, so nothing can infer, paraphrase, or count its way to the right answer — the probe either has it verbatim in its prompt, or it does not.

Ask `preflight-probe` (via `SendMessage`, the same teammate spawned in Step 2) and wait for its reply before proceeding:

> Your instructions end with a section titled "Preflight self-check" containing a line "PREFLIGHT-TOKEN: <value>". Reply with only that value. If your instructions contain no such section, reply exactly NO-TOKEN. Do not read any file to answer.

Do not ask for "your preflight token" by that bare name — a HEALTHY, correctly role-loaded agent replied `NO-TOKEN` to that phrasing in a live control: it had the text but had not categorised it under that name. Point at the section explicitly, as above; never assume an agent has named or categorised its own instructions.

Hold this check object for Step 5 — nothing is printed here. `status` is a placeholder the VERB always overwrites by comparing `detail` against the token it minted — a value this skill never has. That placeholder MUST fail closed: PASS or SKIP would ship a decorative green if the verb's override step ever broke, in the one check built to catch decorative greens.

- Probe replies with any content: hold `{"check":"role-prose-in-context","status":"FAIL","detail":"<the reply, EXACTLY as received — no trimming, no normalizing, no judging>","witness":"live probe","remediation":""}`.
- Probe replies `NO-TOKEN`: `detail` is the literal string `"NO-TOKEN"` — it answered, and the token was absent from what it saw.
- The `SendMessage` errors, or no reply ever arrives (including the case where Step 2's spawn already FAILed and there is no probe left to ask): `detail` is the literal reserved string `"PROBE-NO-ANSWER"`.

`detail` must never be empty and this check must never be omitted. `PROBE-NO-ANSWER`, `NO-TOKEN`, and an actual observed value are three different facts with three different remediations on the verb side — collapsing any of them into `""` destroys the distinction.

Then continue to Step 4 regardless of what Step 3 held — nothing about Step 4 depends on how this check came out.

## Step 3b — learnings-loaded

Witnesses that a spawned teammate actually LOADS its role learnings (the "memories primed" item). Learnings are not injected by any hook — a role loads them only by running `ateam learnings <role>` itself, so the honest witness is a real teammate doing exactly that inside its own session. Ask `preflight-probe` (the same teammate, via `SendMessage`) and wait for its reply:

> Run this exact command in your shell: `ateam learnings implementer`. Then reply with only the single line of its output that begins with `[learnings implementer:` — verbatim, nothing else. If no such line appears, reply exactly PROBE-NO-ANSWER.

Hold this check object for Step 5 — nothing is printed here. As with role-prose, `status` is a FAIL placeholder the VERB overwrites: the verb independently re-reads the same store and PASSes only when the probe's reported entry count matches ground truth — a number this skill never has, and a fabricated one cannot match. Fail-closed for the same reason.

- Probe replies with a `[learnings implementer: …]` line: hold `{"check":"learnings-loaded","status":"FAIL","detail":"<the line, EXACTLY as received>","witness":"live probe","remediation":""}`.
- The `SendMessage` errors, or no reply arrives (including the case where Step 2's spawn already FAILed and there is no probe to ask): `detail` is the literal reserved string `"PROBE-NO-ANSWER"`.

`detail` must never be empty and this check must never be omitted. Then continue to Step 4 regardless of the outcome.

## Step 4 — shut it down (best-effort, non-blocking)

Send a `shutdown_request` to `preflight-probe` via `SendMessage`. This is cleanup, not part of the verdict: attempt it once, and whether it succeeds, errors, or does not complete promptly, proceed to Step 5 regardless — never wait for it, and never let it stop you from emitting the verdict. No check depends on the probe having shut down. Still nothing printed.

## Step 5 — emit the verdict

This is the ONLY point in the entire run where you print anything. Take the check objects you held from Steps 1-3b (four of them, in this path: `role-types-available`, `teammate-spawns`, `role-prose-in-context`, `learnings-loaded`), place them into one `checks` array, and count `PASS`/`FAIL`/`SKIP` across it for the top-level fields. End your turn by printing that single envelope object — bare JSON text, no code fence, no prose before or after it, nothing else in the message. The fenced block below is shown that way only for this document's readability; your actual output has no backticks around it:

entire final message:

```json
{"checks":[...],"pass":N,"fail":N,"skip":N,"session_id":"<$CLAUDE_CODE_SESSION_ID>"}
```

`session_id` is this session's own id (the `$CLAUDE_CODE_SESSION_ID` environment variable) — the same session that ran the probe, regardless of whether it was launched interactively or by `ateam preflight`'s `--session-id`. This is the exact object `ateam preflight` parses out of `--output-format json`'s `.result` field — a parse failure there is that verb's `probe-session-verdict` check, not something to guard against here beyond printing valid JSON and nothing but that JSON.
