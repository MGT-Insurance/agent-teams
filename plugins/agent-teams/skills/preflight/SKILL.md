---
name: preflight
description: Verify an agent-teams install by spawning one real teammate and witnessing what it actually got. Use when asked to "run preflight", "check agent-teams is working", "verify the install", or when invoked as /agent-teams:preflight. This is the primary check — directly invocable inside an already-dispatched session as a first-class standalone mode, not a fallback. `ateam preflight` is a launcher on top of this skill for use OUTSIDE a dispatched session; it does not replace it.
---

You verify that role teammates spawned from this session actually get their role definition, by spawning one real `agent-teams-implementer` and asking it something only a teammate with role instructions attached can answer. This is the substance of the check — `ateam preflight` (a separate Go verb) exists only to launch this skill from a session that has no role agents of its own yet.

Contract: `agent-teams-25s3.2` (frozen) defines the status vocabulary, the check-result and top-level JSON shapes, and exit-code handling. This skill implements the JSON side of that contract; it does not restate the reasoning behind it.

## Checks this skill owns

| check id | witnesses |
|---|---|
| `role-types-available` | `agent-teams-implementer` is a spawnable agent type in this session |
| `teammate-spawns` | the Agent tool call actually returns a response from the probe |
| `role-prose-in-context` | the probe's answer is concrete and role-consistent, not a generic agent's disclaimer |

## Step 1 — role types available

Check the "Available agent types for the Agent tool" listing already present in this session's context for `agent-teams-implementer`.

**If present:** continue to Step 2.

**If absent:** this session has no agent-teams role agents at all — it was not launched by `ateam dispatch`/`ateam resume` (or by `ateam preflight`, which launches one for exactly this reason). That is a normal, expected way to invoke `/agent-teams:preflight` directly, not a broken run — report it accurately and stop. Do not spawn anything. Emit exactly one check, then the top-level envelope, and end your turn on that bare JSON:

```json
{"check":"role-types-available","status":"FAIL","detail":"agent-teams-implementer is not in this session's available agent types","witness":"available agent types listing in session context","remediation":"run \"ateam preflight\" to launch a dispatched probe session, or invoke this skill from inside a session already launched by \"ateam dispatch\"/\"ateam resume\""}
```

```json
{"checks":[<the object above>],"pass":0,"fail":1,"skip":0,"unpinned":0,"session_id":"<$CLAUDE_CODE_SESSION_ID>"}
```

## Step 2 — spawn the probe teammate

`role-types-available` passed (record it as such, `witness: "available agent types listing in session context"`, `remediation: ""`). Spawn exactly one teammate via the Agent tool:

- `subagent_type: "agent-teams-implementer"` — the hyphen key. Never the colon form; it no longer exists.
- `name: "preflight-probe"`
- `mode: "bypassPermissions"`
- **No `model` argument.** This is load-bearing (agent-teams-25s3.2 amendment): the sidecar's resolved model is only meaningful evidence on the Go side of this initiative when the caller passed none.

The prompt must NOT name the role, must NOT quote any role prose, and must instruct the probe to answer only from its own instructions as already given to it, without reading any file. For example:

> You are being asked a single question. Do not read any file to answer it — answer only from the instructions you were already given. Do not identify what role or instructions you have. Question: how many times does the literal token `SendMessage` appear in your instructions, and what is the first word of the line containing the last occurrence? Reply with only the number and the word.

If the Agent call errors, hangs with no response, or otherwise produces nothing to judge:

```json
{"check":"teammate-spawns","status":"FAIL","detail":"<what happened — error text or \"no response\">","witness":"live probe","remediation":"confirm claude is on PATH and the roles directory resolves; retry the spawn"}
```

Emit `role-prose-in-context` as `SKIP` (its dependency failed — contract agent-teams-25s3.2 artifact (2)), `detail: "teammate-spawns did not produce a response to judge"`, `witness: "live probe"`, `remediation: ""`, then go to Step 5.

If it responds at all — with any content, even a bad answer — record:

```json
{"check":"teammate-spawns","status":"PASS","detail":"probe returned a response","witness":"live probe","remediation":""}
```

and continue to Step 3.

## Step 3 — judge the reply

This check's predicate is deliberately weak, and it must be labelled as such — see "What this check does NOT prove" below. No fixture value lives anywhere in the role prose or in this skill; there is no numeric answer to compare against.

**PASS** iff the reply gives a concrete, role-consistent answer (an actual count and an actual word) — i.e. it looks like something computed by reading a real instruction body, whatever that number turns out to be.

**FAIL** iff the reply reports having no role-specific instructions, describes itself as a generic or general-purpose agent, declines the question because it "has no such instructions", or otherwise fails to produce a concrete count-and-word pair.

```json
{"check":"role-prose-in-context","status":"<PASS|FAIL>","detail":"probe answered: <verbatim reply, truncated to one line>","witness":"live probe (weak: an agent in this repo could in principle read roles/*.md)","remediation":"<empty on PASS; on FAIL: \"confirm buildAgentsJSON is attaching a non-empty prompt body for agent-teams-implementer\">"}
```

**What this check does NOT prove:** a concrete answer shows the prompt body reached the probe and was non-empty and in context. It does NOT independently confirm the answer is *correct* — this skill cannot read `roles/implementer.md` to check the count without contaminating the very thing it's probing, so it cannot verify the content is *right*, only that it is present, concrete, and not a generic agent's disclaimer. The strong evidence that the correct role definition attached is the verb-side sidecar check (`ateam spawn-check`, agent-teams-25s3.3); this probe only adds what that sidecar cannot show.

## Step 4 — shut it down

`SendMessage` a `shutdown_request` to `preflight-probe`. This is a one-shot probe — it does not need to persist past this run.

## Step 5 — emit the verdict

Aggregate the checks from whichever steps ran into the contract's top-level shape and end your turn on it — bare, no prose before or after, no code fence:

```json
{"checks":[...],"pass":N,"fail":N,"skip":N,"unpinned":N,"session_id":"<$CLAUDE_CODE_SESSION_ID>"}
```

`session_id` is this session's own id (the `$CLAUDE_CODE_SESSION_ID` environment variable) — the same session that ran the probe, regardless of whether it was launched interactively or by `ateam preflight`'s `--session-id`. `pass`/`fail`/`skip`/`unpinned` are the counts of that token across `checks`. This is the exact object `ateam preflight` parses out of `--output-format json`'s `.result` field — a parse failure there is that verb's `probe-session-verdict` check, not something to guard against here beyond emitting valid, bare JSON.
