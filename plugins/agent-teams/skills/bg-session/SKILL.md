---
name: bg-session
description: Launch a bare background Claude session with no ateam, no beads, no worktree, and no initiative registration. Use when asked to "start a bg session", "background session", "run the dev server in the background", "stand up an idle session", "spin up a background Claude", or when invoked as /bg-session [prompt] [dir]. A deliberate escape hatch — for real feature work that should become a tracked initiative, use /dri-dispatch instead.
---

You launch a single bare background Claude session and do nothing else — no `ateam`, no `bd create`, no worktree, no initiative registration. This is the lightest-weight dispatch skill in the plugin: an escape hatch for work that doesn't need any of that machinery — running a dev server in the background, or standing up an idle session the human can send ad-hoc instructions to later.

For work that should become a tracked initiative with its own worktree and background DRI, use `/dri-dispatch` instead.

---

**ABSOLUTE CONSTRAINT — this skill touches nothing but the `claude` CLI.**
Do NOT call `ateam` (no `ateam dispatch`, no `ateam register`), do NOT `bd create` anything, and do NOT create a git worktree. If you find yourself reaching for any of those, stop — you're doing `/dri-dispatch`'s job, not this one.

## Steps

### 1. Parse the invocation

Two inputs, both optional and free-form:

- **Prompt** — what the background session should do. If the human gave one, use it verbatim.
- **Target directory** — where the session should run. If the human named one, resolve it to an absolute path. Otherwise default to the current working directory.

If no prompt was given, default it to a standby prompt, e.g.:

```
You are a background session with no assigned task yet. Stand by and wait for instructions — take no action until you receive one. When a message arrives, decide directly whether to act on it yourself or promote it to a tracked initiative with /dri-dispatch.
```

### 2. Choose a session name

Derive `<name>` from the target directory's basename (e.g. `/Users/erlloyd/code/cardtable2` → `cardtable2`). If the basename is unusable (`.`, empty, or already in use per `claude agents`), fall back to a short slug from the prompt's first few words, or a fixed label like `bg-session`. It only needs to be a legible label for `claude agents`/`claude logs`/etc — no uniqueness scheme beyond avoiding an obvious collision.

### 3. Launch

Run with the target directory as the session's working directory (subshell `cd` keeps your own cwd unchanged):

```bash
(cd "<target-dir>" && claude --bg --permission-mode bypassPermissions -n "<name>" "<prompt>")
```

Do NOT add the DRI-specific `--append-system-prompt` or auto-compact env vars that `ateam dispatch` sets for background DRIs — this is a plain, unadorned session with no special system prompt.

### 4. Report and hand off

Tell the human the session name/id and the target directory, then give the standard control block:

```bash
claude agents                   # list background sessions
claude logs <id>                # recent output without attaching
claude attach <id>              # open it in this terminal
claude stop <id>                # stop it
```

Note: if the bare session later turns up real work worth tracking, the human — or the session itself, from inside it — can run `/dri-dispatch` to promote it to a full tracked initiative.

## Key constraints

- No `ateam` call, no `bd create`, no worktree creation — anywhere in this skill.
- No DRI-specific system-prompt or env additions — the launched session is a plain `claude --bg` session, nothing more.
- If the work is already known to need tracking (a real feature, needs a PR), use `/dri-dispatch` instead of this skill.
