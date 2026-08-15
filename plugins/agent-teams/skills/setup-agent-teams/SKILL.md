---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads and optional Codex compatibility, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, installs the ateam launcher, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
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

The DRI's team-orchestration model (team-joined background spawns + `SendMessage` peer messaging) requires the `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` env var to be set. Without it, teammates cannot join a team or message each other, and the DRI cannot orchestrate a background team at Phase 4. There is no separate team-creation step — with the env var set, the team forms automatically when the first teammate is spawned (the pre-v2.1.178 `TeamCreate`/`TeamDelete` tools no longer exist).

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

## 5. Install `ateam` onto PATH

`ateam` ships as prebuilt per-platform binaries inside the plugin's `bin/` directory. Setup owns putting bare `ateam` on PATH — it creates a symlink in `~/.local/bin` (which is on PATH on standard macOS/Linux user setups). This is idempotent: re-running setup is always safe.

A SessionStart hook (`ensure-ateam-link.sh`) now keeps this symlink pointed at the active plugin installation on every session, so it self-heals across plugin updates. Step 5 remains useful as a first-run convenience, not something you need to re-run after an upgrade.

### 5a. Resolve the installed wrapper path

Work through the following resolution order and stop at the first path that exists:

**Option A — harness auto-add already resolved it:**

```bash
command -v ateam
```

If this prints a path (exit 0), that is the wrapper. Use it directly.

**Option B — marketplace cache install:**

```bash
python3 -c "
import json, os
data = json.load(open(os.path.expanduser('~/.claude/plugins/installed_plugins.json')))
plugins = data.get('plugins', {})
for key, entries in plugins.items():
    if key.startswith('agent-teams@'):
        for e in entries:
            p = e.get('installPath', '')
            candidate = p + '/bin/ateam'
            if os.path.isfile(candidate):
                print(candidate)
                break
"
```

If this prints a path, that is the wrapper.

**Option C — local directory-marketplace checkout:**

```bash
python3 -c "
import json, os
data = json.load(open(os.path.expanduser('~/.claude/plugins/known_marketplaces.json')))
for mp_name, mp in data.items():
    src = mp.get('source', {})
    if src.get('source') == 'directory' and 'agent-teams' in mp_name:
        candidate = src['path'] + '/plugins/agent-teams/bin/ateam'
        if os.path.isfile(candidate):
            print(candidate)
            break
"
```

If this prints a path, that is the wrapper.

If none of the three options resolves a path, STOP — the plugin is not installed. Confirm the agent-teams plugin is installed in `~/.claude/settings.json` and retry.

### 5b. Canonicalize through symlinks

The resolved candidate may itself be a symlink (e.g. Option A returns `~/.local/bin/ateam` if setup has already run). Follow symlinks to the real wrapper before installing — otherwise `ln -sf` points `~/.local/bin/ateam` at itself, creating a self-referential loop that breaks on re-run.

With `WRAPPER_PATH` set to the candidate from 5a:

```bash
# Follow symlinks to the real wrapper (POSIX; works on macOS and Linux)
while [ -h "$WRAPPER_PATH" ]; do
    t="$(readlink "$WRAPPER_PATH")"
    case "$t" in
        /*) WRAPPER_PATH="$t" ;;
        *)  WRAPPER_PATH="$(dirname "$WRAPPER_PATH")/$t" ;;
    esac
done
echo "Real wrapper: $WRAPPER_PATH"
```

Report the printed path. It must be the real `bin/ateam` inside the plugin installation, not a path under `~/.local/bin`.

### 5c. Install the symlink

With `WRAPPER_PATH` canonicalized in 5b:

```bash
mkdir -p ~/.local/bin
ln -sf "$WRAPPER_PATH" ~/.local/bin/ateam
```

`ln -sf` is force-mode: it overwrites any existing symlink and does not error on re-run. If `~/.local/bin` does not exist it is created. Report the result of `ls -la ~/.local/bin/ateam`.

### 5d. Smoke test — fail loud

```bash
ateam ws
```

Expected: prints the workspace path (e.g. `/Users/you/.agent-teams`). Exit 0.

If this fails with "command not found" or a non-zero exit:

**STOP. Do not proceed.** `~/.local/bin` is not on PATH in this shell environment. The human must add it:

```
export PATH="$HOME/.local/bin:$PATH"
```

Add that line to `~/.zshrc` (or `~/.bashrc`) so it persists, then open a new terminal and re-run `/setup-agent-teams` from step 5 onward.

If the error is "unsupported platform" rather than "command not found", the symlink resolved correctly but the plugin's `bin/` directory is missing the platform binary — file an issue against the agent-teams plugin.

### 5e. Install the global-workspace PRIME.md

Left unset, `bd prime` dumps the ENTIRE all-role memory store into every session that resolves this workspace, on every PreCompact — hundreds of KB across hundreds of memories once the store has been running a while. The fix is a `PRIME.md` override at `$AGENT_TEAMS_HOME/.beads/PRIME.md`; `ateam steward init` installs it idempotently from the plugin's bundled template. Run it now rather than waiting for someone to happen to start a Steward session first:

```bash
ateam steward init
```

