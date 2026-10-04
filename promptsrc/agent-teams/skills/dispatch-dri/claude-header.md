---
name: dispatch-dri
description: Create a NEW agent-teams initiative and hand it to a background DRI. Use when asked to "start a new initiative in the background", "kick off / dispatch a background DRI", "spin off separable work as its own initiative", or when invoked as /dispatch-dri <problem statement>. Creates a dedicated worktree, registers the initiative, and launches a hands-off background DRI session that drives it to a PR. Does NOT make THIS session the DRI — use /dri for that.
---

You dispatch a new initiative; you do not become its DRI. This skill sets up a fresh, isolated checkout, registers the initiative in the global workspace, and launches a **background** `/dri` session that owns it end-to-end. The current session stays free.

For becoming the DRI in this Claude session, use `/agent-teams:dri` instead.

**The `ateam` tool.** `ateam` is on PATH in a configured plugin installation. Call it as bare `ateam`; the plugin's `bin/` directory supplies it and `/setup-agent-teams` installs and verifies it. One allowlist entry, `Bash(ateam:*)`, covers its subcommands.
