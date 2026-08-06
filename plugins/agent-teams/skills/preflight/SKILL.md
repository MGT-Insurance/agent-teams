---
name: preflight
description: Verify an agent-teams install by spawning one real teammate and witnessing what it actually got. Use when asked to "run preflight", "check agent-teams is working", "verify the install", or when invoked as /agent-teams:preflight. This is the primary check — directly invocable inside an already-dispatched session as a first-class standalone mode, not a fallback. `ateam preflight` is a launcher on top of this skill for use OUTSIDE a dispatched session; it does not replace it.
---

You verify that role teammates spawned from this session actually work, by spawning one real `agent-teams-implementer`, confirming it responds, and shutting it down. Whether its role definition actually attached is NOT something this skill asks the probe about — that property is proven elsewhere, by stronger evidence than any in-session question could produce (see Step 3). `ateam preflight` (a separate Go verb) exists only to launch this skill from a session that has no role agents of its own yet.

Contract: `agent-teams-25s3.2` (frozen) defines the status vocabulary, the check-result and top-level JSON shapes, and exit-code handling. This skill implements the JSON side of that contract; it does not restate the reasoning behind it.

## Checks this skill owns

| check id | witnesses |
|---|---|
| `role-types-available` | `agent-teams-implementer` is a spawnable agent type in this session |
| `teammate-spawns` | the Agent tool call actually returns a response from the probe |
| `role-prose-in-context` | ships `UNPINNED` by design — no in-session question can soundly distinguish a role-loaded probe from one with nothing attached; see Step 3 |

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

This spawn exists to exercise the spawn mechanism itself — its purpose is the sidecar record it produces (read by the GO track's `role-definition-attached`, `role-model-attached`, `spawn-record-present`, and `spawn-permission-mode` checks), not any answer the probe gives. The prompt should not name the role or quote role prose; it only needs to elicit a response. For example:

> This is a one-shot preflight connectivity check, unrelated to any task. Reply with a brief acknowledgement, then take no further action.

If the Agent call errors, hangs with no response, or otherwise produces nothing:

```json
{"check":"teammate-spawns","status":"FAIL","detail":"<what happened — error text or \"no response\">","witness":"live probe","remediation":"confirm claude is on PATH and the roles directory resolves; retry the spawn"}
```

If it responds at all — with any content:

```json
{"check":"teammate-spawns","status":"PASS","detail":"probe returned a response","witness":"live probe","remediation":""}
```

Either way, continue to Step 3 — `role-prose-in-context`'s status does not depend on how the spawn went.

## Step 3 — role-prose-in-context ships UNPINNED

No in-session question can soundly witness whether a role definition attached to the probe. Do not add one — every shape tried failed against a real control run, for reasons specific to the property itself, not to the phrasing, and each fix broke a different way:

- **Exact-match verbatim reproduction** fails a HEALTHY install: a correctly role-loaded agent PARAPHRASES rather than reproducing its own instruction text verbatim.
- **Relaxing to "contains" or semantic equivalence** fails the other way: the anchor phrase handed to the probe leaks enough of the answer that a role-less agent can infer a plausible match with nothing attached — discriminating power goes DOWN, not up.
- **Refusal is available to both classes.** A healthy agent may decline to quote its own instructions verbatim; so may a generic agent with nothing attached. It cannot be scored soundly in either direction: ruling refusal FAIL reds a healthy install; ruling it PASS or SKIP greens the role-less case, which is the likely path a declining agent takes, not a corner case.

This is exactly what contract artifact (2) means by `UNPINNED`: "the property CANNOT be witnessed by this tool, by design." Emit it unconditionally, regardless of how Step 2's spawn went:

```json
{"check":"role-prose-in-context","status":"UNPINNED","detail":"no in-session question can soundly witness role-prompt attachment: a probe that disobeys \"do not read any file\" can answer correctly with nothing attached; refusal is available to both a role-loaded and a role-less agent; a healthy agent may paraphrase rather than reproduce","witness":"no in-session question distinguishes a role-loaded agent from one that declines or paraphrases; the assembled prompt the live agent actually received cannot be read by the verb (sees only what it built), by this skill, or reliably self-reported by the probe","remediation":""}
```

**What IS proven, elsewhere — `UNPINNED` here does not mean unverified.** Role attachment itself is proven by `role-definition-attached` (agent-teams-25s3.3): a harness-written sidecar record the spawned agent cannot influence. A non-empty prompt body is proven deterministically before any session even launches: `parseRoleFile` hard-fails ("role body is empty after stripping frontmatter", `internal/verbs/agentsjson.go`), surfacing as exit 2 through `roles-payload-builds`. The one genuinely unwitnessed residual is narrower than either of those: whether the assembled prompt was truncated or mangled somewhere between the payload the verb built and the agent that actually ran — something no party can observe (not the verb, which sees only what it built; not this skill, which cannot read the assembled prompt; not the probe's own self-report, which is precisely the unreliable channel this design eliminates).

## Step 4 — shut it down

`SendMessage` a `shutdown_request` to `preflight-probe`. This is a one-shot probe — it does not need to persist past this run.

## Step 5 — emit the verdict

Aggregate the checks from whichever steps ran into the contract's top-level shape and end your turn on it — bare, no prose before or after, no code fence:

```json
{"checks":[...],"pass":N,"fail":N,"skip":N,"unpinned":N,"session_id":"<$CLAUDE_CODE_SESSION_ID>"}
```

`session_id` is this session's own id (the `$CLAUDE_CODE_SESSION_ID` environment variable) — the same session that ran the probe, regardless of whether it was launched interactively or by `ateam preflight`'s `--session-id`. `pass`/`fail`/`skip`/`unpinned` are the counts of that token across `checks`. This is the exact object `ateam preflight` parses out of `--output-format json`'s `.result` field — a parse failure there is that verb's `probe-session-verdict` check, not something to guard against here beyond emitting valid, bare JSON.