Expected: prints `installed: <path>/.beads/PRIME.md` on first run (or nothing about PRIME.md if it's already installed and unchanged — self-healing is silent), followed by the Steward session directory path. Safe to re-run: it never overwrites a human-edited or unrecognized PRIME.md.

### 5f. Check optional Codex compatibility

Run the shared runtime check; do not infer compatibility from `codex --version`
or the executable path:

```bash
ateam runtime check codex --optional
```

- `compatible standalone installation` means Codex dispatch is ready.
- `absent` is informational; continue Claude setup normally.
- `incompatible installation` is a warning; continue Claude setup, but tell the
  human Codex initiatives require reinstalling Codex with the official
  standalone installer.

If the human intends to use Codex now, rerun without `--optional`; do not claim
Codex is ready unless `ateam runtime check codex` exits zero.

### Allowlist `ateam`

Add the following entry to the `permissions.allow` array in `~/.claude/settings.json` so workspace operations do not prompt:

```
"Bash(ateam:*)"
```

This single entry covers all `ateam` subcommands regardless of which per-platform binary is selected.

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

This single entry covers all `ateam` subcommands regardless of where the symlink target resolves — no re-allowlisting is needed after a plugin version upgrade, because the allowlist matches the bare command name, not the resolved binary path.

Step 5 already verified `ateam ws` resolves. Confirm it still works:

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

## 7. Playwright CLI (tester live-UI verification)

There is no MCP server for this — the tester drives and observes the real browser via `npx @playwright/cli <command>` (plain Bash, no MCP wiring needed, so it works the same in interactive and `claude --bg` headless sessions). The CLI ships its own agent skill (its path is printed at the top of `npx @playwright/cli --help`); consult that plus `--help` for the full command surface rather than looking for one here.

**Prerequisite:** `npx` (Node.js) must be on PATH. The first browser use will trigger a one-time Playwright browser download. To pre-install and avoid that delay:

```bash
npx playwright install chromium
```

No credentials or auth required.

**Smoke check:** confirm the CLI is reachable and see its command surface + shipped skill path:

```bash
npx @playwright/cli --help
```

If that prints usage output, the CLI is available and the tester can use it for live UI verification (session model: `open` a named session with `-s=<name>` before targeting it with any other command; see `plugins/agent-teams/roles/tester.md` for the two operational gotchas).

## 8. Register a repo's worktree-setup hook (OPTIONAL — one-time per repo)

When a worktree needs live env wiring — running a dev server, creds-dependent validation (e.g. socotra), a pre-commit hook that requires it — an agent runs `ateam worktree-setup <wtPath>` to provision the gitignored files the repo needs (e.g. `.vercel` link, env files). No separate `pnpm install` first: the midgard hook installs dependencies itself when the worktree lacks them, before pulling. It is invoked on-demand, not on every worktree. The verb is a no-op unless a hook is registered for the repo. Registration is optional: with no hook (or a configured script that is missing) the verb prints a harmless message and exits 0. But a registered hook that RUNS and fails now exits nonzero, so a caller can tell provisioning did not complete — install failed, the pull failed, or an env file the worktree needs still did not land.

**How it works.** The verb looks up `<AGENT_TEAMS_HOME>/worktree-hooks/<repo-key>`. The repo-key is the slugified basename of the main checkout (the source checkout behind the worktree) — the same identity dispatch uses for team names. If the file exists, its single line (trimmed) is treated as the absolute path to the repo's setup script; the verb runs `<script> <wtPath> <srcCheckout>`. No file → harmless "no hook configured" message.

**To register a repo**, write the hook file containing the absolute path to the repo's setup script:

```bash
# Example: registering the midgard repo
# repo-key = Slugify(basename of midgard main checkout), e.g. "midgard"
echo /abs/path/to/agent-teams/scripts/midgard-worktree-setup.sh \
  > ~/.agent-teams/worktree-hooks/midgard
```

The reference implementation for midgard is `scripts/midgard-worktree-setup.sh` in this (agent-teams) repo. It copies gitignored files from the source checkout and runs `vercel env pull` to restore creds-dependent tooling in the new worktree. Note: this raw script path is correct ONLY here, as the hook-registration target. Agents provisioning a worktree must always call `ateam worktree-setup <wtPath>` — never the raw script directly (a project memory naming the raw path shadows the discoverable wrapper).

## 9. Smoke test

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

5. Confirm step 5e's PRIME.md install actually took, not just that the workspace exists:
   ```bash
   ateam audit
   ```
   Expected: an `audit: bd prime clean — <n> bytes, no memory dump (budget 10240)` line (alongside the leaked-work-beads line). If instead you see `audit: FAILED — the global workspace has no installed PRIME.md`, step 5e didn't take — re-run `ateam steward init` (do not write PRIME.md by hand).

## 10. Verify memory-routing hook is active

The agent-teams plugin ships a `block-claude-memory-writes.sh` PreToolUse hook that is **automatically registered from `hooks.json`** — no install step is needed. This step verifies it is active, not re-installs it.

Run both probes and confirm the results match expectations:

**Probe A — deny (write to a Claude memory path):** Ask Claude to attempt a Write to any path under `~/.claude/projects/test-project/memory/` (e.g. `~/.claude/projects/test-project/memory/smoke.md`). The hook must intercept and deny it with a message beginning `BLOCKED: agent-teams routes persistent memory to ateam`.

**Probe B — allow (write to a normal path):** Ask Claude to attempt a Write to `/tmp/hook-verify.txt`. The hook must pass through and the write must succeed normally. Delete the file afterward.

If Probe A is not denied, the plugin hooks are not loading — confirm the plugin is installed (`~/.claude/settings.json` has the agent-teams plugin listed) and that `hooks.json` contains the `PreToolUse` block. Do NOT copy or re-register the hook manually; diagnose why plugin hook loading failed.

## 11. Report

Confirm to the human: workspace path, remote URL, `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` set, Codex compatibility (`compatible`, `absent`, or `incompatible`), the interactive-DRI permission profile (`Bash(ateam:*)` allowlist, scoped git allowlist, and worktree-root `additionalDirectories` — each applied or skipped), smoke-test results, hook-verify results, and that `/dri` is ready to use.
