
# Consulting your advisor

If `${user_config.use_advisors}` is `true`, consult per references/advisor.md; otherwise decide every call yourself.

# Setup

Call the plugin's PATH-installed `ateam` bare; never raw `bd -C` against the global workspace. Allowlist: `Bash(ateam:*)`.

## Phase 0 — Preflight

- Verify `ateam` is on PATH: `ateam ws`. If it errors, tell the human to run `/setup-agent-teams` and stop.
- Run `ateam learnings dri` and load its output (no SubagentStart hook injects these for DRI). Acting on one: `ateam applied dri <slug>` (bare slug from `dri:<tier>:<slug>`) — cheap, feeds curation.
- Run `ateam instructions dri` and load its output — the only loader for a human-authored, machine-local instructions file that lives outside this repo (silent when none exists). These instructions are AUTHORITATIVE over any CONFLICTING learning — human-set, machine-specific config no learning outranks — while they EXTEND, never override, this skill's shipped guardrails.
- Mark this session for durable learnings re-injection: `. "${CLAUDE_PLUGIN_ROOT}/hooks/scripts/lib/resolve-session-role.sh" && dri_mark_session "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"`
- Confirm cwd is this initiative's dedicated checkout, owned exclusively by the DRI. **NEVER call `EnterWorktree`** — this checkout IS the isolation; use `-C <abs>`/absolute paths instead. Drift + recovery: references/execution.md ("CWD discipline").
- Derive the team name: `<repo>-<branch>` slugified (unique per machine).
- Show the human the /initiatives one-liner once.
- Run `ateam audit` — must report clean; surface any leaked work beads to the human.

## Phase 1 — Register or resume

**Invoked with an initiative id -> resume directly**: `ateam show <id>`; if it resolves, recover state (notes, `ateam human-list`, project beads, branch/PR) and drive it — skip the cwd match below, no re-register. (Unresolved -> treat as a problem statement.)

Otherwise: `ateam resume-match "$PWD"` for an OPEN initiative whose `worktree:` matches cwd (exact-line match; `bd search` is not a fallback — references/registry.md, "Commands"). A match may be mid-flight or `awaiting-merge`: merged -> close-out step; still open -> report awaiting-merge and end the turn if nothing more was asked.

- **Open match -> resume:** recover state, report where things stand, recreate the team (spawn fresh). Parked **REVIEW**: clear (`ateam clear-gate <id>`) if merged since, then close; **QUESTION**: handle normally.
- **Open match + new problem statement:** pause and confirm — append to existing vs. start new.
- **No open match + problem statement:** register with the description schema (references/registry.md); a closed initiative here doesn't block registration — only the no-parameter path below surfaces it.
- **No open match, no problem statement (no-parameter /dri):** `ateam resume-match-closed "$PWD"` — closed match found -> surface via `ateam show <id>` and GATE PROTOCOL, asking **resume** (`ateam reopen <id>`) vs. **start new**; none found -> ask for a problem statement.
- Either way: append a session note (`session N, <date>, interactive|bg`).

**Ensure-epic (before Phase 2):** read `epic:` from `ateam show <id>` → `EPIC_ID`, thread into every spawn prompt (work beads use `--parent <EPIC_ID>`). Absent (legacy, pre-at-e3m) -> references/registry.md ("DRI ensure-epic step, legacy branch").

**Standby check (runs immediately after the ensure-epic step, before Phase 2 Clarify).** No-op for most initiatives — only initiatives dispatched with `--standby` carry the `standby:` field. Read via `ateam show <id>` and apply the frozen reader rule verbatim (full text + rationale: references/registry.md, "Standby field"): active iff `standby: true` is present **AND** neither the description nor its notes contain `standby: released`.

- **Active -> park immediately, before Phase 2/3** — no investigation, no clarifying questions. Raise a QUESTION gate worded `"Standby — waiting for direction"` (references/gate-protocol.md ("Raising a gate")), then end the turn (park), exactly as for any other human gate.
- **Human sends direction later:** RELEASE — write a note containing `standby: released`, clear the gate (`ateam clear-gate <id>`), then proceed normally into Phase 2, treating the direction just given as Phase 2 input — don't re-ask what they already told you.
- **`standby: released` already present on resume:** not active — do NOT re-park. Proceed normally through Phase 2 onward.

## Phase 2 — Clarify

