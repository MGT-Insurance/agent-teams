---
name: dri-dispatch
description: Create a NEW agent-teams initiative and hand it to a background DRI. Use when asked to "start a new initiative in the background", "kick off / dispatch a background DRI", "spin off separable work as its own initiative", or when invoked as /dri-dispatch <problem statement>. Creates a dedicated worktree, registers the initiative, and launches a hands-off background DRI session that drives it to a PR. Does NOT make THIS session the DRI — use /dri for that.
---

You dispatch a new initiative; you do not become its DRI. This skill sets up a fresh, isolated checkout, registers the initiative in the global workspace, and launches a **background** `/dri` session that owns it end-to-end. The current session stays free.

Use this when:

- A human wants to kick off an initiative without tying up this session.
- A DRI (or any session) surfaces **separable** work — a discovery bead that is really its own feature, infra/tooling work, anything that would balloon the current scope. Do NOT absorb it; dispatch it here. The current initiative stays focused; the new one gets its own DRI, checkout, team, and PR.

For *becoming* the DRI in this session, use `/agent-teams:dri` instead.

## The `ateam` tool

Your plugin directory is injected at load time. The workspace tool is at `<plugin-root>/scripts/ateam` (from a skill at `plugins/agent-teams/skills/dri-dispatch/SKILL.md`, that's two levels up from the skill dir, then `scripts/ateam`). Resolve this to its absolute path once and write that LITERAL absolute path wherever this document shows `<ateam>` below. Use the literal path each time — do not assign it to a shell variable.

**🚨 CARDINAL RULE.** The GLOBAL workspace (reached ONLY via `<ateam>`) holds ONLY initiative-tracking beads and role memories. Registering an initiative is the ONE write you make there, and `<ateam> register` is the only sanctioned way to do it — NEVER `bd -C <global> create`. All work beads (the planner's decomposition, feature/task/discovery beads) live in the PROJECT repo and are created by the background DRI and its team, not here.

## Steps

### 1. Preflight

- Resolve the absolute path to `<ateam>`. Verify it works: `<ateam> ws` prints the workspace path. If it fails, tell the human to run `/setup-agent-teams` and stop.
- Run `<ateam> audit`. It must report clean before you add anything.

### 2. Scope the initiative

- **Problem statement.** Take it from the invocation. If none was given, ask the human for one — this is the single load-bearing input.
- **Figure out the target repo yourself; involve the human only if you cannot.** The initiative may target a repo OTHER than the one this dispatcher session is sitting in, so do not blindly assume cwd. Identify the target directory the human means (explicit in the invocation if they named one, otherwise the current directory), then resolve the actual repo root FROM that directory — `git -C <target-dir> rev-parse --show-toplevel`, and if it is a worktree, `git -C <target-dir> rev-parse --git-common-dir` for the shared repo. (The target may be a subdirectory or a worktree, not the repo root — resolve, don't assume.) If that yields a single unambiguous repo, proceed silently. Ask the human about the repo location ONLY when you are not confident: cwd is not inside any repo, the problem clearly refers to a different project you cannot locate, or more than one repo plausibly fits.
- **Base branch.** The repo's integration branch (default `main`); if it is ambiguous or the human implied otherwise, confirm the base before creating the worktree — a wrong base is expensive to unwind.
- **Slug.** Derive a short kebab-case slug from the problem (e.g. `add-undo-stack`). This names the worktree, the branch, and the background session.

### 3. Create the worktree (Option A: the DRI is born inside it)

The DRI session's cwd is fixed at launch and cannot be reliably relocated mid-session, so it must START in its own checkout. Create that checkout now, under the canonical worktree root so it is already inside the pre-approved `additionalDirectories` (set by `/setup-agent-teams`):

```bash
git -C <project-repo> worktree add <ws>-worktrees/<slug> -b <slug> <base-branch>
```

where `<ws>` is the path printed by `<ateam> ws`. (A worktree created from anywhere — including from inside another worktree — is a peer attached to the shared repo, not a child; `.beads/` discovery is unaffected. So a DRI can later spawn its own implementer worktrees from here with no nesting.)

### 4. Register the initiative (mode: bg)

Write the description body to a temp file (avoids the newline-`#` safety prompt), using the exact line-oriented schema, then register:

```
problem: <one-line problem statement>
repo: <abs path to main repo>
worktree: <abs path of the worktree created in step 3>
branch: <slug>
team: <repo>-<slug> slugified
mode: bg
```

```bash
<ateam> register --title "<problem statement, short>" --file /tmp/initiative-body.txt
```

`register` prints the new initiative **id** — capture it. You pass it to the DRI in Step 5 (that is how the DRI knows which initiative it owns) and report it to the human. Set `worktree:` to the worktree's absolute path so the registry records where the DRI lives, but resume no longer depends on it matching `$PWD` exactly — the dispatched DRI resumes by id, not by path.

### 5. Dispatch the background DRI

Launch the background `/dri` into the worktree, passing the **initiative id** from Step 4. Telling the DRI exactly which initiative it owns is more robust than making it infer one from `$PWD`, and an id is not a problem statement, so it never trips `/dri`'s "open match + new problem → ask the human" guard. `ateam new-initiative` forwards its argument to `/dri` — it `cd`s into the worktree and launches `claude --bg … "/dri <initiative-id>"`:

```bash
<ateam> new-initiative <ws>-worktrees/<slug> <initiative-id>
```

The background DRI boots in the worktree, resumes the initiative by id, and drives it through plan → execute → PR. It runs under `--permission-mode bypassPermissions` for hands-off operation.

### 6. Report and hand off

Tell the human:

- The initiative id and the worktree path.
- The background session name (the slug) and the id `new-initiative` printed.
- How to watch and control it:

```bash
claude agents          # list background sessions
claude logs <id>       # recent output without attaching
claude attach <id>     # open it in this terminal
claude stop <id>       # stop it
```

Any human gate the background DRI parks on surfaces through `<ateam> human-list` and the `/initiatives` dashboard — so a needed decision is discoverable without tailing logs.

## Permissions

A backgrounded DRI has **no human attached to answer prompts**, so it runs with `--permission-mode bypassPermissions` (what `new-initiative` sets). This requires a **one-time interactive acceptance** of bypass mode on the machine first. A bypass-mode DRI edits a real repo unattended — only dispatch one for well-scoped work, and confirm with the human first when the initiative touches sensitive tooling or infrastructure.

Reference: https://code.claude.com/docs/en/agent-view
