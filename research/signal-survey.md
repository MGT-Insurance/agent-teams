# Work-product signal survey for a better hung-scan stall tripwire

Initiative: at-jolk (epic agent-teams-sgr5). Research only — no code/beads changed.

## 0. What hung-scan consumes today (baseline)

Read `internal/verbs/hung_scan.go` + `internal/verbs/hung_tick.go` directly.

- The **only** signal is `claude agents --all --json` (`defaultAgentsJSONAll`):
  each tied session's `status`/`state` (busy/working vs idle) and PID
  presence, joined to the initiative's `worktree:`/`session:` lines and its
  bd labels.
- Classification is a first-match-wins ladder: **DEAD** (worktree dir
  missing) → **AWAITING-HUMAN** (labels `human` + `gate:question`/
  `gate:review`) → **DEAD** (no live tied session) → **WORKING** (any live
  session busy/working) → **STUCK** (live, all idle, no gate).
- Durable anchor: `StuckSince` is persisted only for **STUCK**, and only the
  STUCK-and-`elapsed >= 15min` case ever becomes `Hung` and feeds the
  escalation ladder (`hung_tick.go`: 2 Steward wake-nudges, then a canned
  Telegram alert — all gated on `entry.Hung`, i.e. STUCK only).
- **DEAD is never escalated by the mechanical tick.** `plugins/agent-teams/skills/steward/SKILL.md:145-146`
  confirms DEAD handling is the *Steward's own eyeballing* of a hung-scan
  snapshot at whatever cadence it happens to wake — exactly the
  "no anchored notion of how long" problem the STUCK ladder was built to
  solve, just left unsolved for DEAD.
- **Zero work-product signal anywhere in this path.** "Busy" only means the
  parent Claude process is inside a tool call / generating tokens. It says
  nothing about whether that tool call is ever going to return, or whether
  git/bead state is actually moving.

This is the central gap driving the design: **a session can be `busy`
forever waiting on a dead child process**, and hung-scan has no way to tell
"actively working" from "waiting on something that already died."

## 1. Candidate signals

### 1a. Worktree git state (index mtime / `git status --porcelain` hash) — ★ top pick

- **Mechanism**: every worktree's `.git` file is a pointer
  (`gitdir: <repo>/.git/worktrees/<name>`); the real `index` and `HEAD` files
  live there. `stat` on `index` gives "time of last stage/commit"; a hash of
  `git status --porcelain` output diffed tick-over-tick gives "did the
  working tree change at all" (catches edits not yet staged too).
- **Cost**: reading the `gitdir:` pointer + `stat`-ing `index`/`HEAD` is two
  tiny file reads, no subprocess, sub-millisecond. `git status --porcelain`
  is one subprocess against a *local* repo (not the contended global bd DB)
  — cheap in every worktree I sampled (near-instant, `midgard` is a
  mid-size monorepo).
- **Durability**: mtime is a real wall-clock timestamp on disk. Critically,
  this survives **detector downtime** (e.g. the machine — and hung-scan's
  own tick — asleep for hours): on wake, `now - index_mtime` is still
  correct even though no tick ran during the gap. A tick-counting or
  "N consecutive polls unchanged" design would NOT have this property;
  hung-scan already avoids that trap for STUCK (StuckSince is a timestamp),
  so extending the same pattern to git state is a small, consistent change.
- **False-positive risk**: real, and it's the important caveat — see §3.
  Planning/investigation phases legitimately produce multi-hour git
  flatlines. This signal must be scoped to "initiative has a bd issue
  claimed/in-progress in *implementation* mode" or combined with session
  status, not applied unconditionally to every open initiative.

### 1b. Commits / reflog on initiative branches

- Same data source as 1a (git), via `git log -1 --format=%at` or reflog.
  Strictly coarser than index-mtime (only sees committed work, misses staged
  work sitting for hours — exactly the pp7z failure mode). Use index mtime
  as primary, commit time as a secondary/backup cross-check (e.g. to
  disambiguate "index touched because of an unrelated `git status` run" —
  not actually a concern since only real add/commit operations touch the
  index, but worth noting `git status --porcelain` itself does NOT touch
  index mtime unless it refreshes stat-cache entries, which it can on some
  git versions — a real value-diff on `git status --porcelain` output is
  the more precise signal than raw mtime if this edge case matters).

