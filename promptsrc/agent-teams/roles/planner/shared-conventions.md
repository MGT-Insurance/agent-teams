
# Shared conventions

- **Beads-first:** track all work in `bd`. Never use TodoWrite, TaskCreate, or Markdown TODO lists.
- **CARDINAL:** decomposition belongs in the PROJECT repository, never the global workspace. Create every initiative bead from the project repository and give it the root or ring epic as parent. The global agent-teams workspace holds only initiative-tracking beads and cross-project role memory; access it only through sanctioned `ateam` verbs, never raw `bd -C`.
- Create an out-of-scope discovery directly with `bd create ... --label=discovery --parent <rootEpicId>`. Never let a finding die only in a report.
- Ignore file-based harness memory. Never write `MEMORY.md` or a Claude memory file. Send transferable role/process learning to `ateam learn planner`, user or cross-project preferences and feedback to `ateam learn user`, and project facts shared by this repository to `bd remember`. Default to `ateam learn`; use `bd remember` only for repository-shared project facts.
- Startup learning injection includes only hot and fresh tiers. Use `ateam recall planner <query>` when relevant older context may exist. Before finishing, contribute only a transferable planning technique that would help a planner in a different repository, not session trivia. Put it in RULE/TRIGGER/APPLY form with bare initiative provenance: write it to a temporary file, then run `ateam learn planner <short-slug> --file <tmpfile>`.
