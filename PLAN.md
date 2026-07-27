# Plan: Role-aware water-cooler participation (at-1ldm)

Epic: `agent-teams-142k`

## Summary

Two deliverables, connected only by one documented environment variable pair:

1. **agent-teams publishes a role signal.** Every `ateam`-launched session gets
   `ATEAM_ROLE` (+ `ATEAM_INITIATIVE` when known) in its process environment.
   Delivery is via the `claude --settings '{"env":{...}}'` JSON, NOT `cmd.Env` —
   the daemon's spare-session pool claims pre-warmed processes over IPC, so env
   set on the exec'd argv never reaches the claimed session, while the
   `--settings` payload does (verified live previously; see the comment at
   `internal/verbs/dispatch.go:434-452`). All launches converge on exactly two
   functions, so the change is small and choke-pointed.
2. **A new, MGT-specific `water-cooler` plugin** repackages midgard's in-repo
   water-cooler skill + hooks so any session on the machine gets them, and its
   SessionStart hook branches on `$ATEAM_ROLE`: concrete per-initiative sync
   instructions for DRIs, a fleet-level read-mostly posture for the steward,
   and today's generic nudge when the variable is absent.

agent-teams never references water-cooler; water-cooler reads two env vars and
degrades gracefully when they are absent. The plugin's publishing home is a
plan-gate decision (options + recommendation in "Staging decision" below).

---

## 1. The contract: `ATEAM_ROLE` / `ATEAM_INITIATIVE`

Frozen by the contract bead before any implementation:

| Variable | Values | Semantics |
| --- | --- | --- |
| `ATEAM_ROLE` | `dri`, `steward` (open enum) | The role of the *session*, set by whatever launched it. Consumers MUST treat unknown values and absence identically: fall back to generic behavior. |
| `ATEAM_INITIATIVE` | initiative id (e.g. `at-1ldm`) | Set when the launcher knows the id (dispatch `/dri` path, resume, `--launch-prompt` review sessions). Omitted for the steward and for `new-initiative` invoked with a bare problem statement. |

**Delivery mechanism:** merged into the existing `--settings` JSON as an `env`
map, e.g. `{"autoCompactWindow":200000,"env":{"ATEAM_ROLE":"dri","ATEAM_INITIATIVE":"at-xyz"}}`.
Claude Code applies a settings `env` map to the session, and hooks (spawned by
the session process) inherit it. Two facts need one cheap live verification
(precedent: `scripts/verify-live-settings.sh`):

- a settings-`env` var is visible to a plugin hook script (`printenv`), and
- `claude respawn <id>` preserves it (respawn revives the same session; if it
  re-reads the original session config this is free — must be confirmed, since
  mail-triggered respawns are a normal DRI lifecycle event).

**Role values per launch path (v1):**

- `ateam dispatch` (default `/dri` path), `ateam new-initiative`, `ateam resume`,
  and every `--launch-prompt` variant (PR-review sessions, `--resume-launch-prompt`
  relaunches from `ateam mail send` / `route-pr-event`) → `ATEAM_ROLE=dri`.
  Review sessions are DRIs of a review initiative; a distinct `review` value is
  a follow-up if wanted (open question 2).
- `ateam steward start` → `ATEAM_ROLE=steward`, no initiative id.
- `/bg-session` skill stays deliberately bare (its SKILL.md already documents
  "no DRI-specific env additions") — no role, generic nudge.
- Interactive sessions (a human running `/dri` in a terminal): not launched by
  ateam, so no env, so generic nudge. Env is the only interface; no marker-file
  fallback (that would couple the plugin to agent-teams internals).
- Subagents (implementer/tester/reviewer via the Agent tool): run inside the
  DRI session's process tree and inherit `ATEAM_ROLE=dri`. That is harmless —
  SessionStart hooks don't fire for subagents, so no nudge duplication.
  Per-subagent role values are explicitly out of scope (follow-up).

## 2. agent-teams changes (this repo)

Every session launch converges on two functions; nothing else execs `claude`
with `--bg` (`claude respawn`/`claude stop` in messaging.go/reap_orphans.go
reuse or kill existing sessions):

