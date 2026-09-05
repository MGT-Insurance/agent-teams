---
name: condense
description: Triggered manually or at wind-down to curate each role's learnings — condense FIRST, drain the fresh tier afterward (that order is load-bearing). A role is gated on how much NEW fresh material it has accumulated, not on the total size of its served set. Lock-guarded; skips cleanly if another condense is already running.
---

**The `ateam` tool.** On PATH via the plugin's `bin/` (installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere.

## Parse the argument

- **`/agent-teams:condense <role>`** — condense ONLY that named role (e.g. `dri`, `implementer`). Lock-guarded (same try-acquire/skip semantics as the all-roles form); NO gate — an explicit single-role invocation always condenses regardless of what `ateam condense-check` would report. See **Single-role form** below.
- **`/agent-teams:condense` (no arg)** — all-roles sweep (see below).

---

## Single-role form (`/agent-teams:condense <role>`)