### 1c. Bead status / updated timestamps

- `bd show <id>` on both the registry (`~/.agent-teams`) and the project
  repo's own `.beads` exposes `created_at`/`started_at`/`updated_at`/
  `closed_at`. Verified on `midgard-6fk0.1`: `started_at` and `closed_at`
  bookend the pp7z incident almost exactly (21:59:39 → 05:58:14 local) —
  but there is **no update in between**. Bead timestamps only move at
  claim-time and close-time; they are a coarse bookend, not a fine-grained
  liveness poll.
- **Lock contention is real and already documented**: at-6nj (closed,
  merged PR #75) reproduced 6 concurrent `bd -C ~/.agent-teams list
  --status=open --json` calls against the embedded-Dolt global workspace
  ranging 0.87s–8.4s under load, and a real incident where a leaked 2s-poll
  dashboard process drove ~90 concurrent bd calls to 13–75s latency each,
  which blew past Claude Code's 30s `UserPromptSubmit` hook timeout. Fix
  was raising the dashboard's poll interval 2000ms→15000ms + a 10s child
  timeout. hung-scan's own existing `bd list --status=open` call at a
  5-minute tick is fine (same order of magnitude as the fixed dashboard
  interval); the risk is **adding more frequent or per-worktree bd calls**
  against the *project* repo's separate `.beads` DB (different DB, own
  embedded Dolt, own lock) — cheap in isolation but multiplies with worktree
  count. Recommendation: only reach into project-repo bd state for
  initiatives *already* flagged as STUCK/DEAD candidates by the cheaper git
  check, not on every open initiative every tick.

### 1d. Transcript growth (`~/.claude/projects/<dir>/<session>.jsonl`)

- Cheap to `stat` (mtime/size), but **actively misleading** without content
  inspection — this is empirically confirmed by the pp7z transcript (see
  §2): during the ~7h stall the file grew at ~23:23 and again 02:23–03:15
  local from `queue-operation`/`system`/`task-notification` entries (mail
  and Stop-hook heartbeats) with **zero assistant/user work content** in
  between. A naive "any transcript growth in N minutes" check would have
  reported the session alive/making-progress for the entire outage. Any use
  of this signal must classify JSONL record types and only count
  `assistant`/`user` turns carrying real `tool_use`/`text` content — not
  `queue-operation`/`system`/`attachment` framing — as "activity." Even
  then it only proves the *parent* session is alive, not that its
  subagent/tool call is progressing (same blind spot as `claude agents`
  busy status).

### 1e. Subagent/teammate liveness (child process table, child jsonl)

- In principle observable: teammate sessions get their own
  `~/.claude/projects/<dir>/<id>.jsonl`, and `ps` can show `claude` child
  processes. In practice, for the pp7z incident the dead subagent
  (`impl-pp7z-contract`) left no separate registry tie — hung-scan only
  ties to the DRI's own `session:` line, so subagent death is invisible to
  it structurally. Making this observable would require the DRI to record
  spawned-teammate PIDs/session-ids somewhere durable (it doesn't today).
  Higher implementation cost than 1a; not recommended as a v1 signal, but
  worth flagging as the deeper fix for "parent shows busy forever waiting
  on a dead child" if git-state alone proves insufficient.

### 1f. Other cheap signals surveyed

- Any-file-under-worktree freshness (`find <wt> -newer <ref>`) —
  subsumed by 1a/git-status; adds cost without new information for a git
  workflow.
- CI/PR status (`gh pr view`) — network-bound, rate-limited, not "cheap";
  useful for AWAITING-HUMAN/review-parked cases but not for the stall
  tripwire itself.

## 2. Incident reconstruction

### at-pp7z (2026-07-23 22:30 → 2026-07-24 05:28 local, ~6h58m)

Full reconstruction from the DRI's own transcript
(`~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-quoting-api-x-appetite-api-integration-hard/2cc31bf0-e672-4496-a1b6-4301388fd1b1.jsonl`),
worktree git state (`/Users/ericlloyd/Code/midgard/.git/worktrees/pp7z-impl-contract`),
and bd bead `midgard-6fk0.1`:

- 22:22:45–22:23:09 local: DRI explicitly checks on the implementer
  subagent and asserts **"Not stalled — it's actively running gates right
  now."** — a live false-negative moment, 7 minutes before the actual death.
- 22:30:36 local: last real system event before the outage (matches the
  task's stated "22:30" start almost to the minute — this is very likely
  the machine-sleep instant).
- 22:30:36 → 05:28:21: transcript contains **only** sparse
  `queue-operation`/`system`/`task-notification` heartbeats (23:23, 02:23,
  03:15) — no assistant/user work turns — until a `"Continue from where you
  left off."` wake prompt at 05:28:13, which the model answers "No response
  requested." (still not real work).
- 05:39:51 local: first real tool_use (Bash) after the wake — recovery
  begins.
- **Git**: `pp7z-impl-contract` worktree was checked out 21:59:36 (matches
  bead `started_at` 21:59:39 almost exactly); its first — and only —
  commit (`45008237ab`, the contract) landed at **05:40:43 local**, ~7h41m
  after checkout. No intervening commit/index activity is possible to see
  directly (current file state is post-recovery), but the objective
  before/after commit bookends plus the bead's own note ("Recovery: machine
  sleep killed impl-pp7z-contract overnight. Staged contract diff already
  DRI-verified... Finishing directly") confirm work was staged well before
  22:30 and sat uncommitted for hours.
- **Bead**: `midgard-6fk0.1` `started_at=2026-07-24T02:59:39Z` (=21:59:39
  local), `closed_at=2026-07-24T10:58:14Z` (=05:58:14 local) — bookends the
  whole incident with zero visibility into the middle.

**Why the existing mechanism didn't fire**: the initiative ties to the
DRI's own session (session: line = `2cc31bf0...`), not the dead
subagent. From the parent's perspective, a subagent tool call that will
never return looks identical to one still legitimately running — `claude
agents --json` very plausibly reported this session as **busy/WORKING the
entire night** (waiting on its own `Agent`/Task-tool call), which
structurally can never become STUCK, so the 15-minute STUCK anchor/ladder
never had a chance to engage. This is the mechanism gap, not a threshold
tuning problem.

**Would a git-state signal have fired, and when?** Yes, dramatically
earlier than the ~7h it actually took: index/`git status` for
`pp7z-impl-contract` would show zero change from ~22:30 onward. Even a
**15–30 minute** "no worktree change while a bead is claimed in-progress"
threshold would have fired by ~22:45–23:00 — 6+ hours before the DRI's own
morning recovery.

### at-gusm (2026-07-22 evening, ~2h stall)

Root-cause is independently documented in at-f307's description (Eric- and
steward-ratified framing) and fixed by at-ps11 (merged, PR #125,
`agent-teams-zalv` epic): **the DRI's own session cwd drifted into a track
worktree** (`at-gusm-track-1`, an in-process subagent side-effect), so
`matchSessionByWorktree` (the pre-at-ps11 matcher) tied the wrong session
to the initiative, classifying it **DEAD instead of STUCK** — and DEAD is
the classification the mechanical ladder never escalates (§0). The
initiative sat for ~2 hours with hung-scan reporting DEAD and nobody woken.

