# bd Mechanics Verification

**Date verified:** 2026-06-11  
**bd version:** 1.0.4 (dev)  
**Verified by:** Task 2 agent against throwaway workspace under /tmp

---

## 1. Non-interactive workspace init

`bd init` requires CWD to be the target directory. The `-C` flag does NOT work for
`init` — it errors with "no beads project found". Use a subshell to avoid the
`cd` + redirection prompt restriction:

```bash
# Create git repo
git -C /tmp/at-verify init

# Initialize beads — must use subshell (no output redirection in compound)
(cd /tmp/at-verify && bd init --prefix at --non-interactive)
```

Sample output:
```
  Repository ID: f73efeb4
  Clone ID: 784ccb8fcc264920
  Hooks installed to: .beads/hooks/
  ✓ Created AGENTS.md with agent instructions
  ...
  ✓ bd initialized successfully!

  Backend: dolt
  Mode: embedded
  Database: at
  Issue prefix: at
  Issues will be named: at-<hash> (e.g., at-a3f2dd)
```

**Key notes:**
- `--non-interactive` (or `BD_NON_INTERACTIVE=1`) skips all prompts; role defaults to maintainer
- Storage uses `.beads/embeddeddolt/` (not a `.db` file)
- After init, `-C /path` works for all subsequent commands
- Init will warn "No Dolt remote configured" if no git origin exists yet

---

## 2. Memory write/read + prefix-matching behavior

After init, `-C` works normally:

```bash
bd -C /tmp/at-verify remember --key "planner:test-slug" "test insight. WHY: test. HOW TO APPLY: test."
# Output: Remembered [planner:test-slug]: test insight. WHY: test. HOW TO APPLY: test.

bd -C /tmp/at-verify memories planner
# Output:
# Memories matching "planner":
#
#   planner:test-slug
#     test insight. WHY: test. HOW TO APPLY: test.

bd -C /tmp/at-verify memories tester
# Output: No memories matching "tester"
```

**Prefix-matching behavior:** `bd memories <keyword>` matches on the **key** field using
substring matching (not full-text content search). A key of `planner:test-slug` matches
the query `planner` but NOT `tester`. The match is against the full key string.

---

## 3. bd human flag/list/respond/dismiss + question attachment

### Flagging an issue as human-needed

```bash
# Create issue
bd -C /tmp/at-verify create --title="Test initiative" --type=task --priority=2 \
  --body-file=/tmp/body.md

# Flag with human label (two equivalent forms):
bd -C /tmp/at-verify tag at-oph human
# OR
bd -C /tmp/at-verify label add at-oph human

# List human-needed issues
bd -C /tmp/at-verify human list
# Output:
# Human-needed beads (1 found)
#
#   at-oph Test initiative
#     Priority: P2

# Stats
bd -C /tmp/at-verify human stats
# Output:
# Human Beads Stats
#   Total:      1
#   Pending:    1
#   Responded:  0
#   Dismissed:  0
```

### BROKEN: bd human respond / bd human dismiss

**`bd human respond <id> -r "text"` and `bd human dismiss <id>` both fail** with
`"storage is nil"` in bd 1.0.4. This is a confirmed bug — it fails regardless of
whether `-C`, `--db`, or subshell-cd is used:

```bash
bd -C /tmp/at-verify human respond at-oph -r "test"
# Error: resolving issue ID at-oph: cannot resolve issue ID "at-oph": storage is nil
```

The error affects ALL project contexts (not just test workspaces — also fails in the
main midgard project).

### Workaround for respond

```bash
# Add response as a comment
bd -C /tmp/at-verify comment at-oph "RESPONSE: Yes, proceed with option A."
# Remove human label
bd -C /tmp/at-verify label remove at-oph human
# Close the issue
bd -C /tmp/at-verify close at-oph
# Verify
bd -C /tmp/at-verify human list   # → "No human-needed beads found."
```

### Workaround for dismiss

```bash
bd -C /tmp/at-verify close at-vn7
bd -C /tmp/at-verify label remove at-vn7 human
bd -C /tmp/at-verify human list   # → "No human-needed beads found."
```

### Question attachment method

`bd note <id> --file=<file>` appends to the NOTES section of the issue. This is the
preferred method for attaching question text (visible in `bd show`):

```bash
printf 'QUESTION: Should we proceed with option A?\n' > /tmp/question.txt
bd -C /tmp/at-verify note at-oph --file=/tmp/question.txt
# Output: ✓ Note added to at-oph — Test initiative
```

Visible in `bd show at-oph`:
```
NOTES
QUESTION: Should we proceed with option A?
```

`bd comment <id> --file=<file>` also works and appears in the COMMENTS section
(visible in `bd show`). Comments are appropriate for the response; notes for the
question. Both `--file=<path>` forms bypass the `\n#` prompt restriction.

---

## 4. JSON matching strategy

### list --json shape

`bd list --status=open --json` returns an array with the full `description` field:

```json
[
  {
    "id": "at-oph",
    "title": "Test initiative",
    "description": "problem: test\nrepo: /tmp/at-verify\nworktree: /tmp/at-verify\nbranch: t\nteam: t\nmode: interactive\n",
    "notes": "question: should we proceed?\n",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "owner": "erlloyd@gmail.com",
    "created_at": "2026-06-11T19:44:49Z",
    "created_by": "Eric Lloyd",
    "updated_at": "2026-06-11T19:45:11Z",
    "labels": ["human"],
    "dependency_count": 0,
    "dependent_count": 0,
    "comment_count": 1
  }
]
```

