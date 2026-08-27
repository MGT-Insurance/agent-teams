---
name: dispatch-dri
description: Dispatch a new agent-teams initiative to a managed background Codex DRI. Use for /dispatch-dri, starting a separable initiative in the background, or handing work to a fresh DRI without occupying the current session. Creates a worktree, registers the initiative, and launches the Codex-native dri skill.
---

# Dispatch a Codex DRI

You dispatch a new initiative; you do not become its DRI. This skill creates an isolated checkout, registers the initiative, and launches a managed background Codex DRI while the current session stays free.

## Dispatcher authority

Use this handoff when a human wants an initiative to proceed without occupying the current session, or when separable work deserves its own DRI, checkout, team, and PR instead of expanding another initiative. Do not absorb that separable work into the current initiative.

**THIS SESSION IS A HANDOFF, NOT AN INVESTIGATION.**

**ABSOLUTE CONSTRAINT — NEVER investigate.** Do not explore or read the codebase, grep files, research mechanisms, analyze architecture, design a solution, or answer open questions on the human's behalf. A vague statement stays vague. The background DRI must investigate independently; dispatcher analysis contaminates the handoff with assumptions.

**ABSOLUTE CONSTRAINT — ALWAYS launch a DRI.** Every invocation must end in a background DRI launch. Do not refuse, decide that the work is too small, judge that the scope is unclear, or ask whether the human wants to proceed. There are only two legitimate pre-dispatch stops:

1. `ateam` or the selected runtime is unavailable. Direct the human to the selected runtime's agent-teams setup skill and stop.
2. No problem statement was provided. Ask only for that statement, then dispatch immediately after receiving it.

Once the required inputs exist, launch unconditionally. Your complete job is preflight, capture the human's framing verbatim, dispatch, and hand off the output.

**CARDINAL Beads boundary.** The global agent-teams workspace holds only initiative-tracking beads and role memory. Initiative registration is the one global write, performed only by `ateam dispatch` through its sanctioned register path—never by raw `bd -C`. All contract, feature, task, test, and discovery beads belong to the PROJECT repository and are created later by the DRI and its team.

## 1. Preflight

- Run `ateam ws`. If it fails or `ateam` is not found, use the selected runtime's setup route and stop.
- Run `ateam audit`. It must report clean before dispatch.

- For a missing or failing `ateam`, direct the human to `agent-teams-codex:setup-agent-teams`.
- Run `ateam runtime check codex`. If the standalone managed Codex runtime, hooks, or role definitions are not ready, direct the human to `agent-teams-codex:setup-agent-teams` and stop.

## 2. Capture the human's framing

The only judgment here is whether the handoff inputs are present. Do not investigate or analyze the repository to fill perceived gaps.

- **Problem statement:** take the one-line statement verbatim from the invocation. If none exists, ask the human for it. Do not rephrase or embellish it.
- **Context:** copy the human's constraints, background, decisions, and unanswered questions verbatim into a temporary body file outside the repository. Do not add analysis, mechanism opinions, or design assumptions. Pass open questions through unanswered. Omit the file and `--body-file` only when the human supplied no additional context.
- **Target repository:** the initiative can target a different repository from the dispatcher's current directory. Default to the single unambiguous current Git repository. Pass `--repo <absolute-path>` only when the human named another target, cwd is not inside a repository, the problem clearly refers to a different project you cannot locate, or more than one repository plausibly fits. Do not explore code to choose.
- **Base branch:** let dispatch detect the repository's default branch. Pass `--base-branch <branch>` only when the human named or clearly implied a non-default base, or when there is genuine base ambiguity.
- **Standby:** pass `--standby` only when the invocation contains that token or an explicit request to park or wait. This is mechanical passthrough, not a judgment about whether standby is warranted. Standby never cancels the launch.

## 3. Dispatch through Codex

Run exactly one creation call:

```bash
ateam dispatch --runtime codex --problem "<one-line problem statement>" \
  [--body-file <tmpfile>] [--repo <absolute-path>] \
  [--base-branch <branch>] [--standby]
```

Omit `--body-file` only when no additional human context exists. Never use `--no-launch`. `ateam dispatch` handles slug creation, worktree creation, initiative registration, and submission of the Codex-native DRI skill through the managed app-server. Work beads remain the DRI's responsibility.

## 4. Report and hand off

Relay the successful dispatch output without replacing it with a summary. Report the initiative ID, absolute worktree path, slug, and base branch exactly as printed. Tell the human that decision gates are visible through `ateam human-list` and the initiatives dashboard, so they do not need to tail runtime logs continuously.

The dispatcher does not investigate after launch and does not take ownership of the new initiative. The background DRI recovers the verbatim framing from the initiative record, investigates, plans, and drives the work.

### Codex managed-runtime controls

Also relay the event-log path. Give the human these runtime controls:

```bash
tail -f <event-log-path>
ateam runtime open codex
ateam show <initiative-id>
ateam human-list
ateam resume <initiative-id> --runtime codex
```

The managed daemon can outlive terminal processes. The durable thread ID is stored as `session:` on the initiative. Mail persists in Beads before waking or steering that same thread; a failed submission leaves the message unread and retryable.