`at-gusm-track-1`'s own transcript (`f908544f-8ef3-418a-9f53-8ff81db38231.jsonl`)
and git state (worktree checked out 18:24 local 07-22; single own commit
`0773198d4e` at 02:43:21 local 07-23, ~8h19m later) show a similarly long
flatline, but this window legitimately overlaps investigation/root-cause
work (per at-9qfb's history, this track was re-diagnosing a wrong prior
finding) — so it's supporting evidence, not a clean isolated repro the way
pp7z is.

**Verdict**: at-gusm's primary fix is already shipped (at-ps11's
deterministic `session:` tie eliminates the cwd-drift misclassification
path). A work-product signal is a **complementary, independent** check —
it would likely have also flagged the stall on its own git-flatline
grounds — but the bigger, still-open gap this incident exposes is
structural: **DEAD is not on the escalation ladder at all**, for either
cwd-drift misclassification (now fixed) or for a subagent that has simply
crashed/exited (`cwd_present:true`, pid nil) — the Steward SKILL.md text
says this should be escalated "exactly like HUNG," but only via the
Steward's own irregular wake-eyeballing, not the durable, mechanical
`hung_tick.go` ladder. Any redesign should extend the anchor/ladder to
DEAD-with-cwd-present, not just STUCK.

## 3. False-positive check

