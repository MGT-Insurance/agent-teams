---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads and optional Codex compatibility, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, installs the ateam launcher, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

If you set AGENT_TEAMS_HOME to a custom path, use that literal path in place of `~/.agent-teams` below.

## Shared workspace setup

Agent-teams requires `bd`; run `bd --version` and stop if it is missing. Resolve the workspace as `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` and use that literal path throughout. Do not improvise a store or use raw global-workspace `bd` as a replacement for `ateam` after setup.

Ask whether an existing private agent-teams memory remote already exists.

For an existing remote, clone it into the workspace, then initialize Beads from inside that checkout:

```bash
git clone <remote-url> <workspace>
(cd <workspace> && bd init --prefix at --non-interactive)
bd -C <workspace> memories dri
```

`bd init` detects the Git origin and bootstraps `refs/dolt/data`; do not run a separate `bd dolt pull`, which may use a stale configured remote.

For a fresh workspace, create a Git repository, initialize Beads, have the human create a private remote, make and push the initial Git commit, then configure and push the separate Dolt remote:

```bash
mkdir -p <workspace>
git -C <workspace> init
(cd <workspace> && bd init --prefix at --non-interactive)
git -C <workspace> remote add origin <url>
git -C <workspace> add -A
git -C <workspace> commit -m "init agent-teams workspace"
git -C <workspace> branch -M main
git -C <workspace> push -u origin main
bd -C <workspace> dolt remote add origin <url>
bd -C <workspace> dolt push
```

The Git remote carries repository files; the Dolt remote carries Beads data under its separate ref. Normal cross-machine synchronization uses `ateam sync` after the `ateam` wrapper is installed.

## 4. Enable team orchestration (REQUIRED)

Team-joined background spawns and `SendMessage` require `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`. Add it to the `env` block of `~/.claude/settings.json` for every future interactive or headless session:

```json
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1",
    "AGENT_TEAMS_HOME": "~/.agent-teams"
  }
}
```

## 5. Install `ateam` onto PATH

`ateam` ships in the plugin's `bin/`. Install an idempotent `~/.local/bin/ateam` symlink; the `ensure-ateam-link.sh` SessionStart hook repairs it after upgrades.

### 5a. Resolve the installed wrapper path

Work through the following resolution order and stop at the first path that exists:

**Option A — harness auto-add already resolved it:**

```bash
command -v ateam
```

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

The printed path must be the real plugin `bin/ateam`, not `~/.local/bin`.

### 5c. Install the symlink

With `WRAPPER_PATH` canonicalized in 5b:

```bash
mkdir -p ~/.local/bin
ln -sf "$WRAPPER_PATH" ~/.local/bin/ateam
```

Report `ls -la ~/.local/bin/ateam`.

### 5d. Smoke test — fail loud

```bash
ateam ws
```

Expected: workspace path and exit 0. On command-not-found/nonzero, STOP; add:

```
export PATH="$HOME/.local/bin:$PATH"
```

Persist it in the shell rc, open a new terminal, and retry step 5. For "unsupported platform", file a plugin issue for the missing platform binary.

### 5e. Install the global-workspace PRIME.md

`ateam steward init` idempotently installs the bundled `$AGENT_TEAMS_HOME/.beads/PRIME.md`, preventing `bd prime` from dumping all-role memory into sessions:

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

## 6. Provision the interactive-DRI permission profile (OPTIONAL — interactive only)

This is only for the human-facing interactive `/dri`; background DRIs and teammates use `bypassPermissions`. Configure the `ateam` allowlist, scoped routine Git verbs, and canonical worktree root below.

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
