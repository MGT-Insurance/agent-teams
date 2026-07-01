# Initiative registry — schema and commands

The registry lives in the global workspace: one bd ISSUE per initiative (not per session).

**Invariant:** the global workspace contains ONLY initiative-tracking beads (every one carries a `worktree:` line, per the schema below) and role memories (`bd remember`, a separate store). It NEVER holds work beads — feature/plan/task/discovery beads all live in the PROJECT repo's `.beads`. `ateam audit` enforces this: it lists any global-workspace issue lacking the tracking schema and exits non-zero. Run it in Phase 0 and at teardown; it must always be clean.

## Description schema (line-oriented; the compaction hook greps `worktree:`)

    problem: <one-line problem statement>
    repo: <abs path to main repo>
    worktree: <abs path of the checkout the DRI owns>
    branch: <branch name>
    team: <team slug>
    mode: interactive|bg
    epic: <root epic bead id in the project repo, e.g. agent-teams-x6ce>
    standby: true    # OPTIONAL — present only when the initiative was dispatched --standby

There is NO `phase:` or `status:` field. The DRI maintains no phase; execution-state (IN-PROGRESS / REVIEWABLE / NEEDS-DECISION) is computed by the dashboard from gate labels and the live session's run/park state.

## Standby field (frozen contract — `--standby`)

An initiative dispatched with `--standby` is registered and its DRI launched, but the DRI **parks on startup waiting for human direction** instead of entering clarify/plan/implement. This keeps `ateam dispatch` / `/dri-dispatch` mechanical (no judgment, no investigation) while the standby *behavior* lives in `/dri`, which already knows how to park on human gates.

The field is a single line in the initiative description, written by `ateam dispatch --standby` (placed directly after the `mode:` line):

    standby: true

Rules (both the writer and every reader copy these verbatim):

- **Off state:** the line is simply **absent**. Never write `standby: false`.
- **Release marker:** once the human gives direction, the DRI records a note containing the line `standby: released`. This is the durable release signal — it survives resume and is independent of gate-label state.
- **Reader rule (`/dri` startup):** standby is *active* iff the description contains `standby: true` **AND** does not contain `standby: released`. When active, `/dri` parks with a QUESTION gate worded **"Standby — waiting for direction"** (see the gate protocol) instead of entering Phase 2. When the human provides direction, the DRI appends the `standby: released` note, clears the gate, and proceeds normally through clarify/plan/execute.
- Rationale for the explicit release marker: a standby DRI parks *before* creating any work beads, so "are there beads yet" cannot distinguish a still-waiting initiative from one that has received direction. The append-only `standby: released` note disambiguates unambiguously across any number of resumes.

**Epic invariant (at-e3m):** every new initiative has a root epic bead in the project repo. `ateam register` auto-creates this epic (via `bd -C <repo> create --type=epic`) and writes its id as the `epic:` line in the description. All work beads filed by the DRI, planner, and role agents must use `--parent <epicId>` so they live under the initiative's subtree. Multiple ring/phase epics are permitted — they are children of the root epic. Bare (unparented) work beads are acceptable only in trivial one-off cases. The `epic:` field is also written to initiative notes by the DRI ensure-epic step when absent (legacy initiatives). The dashboard reads `epic:` to filter the drill-in work-bead list to just this initiative's subtree.

## Commands

Write the body to a temp file first (avoids the newline-# safety prompt), then:

    ateam register --title "<problem statement, short>" --file /tmp/initiative-body.txt

This prints the new issue id on stdout.

- Resume match (open): `ateam resume-match "$PWD"` — prints the id of the OPEN initiative whose description contains an exact `worktree: <path>` line, or nothing on no match. Exact-line matching avoids prefix collisions (e.g. `/a/b` matching `worktree: /a/b/c`).
- Resume match (closed): `ateam resume-match-closed "$PWD"` — same match over CLOSED initiatives (most-recently-created first). The no-parameter /dri flow calls this when there is no open match, so a delivered/closed initiative in the cwd is surfaced to the human (resume vs. start new) instead of silently ignored.

  Note: `bd search "<text>"` does NOT search description body content — it only matches titles. Do not use it as a fallback.

- Phase changes and session starts: `ateam note <id> --file <file>`.
- On delivery (PR opened): status note `delivered` with the PR URL, leave the initiative **OPEN**, AND record the structured `pr:` field (see SKILL.md Phase 5 — required for pr-shepherd routing). A PR that is merely opened is not done — the initiative stays open in an `awaiting-merge` state so a future no-parameter /dri can resume it.
- Close: ONLY when the PR is merged or a human explicitly closes the initiative — `ateam close <id> --reason "merged: <PR URL>"` (or the human's reason). Never close on PR-open alone.
- Reopen: `ateam reopen <id>` — when the human chooses to resume a closed (delivered) initiative surfaced by `resume-match-closed`.

Project-repo beads may also be human-flagged for local detail, but the GLOBAL initiative flag is the canonical "waiting on a human" signal — always raise gates there.