- **at-ps11** (healthy, fast, merged same evening): 8 commits in ~50
  minutes (22:00:55–22:49:20 local) on its own worktree — dense,
  continuous git activity. Zero false-positive risk at any reasonable
  threshold (15–60 min).
- **Planning/investigation phases legitimately git-flatline for hours.**
  at-pp7z's *own* early phase (spawning a planner, drafting
  `APPETITE-GATE-PLAN.md`, iterating through 3 steward gate rounds) and
  at-gusm-track-1's root-cause investigation both show multi-hour windows
  with no git changes that are real, wanted work — not stalls. **This is
  the load-bearing caveat for the whole survey**: a bare "no git change for
  N minutes" rule fires false-positive on every design/plan/investigation
  phase. It must be scoped to only apply when the initiative has a bd issue
  in an *implementation* claim (an implementer track with a specific
  in-progress work item), not to planner/DRI-investigation phases —
  mirroring how `classifyInitiative` already treats gate labels as an
  exemption, this needs an analogous "is this phase expected to touch git"
  exemption.
- **Gate-parked sessions produce zero work product legitimately and must be
  exempt.** Confirmed cheaply readable: `bd show <id> --json` labels for
  at-6nj (`gate:review`,`human`), at-y7l9 (`gate:question`,`human`),
  at-7q4j (`gate:review`,`human`), at-o0v (`gate:question`,`human`) all
  carry exactly the label pair `classifyInitiative` already checks
  (`hasLabel(labels,"human") && (gate:question||gate:review)`) — this is
  the *same* single `bd list --status=open --json` call hung-scan already
  makes every tick, so the gate exemption costs nothing extra and already
  generalizes to any new signal built on top.
- **Human-idle case** (at-2bkv style, ~16-min idle in an interactive
  session): interactive-mode initiatives are a distinct failure domain —
  the human, not the model, is the bottleneck, and no automated signal
  (git, bead, transcript) should fire escalation against the human's own
  pacing. `mode: interactive` is already a labeled/registry-visible field
  (see at-2bkv's description: `mode: interactive`) and should gate out of
  any stall tripwire the same way `mode: bg` initiatives are the intended
  target — recommend restricting the whole tripwire to `mode: bg`
  initiatives, sidestepping the interactive-idle false-positive class
  entirely rather than trying to distinguish "human idle" from "model
  stalled" within an interactive session.

## 4. Ranked shortlist

1. **Git worktree state (index mtime + `git status --porcelain` diff),
   scoped to initiatives with a claimed in-progress *implementation* bead**
   — cheapest (near-free stat/local subprocess, no contention with the
   global bd DB), highest resolution, and the only signal that would have
   caught pp7z hours earlier. Primary recommendation.
2. **Bead started_at/updated_at/closed_at bookends** (existing `bd list`
   call, zero extra cost) — good coarse corroboration and a natural gate
   for #1 (only check git-flatline when a bead is actually claimed
   in-progress), but too coarse alone (no mid-task updates).
3. **Escalate DEAD, not just STUCK, on the same durable anchor/ladder** —
   not a new signal so much as closing the gap §0/at-gusm exposed; without
   it, a crashed subagent (or a session a future matcher misclassifies)
   still has no durable escalation path.
4. **Transcript content-typed activity** (assistant/user turns only, not
   queue/system noise) — useful secondary corroboration ("is the parent
   session itself alive") but must filter record types or it actively masks
   stalls, as demonstrated in the pp7z transcript.
5. **Subagent/teammate liveness** — most direct fix for "busy forever
   waiting on a dead child," but requires new bookkeeping (durable
   parent→child session ties) not present today; worth a follow-up
   initiative if git-state + DEAD-ladder prove insufficient in practice.

**Threshold**: the existing 15-minute STUCK threshold is well-supported for
session-idle detection; for the new git-flatline signal, evidence supports
something in the **15–30 minute** range (same order of magnitude) *given
the implementation-claimed-bead gate* — pp7z's flatline was ~7 hours and
at-ps11's healthy commits are minutes apart, so there is a wide safe margin
and no evidence pushing toward a larger number. Do not apply it
unconditionally to planning/investigation phases (see §3) or to
`mode: interactive` initiatives.
