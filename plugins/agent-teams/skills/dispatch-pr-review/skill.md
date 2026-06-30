---
name: dispatch-pr-review
description: "Dispatch a background PR review session for a GitHub pull request. Use when invoked as /dispatch-pr-review <PR>, where <PR> is a full URL (https://github.com/owner/repo/pull/123), short form (owner/repo#123), or bare number (#123 or 123, inferring repo from cwd). Parses the reference, registers a review initiative via ateam dispatch, and launches a background /agent-teams:review-pr session. Does NOT review the PR itself."
---

You dispatch a background review session for a single PR; you do not perform the review yourself. This skill parses the PR reference the human provides, writes structured metadata, and calls `ateam dispatch` with `--launch-prompt "/agent-teams:review-pr {id}" --skip-epic`. The background session runs the `review-pr` skill end-to-end — checkout, diff, reviewer subagent, inline GitHub comments, close.

Use this when:

- A human wants to kick off a PR review without tying up this session.
- A dispatcher wants to send a review to a background session from the terminal.

For webhook-triggered reviews (CI/GitHub Actions), `ateam route-pr-event` handles the same flow programmatically — this skill is the human-facing equivalent.

---

**THIS SESSION IS A HAND-OFF, NOT A REVIEW.**

**ABSOLUTE CONSTRAINT — NEVER review the PR yourself.**
The dispatcher MUST NOT read the diff, evaluate code quality, form opinions about the changes, or do any review work. That is the background `review-pr` session's job. If you find yourself reading the PR diff or reasoning about the code, STOP immediately.

**ABSOLUTE CONSTRAINT — ALWAYS launch a background review. This is not optional.**
Every invocation of this skill MUST end by launching a background session via `ateam dispatch`. The only stopping points before dispatch are: (1) `ateam` is not on PATH — tell the human to run `/setup-agent-teams` and stop; (2) no PR reference was provided — ask the human for one, then dispatch immediately once received; (3) the PR reference cannot be parsed into owner/repo/number — report the parsing failure and stop. Once those conditions are resolved, dispatch unconditionally.

Your job is: preflight, parse the PR reference, dispatch. Nothing more.

---

## The `ateam` tool

`ateam` is on PATH — it ships as a prebuilt binary in the plugin's `bin/` (auto-added to PATH; installed/verified by `/setup-agent-teams`). Call it as bare `ateam` everywhere this document shows `ateam`. One allowlist entry covers all subcommands: `Bash(ateam:*)`.

## Steps

### 1. Preflight

- Verify `ateam` is on PATH: run `ateam ws`. If it errors or is not found, tell the human to run `/setup-agent-teams` and stop.
- Run `ateam audit`. It must report clean before you add anything.

### 2. Parse the PR reference

The human provides a PR reference as the argument. Parse it into three values: `owner`, `repo`, `pr-number`.

Supported formats:

| Format | Example | Parsing |
|---|---|---|
| Full URL | `https://github.com/acme/widgets/pull/42` | Extract owner=`acme`, repo=`widgets`, pr-number=`42` from the URL path. |
| Short form | `acme/widgets#42` | Split on `/` and `#`. |
| Bare number | `42` or `#42` | Infer `owner/repo` from the current directory's git remote (see below). |

**Inferring repo from cwd (bare number only):**

Run:

```bash
git remote get-url origin
```

Parse the output to extract `owner/repo`. Handle both SSH (`git@github.com:owner/repo.git`) and HTTPS (`https://github.com/owner/repo.git`) formats. Strip any trailing `.git`. If the remote cannot be parsed or there is no git repo in cwd, report the error and ask the human to provide the full PR reference.

Construct the full PR URL: `https://github.com/<owner>/<repo>/pull/<pr-number>`

### 3. Determine the local repo path

The `--repo` flag tells `ateam dispatch` where to create the worktree. Use the current working directory. If cwd is not a git repository, report the error and ask the human to `cd` into the target repo or provide the full path.

### 4. Write metadata and dispatch

Write structured PR metadata to a temp file and call `ateam dispatch`:

```bash
# Write metadata
TMPFILE="${CLAUDE_JOB_DIR:-/tmp}/dispatch-review-meta-${pr_number}.txt"
printf 'pr-number: %s\npr-repo: %s/%s\npr-url: %s\n' \
  "<pr-number>" "<owner>" "<repo>" "<pr-url>" > "$TMPFILE"

# Dispatch
ateam dispatch \
  --repo "$(pwd)" \
  --problem "Review PR #<pr-number> (<owner>/<repo>)" \
  --body-file "$TMPFILE" \
  --launch-prompt "/agent-teams:review-pr {id}" \
  --skip-epic
```

`--skip-epic` prevents the review initiative from being grouped under an epic. `--launch-prompt` causes the background session to run the `review-pr` skill instead of the full `/dri` playbook.

### 5. Report and hand off

Relay the output `ateam dispatch` printed. Tell the human:

- The initiative id and worktree path (from dispatch output).
- The PR being reviewed: `PR #<pr-number> (<owner>/<repo>)`.
- How to watch and control the review session:

```bash
claude agents                   # list background sessions
claude logs <session-id>        # recent output without attaching
claude attach <session-id>      # open it in this terminal
claude stop <session-id>        # abort early OR reap a finished idle session
```

When the background review finishes, it closes its initiative and the session stays idle — use `claude stop <session-id>` to reap it.

## Key constraints

- This skill does NOT review the PR — it only dispatches a background session that does.
- No codebase exploration, no diff reading, no opinion forming.
- Uses `ateam` (not raw `bd -C`) for all global workspace operations.
- CARDINAL RULE: no work beads in the global workspace.