1. **`rawLaunchBGSession` + `bgSessionArgs`** — `internal/verbs/dispatch.go:455-543`.
   Thread `role` and `initiativeID` parameters through `launchFunc`/`rawLaunchFunc`
   so `bgSessionArgs` builds the merged settings JSON (replacing the flat
   `autoCompactWindowSettingsJSON` constant with a small builder). Callers:
   `launchBGSession` (dispatch//dri, new-initiative, resume, mail-relaunch) and
   the `--launch-prompt` path in `dispatchKong.Run` / `resumeKong.Run`.
   `new-initiative` passes the initiative id only when its `driArg` is an id.
   Unit tests extend the existing argv assertions (`dispatch_test.go`,
   `TestBGSessionArgs_ContainsSettingsFlag` and friends).
2. **`defaultStewardLaunch`** — `internal/verbs/steward_start.go:201-213`.
   Add `--settings '{"env":{"ATEAM_ROLE":"steward"}}'` (steward currently passes
   no `--settings` at all). Also update the two places that document the manual
   launch command: `steward_start.go` header comment and
   `plugins/agent-teams/skills/steward/references/operations.md:20`.

**Release protocol (mandatory, per repo CLAUDE.md):** rebuild the committed
binaries (`sh scripts/build-binaries.sh`, commit `plugins/agent-teams/bin/`)
and bump the version in BOTH `.claude-plugin/marketplace.json` and
`plugins/agent-teams/.claude-plugin/plugin.json`. A source-only change ships
nothing.

No skill, hook, or doc in agent-teams mentions water-cooler. The env var is
generic ("what role is this session"), useful beyond this consumer.

## 3. The water-cooler plugin (new, MGT-specific)

Repackages the three midgard assets — `.claude/skills/water-cooler/SKILL.md`
(5.1 KB protocol skill), `.claude/hooks/water-cooler-session-start.sh`
(SessionStart context nudge), `.claude/hooks/water-cooler-pr-created.sh`
(PostToolUse nudge on `gh pr create`) — as a user-level plugin:

```
<staging-home>/
  .claude-plugin/marketplace.json          # lists the water-cooler plugin
  plugins/water-cooler/
    .claude-plugin/plugin.json             # name, version
    skills/water-cooler/SKILL.md           # midgard skill, paths de-midgardized
    hooks/hooks.json                       # SessionStart + PostToolUse(Bash)
    hooks/scripts/session-start.sh         # role-branched (below)
    hooks/scripts/pr-created.sh            # carried over, + role-aware pr_url text
    README.md                              # install + contract pointer
```

Conversion mechanics: hook paths become `${CLAUDE_PLUGIN_ROOT}/hooks/scripts/...`
(same convention as agent-teams' hooks.json); the skill's reference to
`.claude/skills/water-cooler/SKILL.md` becomes the plugin skill reference; the
SessionStart matcher should be `startup|resume|clear|compact` so long-lived bg
sessions re-receive the nudge after compaction (midgard's bare hook only fires
on the default matcher). No `userConfig` options in v1.

### Role-branched SessionStart behavior

The weak-nudge finding (DRI took ~25 min to first sync; fleet syncs at zero
during the soak) says generic "sync sometime soon" prose loses to a DRI's
crowded startup context. The fix within the dispatched framing: make the
injected instruction **concrete and immediate** so following it requires zero
decisions:

- **`ATEAM_ROLE=dri`:** hook composes the actual call: workstream key = current
  branch (`git branch --show-current`; hook cwd is the session worktree),
  initiative id from `$ATEAM_INITIATIVE`, and instructs: sync NOW in the first
  work turn (not "roughly hourly") with workstream/title/status; sync with
  `pr_url` when a PR opens; closing status at wind-down. One initiative = one
  workstream entry.
- **`ATEAM_ROLE=steward`:** one fleet-level heartbeat per wakeup, primarily a
  reader — triage peer deltas against watched initiatives; explicitly told NOT
  to create per-initiative entries.
- **Absent/unknown:** exactly today's generic midgard nudge.

All branches keep the existing graceful-degradation clause: if `water_cooler_*`
MCP tools aren't connected, skip silently.

### Reinforcement beyond SessionStart (enhancement tier)

- **PostToolUse on `gh pr create`** (carried over from midgard, part of the
  plugin from day one): the highest-value nudge — fires at the exact moment
  `pr_url` exists.
- **Rate-limited heartbeat:** a `UserPromptSubmit` hook gated by a state file
  (fires at most once/hour) re-injects "sync with cursor as since". Bg DRI and
  steward sessions take turns via mail wakes, so UserPromptSubmit is the
  per-wakeup seam — this is how the steward gets its "one heartbeat per wakeup"
  without per-initiative noise. Enhancement, not loop-closing.
- **PreCompact:** compaction marks a long session — a natural re-nudge point.
  Enhancement; may be redundant with the SessionStart `compact` matcher.

No enforcement machinery (no Stop-blocking, no forced tool calls) — guidance
placement only, per the dispatched framing.

### Midgard double-nudge

Midgard sessions would get both the in-repo hook and the plugin hook. Both are
one-paragraph context injections, so the interim cost is noise, not breakage.
Plan: no dedupe logic in the plugin; retire the midgard in-repo hooks +
settings.json entries (and point `docs/WATER-COOLER.md` at the plugin) in a
small midgard PR once the plugin is proven — filed as a blocked enhancement
bead (work happens in midgard, tracked here).

## 4. Staging decision — where the plugin lives and installs from

Eric's current marketplaces (`~/.claude/plugins/known_marketplaces.json`):
`claude-plugins-official` (GitHub), `beads-marketplace` (GitHub
gastownhall/beads), `agent-teams` (**local directory** `~/Code/agent-teams`,
autoUpdate). Teammates need a GitHub-sourced marketplace
(`claude plugin marketplace add <org>/<repo>`, installed once user-level).

| Option | Pros | Cons |
| --- | --- | --- |
| **A. New `MGT-Insurance/mgt-plugins` repo** (recommended) | Clean ownership; tiny clone; room for future MGT plugins; zero coupling to agent-teams or midgard | One more repo to create/administer |
| B. The `MGT-Insurance/water-cooler` repo itself | The hub ships its own client; no new repo; teammates already have access | Mixes service-written bulletin data with plugin code; marketplace clone churns with every bulletin commit |
| C. midgard repo | Assets already there; one retirement PR doubles as the move | Marketplace add clones the entire monorepo into `~/.claude/plugins/marketplaces/` per teammate — heavy and slow to update |
| D. agent-teams marketplace repo | Reuses an existing marketplace | Couples the MGT-specific plugin to the deliberately generic agent-teams repo — against the spirit of the hard constraint even though only the CLI/plugin content is technically bound by it |

**Recommendation: A.** B is an acceptable second if Eric dislikes a new repo.

Loop closure does NOT wait on GitHub publishing: Claude Code accepts directory
marketplaces (Eric's agent-teams install is one), so the plugin is developed in
a local repo, installed via `claude plugin marketplace add <local-dir>`, and
verified live; pushing it to the chosen home is the first enhancement. The
staging answer decides the repo's final remote, not the loop-closing work.
Execution note: the plugin beads' code lands in that separate repo, never in
agent-teams — only the tracking beads live here.

## 5. Bead plan

Contract first, then a loop-closing SET (smallest end-to-end exercise: one real
launch path sets the env → plugin hook reads it → role guidance lands in a live
session), then blocked enhancements. All beads `--parent agent-teams-142k`.

**Contract**
- C1. Freeze `ATEAM_ROLE`/`ATEAM_INITIATIVE`: names, values, settings-env
  delivery, absence semantics, consumer rules. Blocks everything below.

**Loop-closing set** (file-disjoint where parallel)
- L1. Thread role/initiative through `bgSessionArgs`/`rawLaunchBGSession` and
  all dispatch/resume callers + unit tests. (`internal/verbs/dispatch.go`)
- L2. Steward launch sets `ATEAM_ROLE=steward` + manual-command docs.
  (`internal/verbs/steward_start.go`, `skills/steward/references/operations.md`)
- L3. Water-cooler plugin scaffold: marketplace.json, plugin.json, ported
  skill, role-branched SessionStart hook, pr-created hook. (separate local repo)
- L4. Release protocol: rebuild committed binaries + version bump in both
  marketplace.json and plugin.json. (after L1+L2)
- L5. Live loop-close verification: install plugin from local dir marketplace,
  dispatch a probe initiative, confirm the hook sees `ATEAM_ROLE=dri` and emits
  the DRI guidance; confirm generic fallback in a bare session; confirm
  `claude respawn` preserves the settings-env. One-time real-session cost,
  precedent `scripts/verify-live-settings.sh`.

**Enhancements (blocked on L5)**
- E1. Publish the plugin to the chosen staging home + install docs (+ point
  midgard's `docs/WATER-COOLER.md` at it).
- E2. Rate-limited heartbeat hook (UserPromptSubmit, once/hour state file):
  DRI hourly cursor-sync + steward per-wakeup fleet heartbeat.
- E3. Midgard retirement PR: remove in-repo water-cooler hooks/settings wiring.
- E4. Distinct `review` role value + `--role` override on dispatch (only if
  open question 2 answers "yes").

## 6. Open questions for the plan gate

1. **Staging home** (section 4). Recommended default: new
   `MGT-Insurance/mgt-plugins` repo.
2. **Review-PR sessions' role**: `dri` in v1, or a distinct `review` value now?
   Recommended default: `dri` in v1; E4 later if steward/consumer behavior ever
   needs to distinguish them.
3. **Midgard dedupe**: accept interim double-nudge and retire midgard's in-repo
   hooks after the plugin is proven (E3)? Recommended default: yes — no dedupe
   logic in the plugin.
4. **Heartbeat hook** (E2): in scope as a blocked enhancement? Recommended
   default: yes — the soak-test finding says SessionStart alone under-delivers;
   this is the cheapest placement that reaches bg sessions mid-life.
5. **Live verification budget**: L5 spends one or two short real bg sessions.
   Recommended default: approve — it is the only way to verify the settings-env
   path end-to-end, and the daemon-pool env gotcha is exactly the kind of thing
   that only live checks catch.
