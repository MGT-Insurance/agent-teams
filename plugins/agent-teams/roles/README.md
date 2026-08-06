# Why these definitions live here instead of `agents/`

These are the agent-teams role agent definitions (`planner.md`,
`implementer.md`, `tester.md`, `reviewer.md`, `investigator.md`). They live
in `roles/` instead of
`plugins/agent-teams/agents/` **so Claude Code does not auto-register
them**, because plugin-scope registration is broken for named teammate
spawns. This is a workaround for an open upstream Claude Code bug, not a
stylistic choice — read on before "cleaning it up."

## The upstream defect

All three OPEN as of 2026-08-04:

- **anthropics/claude-code#78234** — root cause. The in-process teammate
  spawn path resolves the agent type and then discards it via a source
  filter excluding `built-in` and `plugin` sources.
- **anthropics/claude-code#81746** — our exact case.
- **anthropics/claude-code#81852** — the `tools:` allowlist is dropped on
  the same path. Does not currently affect us — none of our role
  definitions declare a `tools:` key — but it will the moment someone adds
  one.

## The docs say this should work

[code.claude.com/docs/en/agent-teams](https://code.claude.com/docs/en/agent-teams)
states you can reference a subagent type from any scope including plugin,
and that "the teammate honors that definition's `tools` allowlist and
`model`, and the definition's body is appended to the teammate's system
prompt." Behavior contradicts documentation. That is why this is a bug we
are compensating for, not a design we are fighting — and it is what makes
this workaround temporary rather than architectural.

Concretely: before this move, a **named** spawn of a plugin-scope role
(`subagent_type: "agent-teams:planner"` + `name:`) silently produced a
generic agent — full tools, no role prompt, no per-role model — with **no
error**. Only an **unnamed** spawn (addressed later by `agentId`) picked up
the plugin definition correctly.

## Why the obvious fixes do not work

- **Same-name precedence override.** The scope-priority table (`--agents`
  is priority 2, plugin is priority 5) only applies "when multiple
  subagents share the same name." That can never fire here: a plugin agent
  registers under a **namespaced** identifier containing `:`
  (`agent-teams:planner`), and CLI/file-based definition names cannot
  contain `:` — Claude Code refuses to load one, enforced from v2.1.218. The
  two can never share a name, so priority never applies. This was tested
  directly, not reasoned: bare-named CLI agents (`planner`, `implementer`,
  …) did not override the plugin ones — **both** appeared in the agent
  listing, and naming the colon form still produced a generic spawn.
- **A `settings.json` `"agents"` key.** Silently ignored.
- **User scope (`~/.claude/agents/`).** Works, but scatters copies that go
  stale against the plugin — rejected.

## The fix in force

The definitions moved to `plugins/agent-teams/roles/`, a directory
Claude Code does not scan for auto-registration (unlike `agents/`,
`skills/`, `hooks/`, `commands/`). `ateam dispatch` (and `ateam resume`,
and every other background launcher that goes through `bgSessionArgs`)
builds a `claude --agents '<json>'` payload from these same files at launch
time and passes it on every background launch. `--agents` is a **CLI-scope**
definition — a scope the discard bug does not filter — so a **named**
teammate spawn of `agent-teams-<role>` (hyphen key, generated from the
filename) gets its full role prompt, its own `model:`, and a mailbox
identity, exactly as the docs describe for plugin scope.

The discriminator that proves a definition actually attached is
`customAgentType` in the subagent's `.meta.json` sidecar
(`~/.claude/projects/<project>/<session>/subagents/*.meta.json`) —
**not** `agentType`. `agentType == the spawn name` is normal for every
teammate, named or not, and proves nothing. A working named spawn carries
`customAgentType`; a broken one does not.

## What this costs today

An interactive session that was not launched by `ateam dispatch` or
`ateam resume` — `/dri` typed by hand, `/agent-teams:review-pr` invoked
directly, an ad hoc session — has **no** `agent-teams-<role>` types at all,
because only those two launchers inject the `--agents` payload. Such a
session cannot run a team: naming `agent-teams:<role>` (colon) is blocked
by a hook, and it no longer resolves to anything even if it weren't. This
is a deliberate, accepted trade-off — the human only dispatches DRIs — not an
oversight, and it must not be "fixed" by putting a copy of the definitions
back in `agents/`.

## Where the moving parts live

| Location | Why |
|---|---|
| `plugins/agent-teams/roles/*.md` (this directory) | The role definitions themselves, kept out of Claude Code's `agents/` auto-registration scan. Adding one here is all it takes: `buildAgentsJSON` scans the directory and has no allow-list of role names. |
| `internal/verbs/agentsjson.go` | Builds the `--agents` JSON payload from these files at launch time (`resolveRolesDir` + `buildAgentsJSON`); also backs the `ateam agents-json` verb used to inspect the payload without launching a session. |
| `internal/verbs/dispatch.go` (`bgSessionArgs`) | Passes `--agents <payload>` on every background launch (`ateam dispatch`, `ateam resume`, `ateam new-initiative`, the `--launch-prompt` path). |
| `plugins/agent-teams/hooks/scripts/block-colon-named-role-spawn.sh` | PreToolUse hook that denies a **named** spawn of the removed `agent-teams:<role>` colon form. The colon path never consults the type registry, so a stale colon-named spawn would otherwise launch a generic agent with full tools and no error, silently — this hook converts that into a loud, actionable message pointing at the hyphen key. |
| `plugins/agent-teams/hooks/scripts/block-model-divergence.sh` | Resolves role definitions from `roles/` (repointed off the old `agents/` path) to check a spawn's `model:` against the definition's; also matches both the hyphen (`agent-teams-*`) and colon (`agent-teams:*`) forms. |
| `plugins/agent-teams/hooks/hooks.json` (`SubagentStart` matcher) | `agent_type` in the hook payload is the arbitrary spawn **name**, not the role, so no regex can match a named spawn by role — the old role-keyed matcher (`(^|:)(implementer\|planner\|tester\|reviewer)$`) was already dead for every named spawn before this initiative even started. It is being widened to a throttled catch-all for that reason (the action it gates, a learnings pull, is role-independent anyway). |
| `plugins/agent-teams/skills/dri/references/execution.md` | Tells the DRI which key to name: prefer `agent-teams-<role>` (hyphen) with a `name:`; never use `agent-teams:<role>` (colon) at all. |
| `ateam spawn-check` (`internal/verbs/spawncheck.go`) | The witness. Scans a session's `.meta.json` sidecars and flags any `taskKind == "in_process_teammate"` entry missing `customAgentType` — the one check that reads the harness's own ground truth instead of modeling its behavior, and the only guard that catches a named spawn of a type that silently resolved to nothing. |

## Removal condition

This is scaffolding around someone else's bug, not a permanent design. Run
this test to find out whether it can come out:

1. Put one role definition back under `plugins/agent-teams/agents/`.
2. In a session launched **without** `--agents`, spawn
   `subagent_type: "agent-teams:<role>"` **with** a `name:`.
3. Read that spawn's `.meta.json` sidecar under
   `~/.claude/projects/<project>/<session>/subagents/`.
4. If it carries `customAgentType == "agent-teams:<role>"`, the upstream bug
   is fixed.

If step 4 succeeds, revert the whole workaround:

- Move `roles/` back to `agents/`.
- Drop the `--agents` plumbing from `bgSessionArgs` (and `agentsjson.go`).
- Drop `block-colon-named-role-spawn.sh` (and its `hooks.json` entry).
- Simplify `execution.md`'s spawn rule back to a plain "don't pass `name:`"
  or whatever the fixed behavior actually calls for.
- Keep `ateam spawn-check` — a witness that a definition attached is worth
  having regardless of which scope supplies it.

Check anthropics/claude-code#78234, #81746, #81852 for a fix landing before
attempting this.
