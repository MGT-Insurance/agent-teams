---
name: condense
description: Curate learnings. Condense before draining fresh; this order is load-bearing. A lock prevents concurrent runs.
---

Use bare `ateam`; setup: `agent-teams-codex:setup-agent-teams`.
- `agent-teams-codex:condense <role>` always processes exactly that role after acquiring the lock. It bypasses the all-role gate.
- `agent-teams-codex:condense` runs one all-role, gate-controlled sweep.
