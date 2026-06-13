---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads is installed, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, installs the ateam launcher, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

If you set AGENT_TEAMS_HOME to a custom path, use that literal path in place of `~/.agent-teams` below.

## 1. Verify beads

`bd --version`. If missing: STOP and tell the human — agent-teams hard-requires beads (https://github.com/gastownhall/beads). Do not improvise a fallback.

## 2. Resolve the workspace location

The default workspace path is `~/.agent-teams`. If the human wants a non-default location, have them set `AGENT_TEAMS_HOME` in the `env` block of `~/.claude/settings.json` (applies to all future sessions), and use that literal path in place of `~/.agent-teams` in every command below.

## 3. Create or clone the workspace

Ask the human: do you already have an agent-teams memory remote (e.g. a private `agent-teams-memory` repo from another machine)?

### Existing remote → clone

```bash
git clone <remote-url> ~/.agent-teams
(cd ~/.agent-teams && bd init --prefix at --non-interactive)
```

`bd init` detects the git origin and auto-bootstraps from `refs/dolt/data` — no separate `bd dolt pull` needed (and `bd dolt pull` alone is a footgun: it may pull from a wrong configured remote). Verify knowledge arrived:

```bash
bd -C ~/.agent-teams memories dri
```

### Fresh → init

**Step 1 — create the git repo and initialize beads** (`bd -C` does not work for `init`; a subshell is required):

```bash
mkdir -p ~/.agent-teams
git -C ~/.agent-teams init
(cd ~/.agent-teams && bd init --prefix at --non-interactive)
```

**Step 2 — have the human create a private remote**, e.g.:

```
gh repo create <user>/agent-teams-memory --private
```

**Step 3 — wire up the git remote and push the initial commit** (the remote must have at least one commit before `bd dolt push`):

```bash
git -C ~/.agent-teams remote add origin <url>
git -C ~/.agent-teams add -A
git -C ~/.agent-teams commit -m "init agent-teams workspace"
git -C ~/.agent-teams branch -M main
git -C ~/.agent-teams push -u origin main
```

**Step 4 — add the Dolt remote** (separate from the git remote, but can use the same URL) **and push the Dolt data**:

```bash
bd -C ~/.agent-teams dolt remote add origin <url>
bd -C ~/.agent-teams dolt push
```

## 4. Enable team orchestration (REQUIRED)

The DRI's `TeamCreate` / team-join model requires the `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` env var to be set. Without it, `TeamCreate` silently no-ops and the DRI cannot orchestrate a background team at Phase 4.

Tell the human to add the following to the `env` block of `~/.claude/settings.json`:

```json
"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
```

Example — the `env` block in `~/.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1",
    "AGENT_TEAMS_HOME": "~/.agent-teams"
  }
}
```

This setting applies to all future sessions. It is required regardless of whether you intend to run the DRI interactively or headlessly.

## 5. Install `ateam` on PATH (requires Go 1.26+)

**Go 1.26 or later is REQUIRED** to install the binary. `go install` will fail without it. (Prebuilt binaries — for machines without Go — are a planned follow-up: bead agent-teams-yfm.)

Install from the repo root (the directory containing `go.mod`):

```bash
cd <plugin-repo-root>
go install ./cmd/ateam
```

where `<plugin-repo-root>` is the root of the agent-teams git repo. For example, if the plugin repo is at `/Users/you/Code/agent-teams`, `go.mod` lives there and you `cd` there before running `go install`.

This lands the binary in your Go bin directory (`go env GOBIN`, or `$(go env GOPATH)/bin` when GOBIN is empty).

**Verify it is on PATH:**

```bash
command -v ateam
```

- If it resolves (prints a path) → done; `ateam` is on PATH.
- If not found → print the install directory:
  ```bash
  go env GOBIN   # if non-empty, that is the install dir
  go env GOPATH  # if GOBIN is empty, the dir is $(go env GOPATH)/bin
  ```
  Add that directory to your `PATH` (e.g. in `~/.zshrc` or `~/.bashrc`), then re-run `command -v ateam` to confirm.

## 6. Provision the interactive-DRI permission profile (OPTIONAL — interactive only)

This whole step is **only for interactive DRI sessions** — the human-facing session
that runs `/dri` in a terminal. Backgrounded DRIs and Phase-4 teammates run with
`bypassPermissions` and never prompt, so they need none of this.

Why it matters: a DRI session is **git-heavy** (it owns integration — `git worktree
add`, `git merge`, `git push`, branch ops) and does dozens of git calls per run. The
teammates are silent under bypass, so every permission prompt the human sees comes
from the DRI session's own Bash calls. Without a permission profile, the human is
prompted on essentially every git command. The three entries below quiet that:
the `ateam`/`bd` allowlist, a **scoped git allowlist**, and a **canonical worktree
root** in `additionalDirectories`.

### 6a. Allowlist `ateam`

Allowlist the bare `ateam` command so workspace operations do not prompt.

Tell the human to add the following entry to the `permissions.allow` array in `~/.claude/settings.json`:

```
"Bash(ateam:*)"
```

This single entry covers all `ateam` subcommands regardless of where the binary lives on PATH — no path to resolve, no re-allowlisting after reinstall.

Verify it works:

```bash
ateam ws
```

Expected: prints the workspace path (e.g. `/Users/you/.agent-teams`).

### 6b. Allowlist git (scoped — standard tool, not a wrapper)

The DRI calls **standard `git`** directly — that is deliberate; git is not wrapped in
a bespoke CLI just to dodge prompts. To keep it quiet, add a **scoped** set of git
verbs to the `permissions.allow` array in `~/.claude/settings.json`:

```
"Bash(git status:*)",
"Bash(git log:*)",
"Bash(git diff:*)",
"Bash(git show:*)",
"Bash(git add:*)",
"Bash(git commit:*)",
"Bash(git push:*)",
"Bash(git pull:*)",
"Bash(git fetch:*)",
"Bash(git branch:*)",
"Bash(git checkout:*)",
"Bash(git switch:*)",
"Bash(git merge:*)",
"Bash(git worktree:*)"
```

Use this **scoped** list, NOT `Bash(git:*)`. Scoping leaves genuinely destructive
forms (`git reset --hard`, `git clean`, force-push) to still prompt the human — the
interactive DRI is the human's safety surface, so it should stay prompt-capable for
the dangerous operations while the routine integration verbs run quietly.

### 6c. Pre-approve the DRI worktree root

DRI implementer worktrees live under one canonical root (see
`skills/dri/references/execution.md`): **`<AGENT_TEAMS_HOME>-worktrees`** — e.g.
`~/.agent-teams-worktrees` by default. It is deliberately OUTSIDE the workspace and
any project repo. A worktree created at a fresh path otherwise draws a second,
file-access prompt on top of the allowlist; pre-approving the root removes it.

Add the **absolute** path (no `~`) to the `permissions.additionalDirectories` array
in `~/.claude/settings.json`:

```json
"permissions": {
  "additionalDirectories": ["/Users/you/.agent-teams-worktrees"]
}
```

With 6a–6c in place, an interactive DRI runs its integration git silently and only
prompts the human for genuinely destructive operations.

## 7. Smoke test

Run on BOTH paths (clone or fresh) after step 6 completes.

1. Write a test memory to a temp file and record it. Use the Write tool to create `/tmp/ateam-smoke.txt` with content:
   `setup smoke test. WHY: verify store. HOW TO APPLY: n/a.`

   Then record it:
   ```bash
   ateam learn dri setup-smoke --file /tmp/ateam-smoke.txt
   ```

2. Verify it appears:
   ```bash
   ateam learnings dri
   ```
   Expected: shows `dri:setup-smoke`.

3. Sync roundtrip:
   ```bash
   ateam sync
   ```
   Expected: push succeeds.

4. Clean up the smoke entry and push again to leave the store clean:
   ```bash
   bd -C ~/.agent-teams forget dri:setup-smoke
   ateam sync
   ```

## 8. Report

Confirm to the human: workspace path, remote URL, `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` set, the interactive-DRI permission profile (`Bash(ateam:*)` allowlist, scoped git allowlist, and worktree-root `additionalDirectories` — each applied or skipped), smoke-test results, and that `/dri` is ready to use.