### jq expression for registry matching

To find the id of the issue whose description contains a given worktree path:

```bash
bd -C /path/to/ws list --status=open --json \
  | jq -r '.[] | select(.description | contains("worktree: /path/to/worktree")) | .id'
```

This returns the issue id (e.g., `at-oph`) or nothing if not found.

**Important:** `bd search "<text>"` does NOT search description body content — it only
matches titles. Verified: `bd search "worktree"` returns `[]` even when descriptions
contain the word. Use `list --json | jq` as the primary matching strategy, not `search`.

---

## 5. Sync push/pull + new-machine clone bootstrap

### First push (after initial setup)

Prerequisites: git remote must have at least one commit before `bd dolt push`.

```bash
# Add git remote
git -C /tmp/at-verify remote add origin /path/to/remote.git

# Commit beads files to git
git -C /tmp/at-verify add -A
git -C /tmp/at-verify commit -m "initial commit"
git -C /tmp/at-verify push origin main

# Add Dolt remote (same URL as git remote)
bd -C /tmp/at-verify dolt remote add origin /path/to/remote.git

# Push Dolt data
bd -C /tmp/at-verify dolt push
# Output: Pushing to Dolt remote... Push complete.

# Verify: refs/dolt/data ref on remote
git -C /tmp/at-verify ls-remote origin
# refs/dolt/data    74319300eba3...
# refs/heads/main   e642b7bb7ad0...
```

**Dolt remote URL formats supported:**
- Local path: `/path/to/remote.git`
- GitHub: `git+ssh://git@github.com/org/repo.git`
- DoltHub: `https://doltremoteapi.dolthub.com/org/repo`
- Azure Blob: `az://account.blob.core.windows.net/container/path`

### New-machine clone bootstrap

**Recommended — auto-detect from git origin (simplest):**

```bash
git clone <remote-url> <dir>
(cd <dir> && bd init --prefix <prefix> --non-interactive)
```

`bd init` detects the git origin and auto-bootstraps from `refs/dolt/data`. No
`--remote` flag needed when the repo was cloned from the same remote used for
`bd dolt push`.

**Explicit remote — same behavior:**

```bash
git clone <remote-url> <dir>
(cd <dir> && bd init --prefix <prefix> --non-interactive --remote <remote-url>)
```

**Verification:**

```bash
bd -C <dir> memories planner
# → shows memories from the source workspace

bd -C <dir> list --status=open
# → shows issues from the source workspace
```

**Important:** `bd dolt pull` alone (without `bd init`) will create `.beads/embeddeddolt/`
but pulls from whatever remote is configured in config.yaml (which may be wrong if
config.yaml was committed with a different project's remote). Always use `bd init`
for new-machine bootstrap to get a clean, correct setup.

---

## Live verifications (Task 13) — VERIFIED 2026-06-11

**V1 — slash command in `claude --bg` + workspace access prompt behavior.** `claude --bg "/initiatives"` invokes the skill (confirmed: the spawned session ran `agent-teams:initiatives` and called the workspace). Critical finding on prompts: a workspace command written as `bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" …` triggers Claude Code's **unsilenceable "Contains expansion"** dialog and the `--bg` session freezes on it. Routing through the fixed launcher `~/.agent-teams/bin/ateam` instead produces only the **normal, silenceable** "command requires approval" prompt (with a "don't ask again" option) — and with `Bash(~/.agent-teams/bin/ateam:*)` in `permissions.allow` (installed by `/setup-agent-teams`) the `--bg` run executes **end-to-end with zero prompts**. This is why D14 exists: all workspace access goes through the launcher, never the `${…}` form.

**V2 — AskUserQuestion from a detached `--bg` session.** A backgrounded session told to ask an AskUserQuestion renders the question + options (verified in the session log) and ends its turn awaiting input; the question text is recoverable from the log / on `claude attach`. Implication for the gate protocol: do NOT rely on AskUserQuestion alone in `--bg` mode — the gate protocol already records the question to the initiative AND flags it via `at gate` BEFORE asking, so the parked gate is durably visible in the registry (`at human-list` / `/initiatives`) regardless of how the widget replays on attach. Belt-and-suspenders by design.

**V5 — SessionStart `matcher: "compact"`** is the correct, documented hook for post-compaction context re-injection (SessionStart stdout becomes context; `PostCompact` is side-effects-only). Confirmed against current Claude Code hooks docs. The hook's `hooks.json` as-built is correct.

---

## Summary of surprises

1. **`bd -C` does not work for `bd init`** — flag requires an existing beads project. Use subshell pattern: `(cd <dir> && bd init ...)`.
2. **`bd human respond` and `bd human dismiss` are broken** (storage is nil) in bd 1.0.4. Use the comment + label remove + close workaround.
3. **`bd search` does not search description body** — only title. Use `list --json | jq` for description content matching.
4. **Dolt remote ≠ git remote** — they are configured separately (`bd dolt remote add` vs `git remote add`), but can reference the same URL.
5. **Git remote must have an initial commit** before `bd dolt push` — the bare repo must be non-empty.
6. **Auto-bootstrap works**: `bd init` on a cloned repo auto-detects the git origin and pulls Dolt data — `--remote` flag is optional when cloned from the same URL.
