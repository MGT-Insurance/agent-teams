# Gate protocol — every human gate, exact sequence, never vary

A "gate" is any point where you need the human: clarifications, plan approval, scope changes, destructive/outward actions, or PR review.

## Gate kinds

`ateam gate` accepts an optional `--kind` flag:

- `--kind=question` — (default, omitted) the initiative needs a human answer or decision.
- `--kind=review` — the initiative delivered a PR and needs the human to review/merge it.

Both kinds set the `human` label. `kind=review` additionally sets `gate:review`; `kind=question` sets `gate:question`. The dashboard and `ateam human-list` derive the displayed kind from these labels via the **kind-resolution rule**:

- `labels` contains `gate:review` → **REVIEW** (PR awaiting review/merge)
- `labels` contains `gate:question`, OR contains `human` but no `gate:*` → **QUESTION** (backward-compat: pre-existing gated beads predate the `gate:*` label)
- no `human` label → not gated

## The review gate and execution-state

Raising `--kind=review` at delivery (Phase 5) is the DRI's explicit "this is ready for you" intent bit. It makes the initiative _eligible_ for REVIEWABLE — but the dashboard determines REVIEWABLE from the session's **execution state**, not from the gate alone.

The dashboard joins the initiative to any live Claude session whose `cwd` matches the initiative's worktree. The status computation, in priority order:

1. **NEEDS-DECISION** — `human` + `gate:question` labels present. Highest human priority.
2. **IN-PROGRESS** — the joined session is actively working (`status == "busy"` / `state == "working"`). This **overrides** the review gate. A DRI that keeps working on a PR after opening it (e.g. improving the diff) correctly reads as IN-PROGRESS.
3. **REVIEWABLE** — `human` + `gate:review` present AND no actively-working session.
4. **IN-PROGRESS** — open initiative, no review/question gate.

Conservative rule: while the DRI's session is still running — including teardown tidying — the initiative reads as IN-PROGRESS. It flips to REVIEWABLE only once the session goes idle or exits. This is intentional: the dashboard never tells the human to review too early, even if the PR is technically stable while the DRI is still cleaning up.

The DRI sets NO phase field and maintains no status field. The run/park state of its session IS the signal.

## Structured ask form (primary)

For question gates, default to the structured form. It surfaces the genuinely load-bearing decision as the ask — decision + recommendation + the single key alternative — rather than burying the real fork in prose.

```bash
ateam gate <id> --decision "<one line ≤120 chars>" \
               --recommendation "<short>" \
               --alternative "<the one key alternative>" \
               [--context-file <file>] \
               [--kind=question]
```

Field constraints:

- `--decision` — the actual decision needed. ≤120 chars. Required. One line.
- `--recommendation` — the DRI's recommended answer. One short line.
- `--alternative` — the single key alternative to the recommendation. One short line.
- `--context-file` — optional prose context, ≤280 chars in the file.

The `--file` prose form remains supported as a fallback — use it when the ask genuinely does not fit the structured schema (e.g. a plan-approval gate with a long decomposition attached). When no structured block is present, `/initiatives` and `ateam human-list` render the raw notes.

Guidance: name the fork. The structured form works because it forces you to state _what decision_ the human is actually making, not just what you need to explain. If you can't fill in `--decision` with one concrete line, the question is not crisp yet — refine it before gating.

## Raising a gate

1. **Record the question/note AND flag needs-human** in one call:

   Structured form (preferred for question gates):
   ```
   ateam gate <initiative-id> --decision "..." --recommendation "..." --alternative "..."
   ```

   Prose form (review gates and fallback):
   ```
   ateam gate <initiative-id> --file /tmp/gate-note.txt              # question gate (default)
   ateam gate <initiative-id> --file /tmp/gate-note.txt --kind=review # review gate — PR ready
   ```

   (This notes the message and adds the `human` + `gate:<kind>` labels atomically.)
   (Note: `bd human respond` and `bd human dismiss` are broken in bd 1.0.4 — the label-based approach is the verified path. `bd human list` / `ateam human-list` still works to enumerate flagged issues; see the framework repo's docs/verifications.md.)

2. **Ask and park.** Interactive: ask directly (AskUserQuestion or plain text) and continue when answered. Backgrounded (`--bg`): end the turn with the question as plain text — the session parks; the human sees it on attach, or via /initiatives.
3. **While parked:** keep every workstream that does not depend on the answer moving. Parking the question never parks the team.
4. **On answer/merge:** clear the flag — write the response to a temp file if multi-line, then:
   `ateam clear-gate <initiative-id> --file /tmp/gate-response.txt`
   (Or without `--file` to just remove the labels when no comment is needed.) `clear-gate` removes the `human` label AND any `gate:*` label regardless of kind. Note the resolution on the initiative, then proceed. (`bd human respond/dismiss` are broken in bd 1.0.4 — the label-remove workaround is the verified path; see the framework repo's docs/verifications.md.)

Why this must never vary: the flag is the only machine-wide signal that an initiative is waiting on a human. A gate raised any other way is invisible.
