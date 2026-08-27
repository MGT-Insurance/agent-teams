---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads and optional Codex compatibility, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, installs the ateam launcher, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

If you set AGENT_TEAMS_HOME to a custom path, use that literal path in place of `~/.agent-teams` below.