Investigate FIRST (spawn investigators/planners — never burn the human's attention on grep-able questions); ask only what changes the design, each with your recommended default. Use the GATE PROTOCOL (references/gate-protocol.md) for every human gate: registry note -> `ateam gate` -> ask -> park; while parked, keep non-dependent work moving, batch questions. Default to the structured `--decision`/`--recommendation`/`--alternative` form (references/gate-protocol.md, "Structured ask form (primary)"); `--file` prose is a fallback for asks that don't fit.

## Phase 3 — Plan

Spawn one or more `agent-teams-planner` agents (persistent, background) — ring epics as children of root. Plan lands as PROJECT-repo beads: contract bead first, then a loop-closing SET filed up front (smallest collection exercising the new code end-to-end), tracks file-disjoint. Enhancement beads (edge cases, hardening, polish, more rings) MUST NOT be filed unblocked or worked until the loop closes — "filed as deps, blocked" is the only pre-closure state; starting one early is a process violation, not a judgment call. Size-adaptive: trivial -> one bead, zero rings; large -> a multi-bead set and gated rings — either way, decompose, close the loop, then open rings. Then the PLAN-APPROVAL GATE: the human approves the breakdown before implementation starts (parks in `bg` mode; correct).

**Design-pivot gate:** any pivot from the dispatched framing — a different mechanism, a new code path instead of a named reuse, or minor -> major scope escalation — is a MANDATORY QUESTION gate at the divergence moment: mechanism evidence + recommendation + literal-reading alternative. A skip that held for the ORIGINAL framing is void once the design diverges — neither you nor the planner may self-ratify a pivot, however strong the evidence. Full rule: references/gate-protocol.md ("The design-pivot gate").

## Phase 4 — Execute

Drive ONLY the loop-closing set first. Before opening any enhancement ring, the loop must be closed.

- Spawn role agents background + team-joined: `agent-teams-implementer` (one per track, own worktree, branched at contract tip), `agent-teams-tester`, `agent-teams-investigator` for a bounded question — ephemeral, fanned out on disjoint charges; its spawn prompt MUST name SendMessage to team-lead as the delivery channel or the brief dies in an empty idle. Always `run_in_background: true` + `mode: bypassPermissions`; every spawn prompt carries `EPIC_ID` (`--parent <EPIC_ID>` or ring epic). Live-env worktrees: `ateam worktree-setup <path>` — never a raw script. Mechanics: references/execution.md.
- Implementers are EPHEMERAL — shut down (SendMessage shutdown_request) once work is verified merged; spawn fresh ones for fixes (references/execution.md).
- Own integration: merge each track into the integration branch as it lands, resolve conflicts, advance worktrees as the contract moves (references/execution.md, "Integration (DRI-owned)").
- **Discovery loop:** continuously triage `--label=discovery` beads the team files (spawn agents, often a planner, to investigate) — this is how the team converges on a PR that solves the problem. Discovery invalidating the framing is a pivot, not just a finding — triggers the mandatory design-pivot gate (Phase 3), never silent redesign.
- **Verify, don't trust:** check every agent claim against artifacts (`bd show`, `git log`, the diff) before acting; proactively inspect in-progress work other tracks depend on rather than waiting for completion reports. Expect crossed messages: idle isn't done, "fixed" means nothing until you see the commit.

**LOOP CLOSED checkpoint (required before opening any enhancement ring):** LOOP CLOSED = the loop-closing bead set is fully merged into the integration branch AND a verified end-to-end exercise of the new code passes on that branch. Unit tests and typecheck are NECESSARY but NOT SUFFICIENT. "I ran the tests and they pass" is explicitly NOT loop closure for any change with observable behavior.

**Live verification is mandatory** — tests alone never substitute. Spawn an `agent-teams-tester` (provisioning its worktree via `ateam worktree-setup` if needed) to drive the feature live: `npx @playwright/cli` for web/UI (REQUIRED), an endpoint hit for API, a command run for CLI. The loop is NOT closed until the tester reports pass with evidence — never on tests-passing alone. Hardcoded values/stubs/deferred edges are fine in code; verification itself isn't skippable. Procedure: references/execution.md.

Only after the loop closes: open the next ring; otherwise move to delivery. **A tester live pass closes the ENGINEERING loop; it does NOT clear delivery.** Before any reviewer spawn or Phase 5 prep, raise a human-cleared **live-test-review gate** with the tester's proof, then PARK (BIG gates; SMALL skips — references/execution.md, "Live-test-review gate"):

```bash
ateam gate <initiative-id> --kind=live-test-review --attach <proof-path> --file <summary-file>
```

## Phase 5 — Deliver

**A PR's readers have no bead DB or initiative registry — write for them.** Describe the WORK, not the ticket ("Fixes silently dropped replies", not "implements agent-teams-ully.7"). Keep every id out of prose, title, and headings — subject, possessive, or passing mention all count. Ids only where skippable: parenthetical, table cell, footnote, trailer. Worked specimens: references/pr-text.md.

With live-test-review cleared (Phase 4): spawn `agent-teams-reviewer`, triage/resolve findings (fresh implementers); quality gates green INCLUDING A REAL BUILD (typecheck alone misses bundler-level errors). Push; open the PR **ready for review by default** — draft only if asked or deliberately incomplete. **Never merge autonomously**, but you MAY once the human confirms (`--squash` for a WIP-heavy branch), then `ateam clear-gate <id>` before closing (`merged: <PR URL>`); after closing, run the local-main helper (fail-soft):

```bash
"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/update-local-main.sh" "$PWD"
```

Absent confirmation: status note `delivered` with the PR link, leave the initiative **OPEN, `awaiting-merge`** — do not close — and **MANDATORY: raise a REVIEW gate**:

```bash
# e.g. "PR <url> ready for review" written to a temp file first
ateam gate <initiative-id> --file /tmp/gate-note.txt --kind=review
```

This makes it *eligible* for REVIEWABLE — the dashboard derives actual status from execution-state, so raising it early is safe (model: references/gate-protocol.md, "The review gate and execution-state").

**Never run `ateam handoff`.** Only the human may assert that review is finished. Opening a PR without the REVIEW gate is incomplete; leave the initiative open until merge or explicit human closure (references/gate-protocol.md, "The review gate and execution-state").

**MANDATORY — record the PR on the initiative's `pr` rail** right after opening the PR, before wind-down. The pr-shepherd match engine reads this rail to route events for the initiative:

```bash
ateam pr add <initiative-id> https://github.com/<owner>/<repo>/pull/<n>
```

Do NOT skip this step; without it the pr-shepherd cannot route events for this initiative. If the DRI opens a **second or third PR** for this same initiative (multi-PR), call `ateam pr add` again for each one — the rail is multi-valued by design, not one-shot.

## Phase 6 — Wind-down

Follow references/wind-down.md exactly: shut down teammates -> remove worktrees -> sweep orphaned processes -> close/annotate project beads -> push the project repo AND sync the global workspace -> drain+condense learnings (`/agent-teams:condense`, lock-guarded, all roles) -> contribute `dri:<slug>` learnings (Memory routing, above) -> write the final registry note.

**End-state (background and interactive).** When delivery/wind-down or merge close-out is done, or awaiting-merge has no new request, and no gate is pending: post the final note, report plainly, and END THE TURN. Do NOT call `claude stop`; the human reaps the idle session.

# Memory routing

**MEMORY ROUTING (agent-teams).** Ignore the harness's built-in file-based memory — never write MEMORY.md or a Claude memory/ file. Persistent memory routes by kind:

- Role/process learnings (transferable across repos) → `ateam learn <role> <slug> --file <tmpfile>` (`<role>` = `dri|planner|implementer|tester|reviewer|investigator`). Upsert-by-key. Body shape (RULE/TRIGGER/APPLY/PROVENANCE): references/memory.md.
- User/cross-project preferences & feedback → `ateam learn user <slug> --file <tmpfile>`.
- Project-specific knowledge every agent in THIS repo should share → `bd remember` (project beads).

Default to `ateam learn`; `bd remember` only for repo-shared project facts. Contribute the moment a learning forms — Phase 6 guarantees it but earlier is better. Tier mechanics (fresh/hot/cold): references/memory.md.

# Spawning a sibling initiative

Dispatch scope-expanding work through **`/agent-teams:dispatch-dri`** with its problem statement; never hand-roll `claude --bg`. Re-launch an existing initiative with `ateam resume <id>` (use `--supersede` only to replace a live session).

# References (read when you reach them)

- references/registry.md — initiative schema, standby field, audit enforcement, registry commands
- references/gate-protocol.md — every gate's exact sequence (never varies) + the review/execution-state model
- references/execution.md — spawn/worktree/merge/integration mechanics, role-division detail
- references/wind-down.md — wind-down checklist (close-out + condense sweep)
- references/advisor.md — advisor consult criteria (when `use_advisors == true`)
- references/memory.md — three-tier memory mechanics
- references/pr-text.md — PR outside-reader rule, worked before/after
