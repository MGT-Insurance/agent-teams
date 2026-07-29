# Initiative registry — schema and commands

The registry lives in the global workspace: one bd ISSUE per initiative (not per session).

**Invariant:** the global workspace contains ONLY initiative-tracking beads (every one carries a `worktree:` line, per the schema below) and role memories (`bd remember`, a separate store). It NEVER holds work beads — feature/plan/task/discovery beads all live in the PROJECT repo's `.beads`. `ateam audit` enforces this: it lists any global-workspace issue lacking the tracking schema and exits non-zero. Run it in Phase 0 and at wind-down; it must always be clean.

## Description schema (line-oriented; the compaction hook greps `worktree:`)

    problem: <one-line problem statement>
    repo: <abs path to main repo>
    worktree: <abs path of the checkout the DRI owns>
    branch: <branch name>
    team: <team slug>
    mode: interactive|bg
    standby: true    # OPTIONAL — present only when dispatched --standby; written directly after mode:
    epic: <root epic bead id in the project repo, e.g. agent-teams-x6ce>

There is NO `phase:` or `status:` field. The DRI maintains no phase; execution-state (IN-PROGRESS / REVIEWABLE / NEEDS-DECISION) is computed by the dashboard from gate labels and the live session's run/park state.

**`track-worktree:` (repeatable, D9 — agent-teams-sgr5).** Zero or more additional lines, one per implementer worktree the DRI spawns beyond its own `worktree:` checkout:

    track-worktree: <abs path of an implementer's own worktree>

Unlike every other field above (written once at `ateam register` time), this one accumulates over the initiative's life — append a new line each time you spawn an implementer into a fresh track worktree; never remove an old one. See execution.md's "Worktrees (parallel tracks)" section for the exact append recipe. hung-scan's work-product stall detector unions this set with the primary `worktree:` line when it probes for git activity, so a track worktree's real progress is what keeps the initiative from reading as flatlined — omitting this line doesn't break anything outright (a path-substring fallback covers legacy/missed cases) but does leave detection weaker for that worktree.

## Standby field (frozen contract — `--standby`)

An initiative dispatched with `--standby` is registered and its DRI launched, but the DRI **parks on startup waiting for human direction** instead of entering clarify/plan/implement. This keeps `ateam dispatch` / `/dispatch-dri` mechanical (no judgment, no investigation) while the standby *behavior* lives in `/dri`, which already knows how to park on human gates.

The field is a single line in the initiative description, written by `ateam dispatch --standby` (placed directly after the `mode:` line):

    standby: true

Rules (both the writer and every reader copy these verbatim):

- **Off state:** the line is simply **absent**. Never write `standby: false`.
- **Release marker:** once the human gives direction, the DRI records a note containing the line `standby: released`. This is the durable release signal — it survives resume and is independent of gate-label state.
- **Reader rule (`/dri` startup):** standby is *active* iff the description contains `standby: true` **AND** neither the description nor its notes contain `standby: released`. (The release marker is recorded via `ateam note`, so it lands in the initiative's **notes**, not its description — a reader that only inspects the description would never see the release and would re-park forever. Read both, e.g. via `ateam show <id>`.) When active, `/dri` parks with a QUESTION gate worded **"Standby — waiting for direction"** (see the gate protocol) instead of entering Phase 2. When the human provides direction, the DRI appends the `standby: released` note, clears the gate, and proceeds normally through clarify/plan/execute.
- Rationale for the explicit release marker: a standby DRI parks *before* creating any work beads, so "are there beads yet" cannot distinguish a still-waiting initiative from one that has received direction. The append-only `standby: released` note disambiguates unambiguously across any number of resumes.

**Epic invariant (at-e3m):** every new initiative has a root epic bead in the project repo. `ateam register` auto-creates this epic (via `bd -C <repo> create --type=epic`) and writes its id as the `epic:` line in the description. All work beads filed by the DRI, planner, and role agents must use `--parent <epicId>` so they live under the initiative's subtree. Multiple ring/phase epics are permitted — they are children of the root epic. Bare (unparented) work beads are acceptable only in trivial one-off cases. The `epic:` field is also written to initiative notes by the DRI ensure-epic step when absent (legacy initiatives). The dashboard reads `epic:` to filter the drill-in work-bead list to just this initiative's subtree.

**DRI ensure-epic step, legacy branch (initiative registered before at-e3m, `epic:` field absent from `ateam show <id>`):** (1) in the project repo, create the root epic — `bd create --type=epic --title="<initiative title>" --priority=2 --json` — and capture the epic id from the JSON output; (2) record it in the initiative registry — `printf 'epic: <epicId>\n' > /tmp/epic-note.txt && ateam note <initiativeId> --file /tmp/epic-note.txt`; (3) use that epic id as `EPIC_ID` for all subsequent spawn prompts in this session.

## Commands

Write the body to a temp file first (avoids the newline-# safety prompt), then:

    ateam register --title "<problem statement, short>" --file /tmp/initiative-body.txt

This prints the new issue id on stdout.

- Resume match (open): `ateam resume-match "$PWD"` — prints the id of the OPEN initiative whose description contains an exact `worktree: <path>` line, or nothing on no match. Exact-line matching avoids prefix collisions (e.g. `/a/b` matching `worktree: /a/b/c`).
- Resume match (closed): `ateam resume-match-closed "$PWD"` — same match over CLOSED initiatives (most-recently-created first). The no-parameter /dri flow calls this when there is no open match, so a delivered/closed initiative in the cwd is surfaced to the human (resume vs. start new) instead of silently ignored.
- Resolve initiative (open, ancestor-or-self): `ateam resolve-initiative "$PWD"` — prints the id of the OPEN initiative whose `worktree:` is the path itself **or any ancestor of it**, so a subdirectory resolves too; the most specific (longest) worktree wins. Prints nothing on no match. This is the ancestor-matching sibling of `resume-match` — the plugin's hooks use it because a session's cwd may be anywhere under the worktree, while /dri keeps `resume-match` because it owns the checkout root and exact matching there avoids resuming the wrong initiative.

  Note: `bd search "<text>"` does NOT search description body content — it only matches titles. Do not use it as a fallback.

- Phase changes and session starts: `ateam note <id> --file <file>`.
- On delivery (PR opened): status note `delivered` with the PR URL, leave the initiative **OPEN**, AND record the structured `pr:` field (see SKILL.md Phase 5 — required for pr-shepherd routing). A PR that is merely opened is not done — the initiative stays open in an `awaiting-merge` state so a future no-parameter /dri can resume it.
- Close: ONLY when the PR is merged or a human explicitly closes the initiative — `ateam close <id> --reason "merged: <PR URL>"` (or the human's reason). Never close on PR-open alone.
- Reopen: `ateam reopen <id>` — when the human chooses to resume a closed (delivered) initiative surfaced by `resume-match-closed`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
