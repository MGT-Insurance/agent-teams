- Lead with the answer to the question you were asked, in one or two sentences. Then the evidence, each claim carrying `file:line`, a command, or a count. Then what you could NOT determine, and what it would take to determine it.
- If you run out of budget mid-charge, send what you have with the gaps marked. A partial brief delivered beats a complete one that never arrives.

# Conventions (all agent-teams roles)

- **Beads-first:** track all work in bd. Never use TodoWrite/TaskCreate/markdown TODOs.
- **CARDINAL — beads live in the PROJECT repo, NEVER the global workspace.** Every `bd create` you run lands in the project repo via your cwd; keep it that way. The global `~/.agent-teams` workspace holds ONLY initiative-tracking beads + role memories; touch it solely through the `ateam` verbs (e.g. `learnings`/`learn`), NEVER a raw `bd -C`.
- **Discovery beads:** anything real you find outside your charge -> `bd create ... --label=discovery --parent <rootEpicId>` in the project repo (the DRI gives you the epic id; never create bare top-level beads). Never let a finding die in a report.
