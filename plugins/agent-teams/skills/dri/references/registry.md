# Shared initiative-registry contract

The global registry contains one initiative issue and is accessed only through `ateam`; `ateam audit` must remain clean. The line-oriented description records the problem, repository, DRI worktree, branch, runtime/mode, root project `epic:`, and repeatable `track-worktree:` paths. It carries no DRI-maintained phase or status field.

Every initiative owns a project-repository root epic. Every contract, implementation, test, and discovery bead uses `--parent <epic-id>`. Repair a legacy initiative with no `epic:` by creating the project epic and recording `epic: <id>` in an initiative note before delegation.

Standby is active only when `standby: true` exists and neither description nor notes contains `standby: released`. When active, raise the exact QUESTION gate `Standby — waiting for direction` and park before clarify. Direction records `standby: released`, clears the gate, and then enters the normal lifecycle. Never write `standby: false`.

Opening a PR does not close an initiative. Record it on the `pr` rail, note delivery, leave the initiative open in `awaiting-merge`, and close only after merge or explicit human closure.

# Initiative registry — schema and commands

The registry lives in the global workspace: one bd ISSUE per initiative (not per session).

## Audit enforcement

`ateam audit` enforces the CARDINAL two-databases rule (SKILL.md): it lists any global-workspace issue lacking the tracking schema below and exits non-zero. Run it in Phase 0 and at wind-down; it must always be clean.

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

**`track-worktree:` (repeatable, D9 — agent-teams-sgr5).** Zero or more additional lines, one per implementer worktree beyond the DRI's own `worktree:` checkout:

    track-worktree: <abs path of an implementer's own worktree>

Unlike the fields above (written once at `ateam register` time), this one accumulates — append a line each time you spawn an implementer into a fresh track worktree, never remove an old one (append recipe: execution.md, "Worktrees (parallel tracks)"). hung-scan's stall detector unions this set with the primary `worktree:` line, so a track worktree's real progress keeps the initiative from reading as flatlined — a path-substring fallback covers legacy/missed cases, but it's weaker; record the line every time.

## Standby field (frozen contract — `--standby`)

An initiative dispatched with `--standby` is registered and its DRI launched, but the DRI **parks on startup waiting for human direction** instead of entering clarify/plan/implement. This keeps `ateam dispatch` / `/dispatch-dri` mechanical (no judgment, no investigation) while the standby *behavior* lives in `/dri`, which already knows how to park on human gates.

The field is a single line in the initiative description, written by `ateam dispatch --standby` (placed directly after the `mode:` line):

    standby: true

Rules (both the writer and every reader copy these verbatim):

- **Off state:** the line is simply **absent**. Never write `standby: false`.
- **Release marker:** once the human gives direction, the DRI records a note containing the line `standby: released`. This is the durable release signal — it survives resume and is independent of gate-label state.
- **Reader rule (`/dri` startup):** standby is *active* iff the description contains `standby: true` **AND** neither the description nor its notes contain `standby: released`. (The release marker is recorded via `ateam note`, so it lands in the initiative's **notes**, not its description — a reader that only inspects the description would never see the release and would re-park forever. Read both, e.g. via `ateam show <id>`.) When active, `/dri` parks with a QUESTION gate worded **"Standby — waiting for direction"** (see the gate protocol) instead of entering Phase 2. When the human provides direction, the DRI appends the `standby: released` note, clears the gate, and proceeds normally through clarify/plan/execute.
- Rationale for the explicit release marker: a standby DRI parks *before* creating any work beads, so "are there beads yet" cannot distinguish a still-waiting initiative from one that has received direction. The append-only `standby: released` note disambiguates unambiguously across any number of resumes.

**Epic invariant (at-e3m):** every new initiative has a root epic bead in the project repo. `ateam register` auto-creates it (`bd -C <repo> create --type=epic`) and writes its id as the `epic:` line. All work beads filed by the DRI, planner, and role agents must use `--parent <epicId>` so they live under the initiative's subtree — ring/phase epics are permitted as children of the root. Bare (unparented) work beads are acceptable only in trivial one-off cases. The dashboard reads `epic:` to filter the drill-in work-bead list to this initiative's subtree.

**DRI ensure-epic step, legacy branch** (initiative registered before at-e3m, `epic:` absent from `ateam show <id>`): (1) create the root epic in the project repo — `bd create --type=epic --title="<initiative title>" --priority=2 --json`, capture the id; (2) record it — `printf 'epic: <epicId>\n' > /tmp/epic-note.txt && ateam note <initiativeId> --file /tmp/epic-note.txt`; (3) use it as `EPIC_ID` for all subsequent spawn prompts this session.

## Commands

Write the body to a temp file first (avoids the newline-# safety prompt), then:

    ateam register --title "<problem statement, short>" --file /tmp/initiative-body.txt

This prints the new issue id on stdout.

- Resume match (open): `ateam resume-match "$PWD"` — id of the OPEN initiative whose description contains an exact `worktree: <path>` line, or nothing (exact-line matching avoids prefix collisions like `/a/b` matching `worktree: /a/b/c`).
- Resume match (closed): `ateam resume-match-closed "$PWD"` — same over CLOSED initiatives (most-recent first); the no-parameter /dri flow calls this when there's no open match, so a delivered/closed initiative in the cwd is surfaced (resume vs. start new) instead of silently ignored.
- Resolve initiative (open, ancestor-or-self): `ateam resolve-initiative "$PWD"` — id of the OPEN initiative whose `worktree:` is the path itself **or any ancestor**, longest match wins; nothing on no match. The plugin's hooks use this ancestor-matching sibling because a session's cwd may be anywhere under the worktree; /dri keeps exact `resume-match` because it owns the checkout root.

  Note: `bd search "<text>"` does NOT search description bodies, only titles — never use it as a fallback.

- Phase changes and session starts: `ateam note <id> --file <file>`.
- On delivery (PR opened): status note `delivered` with the PR URL, leave the initiative **OPEN** in `awaiting-merge`, AND record the PR on the `pr` rail via `ateam pr add` (SKILL.md Phase 5 — required for pr-shepherd routing). Opened is not done — the initiative stays resumable until merged.
- Close: ONLY when the PR is merged or a human explicitly closes the initiative — `ateam close <id> --reason "merged: <PR URL>"` (or the human's reason). Never close on PR-open alone.
- Reopen: `ateam reopen <id>` — when the human chooses to resume a closed (delivered) initiative surfaced by `resume-match-closed`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
