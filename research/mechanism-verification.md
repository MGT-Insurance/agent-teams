# hung-scan mechanism verification — at-pp7z overnight-stall incident

Initiative: at-jolk (epic agent-teams-sgr5). Read-only code + artifact
forensics. Code refs are to this checkout (branch tip = current main).

## Verdict (up front)

**CONFIRMED, with one important refinement.** The stuck-since anchor was
reset by every tick that observed the tied session as *not* STUCK
(WORKING, DEAD, or AWAITING-HUMAN) — this is a real, deliberate, documented
code path (`internal/verbs/hung_scan.go:296-354`), not a bug in the sense of
an accident; it's "clear the anchor on any non-STUCK observation," and it
fires on every blip. Transcript evidence (below) shows four real activity
bursts overnight that almost certainly flipped `claude agents --json`
Status to `"busy"` for at least one 5-minute tick each, which is sufficient
to drop the anchor per the code's own logic.

The refinement: I cannot produce a byte-exact tick-by-tick classification
log proving *which specific tick* saw `busy` — hung-scan keeps no such
log (see "What's unrecoverable" below). The reconstruction is: (a) the code
provably drops the anchor on any non-STUCK tick, (b) the transcript
provably shows real assistant/user activity inside four windows overnight,
(c) each window is longer than one 5-minute tick interval, so by pigeonhole
at least one tick almost certainly landed inside "busy." That chain is as
strong as the available evidence gets — it is not a direct observation.

A second, independent contributor also matches the evidence and is at
least as important: one of the four bursts (23:32–23:46, tied to the
`plan-approval` gate raised/cleared during this window) very plausibly
carried the `gate:question` label, which — via
`classifyInitiative`'s evaluation order — preempts STUCK entirely
(AWAITING-HUMAN, rule 2, checked before the live-session/idle checks). Gate
lines aren't versioned in bd (only current labels are visible), so this
can't be proven from current state, but it's consistent with the notes
history on at-pp7z and is a second, independent way a blip resets/delays
the anchor that the current design does not distinguish from a real
recovery-to-work.

## 1. Classification: WORKING vs STUCK vs other

`classifyInitiative` (`internal/verbs/hung_scan.go:223-256`), first-match-wins:

1. **DEAD** — `worktree:` directory missing (orphan check via `dirExists`).
2. **AWAITING-HUMAN** — labels carry `"human"` AND (`"gate:question"` OR
   `"gate:review"`) — checked *regardless of PID presence*, i.e. even if a
   live busy session exists, a real gate wins.
3. **DEAD** — no *live* tied session (`matchSessionsForInitiative` returned
   none with `PID != nil`) — only reached once a real gate is ruled out.
4. **WORKING** — ANY live tied session has `Status == "busy"` OR
   `State == "working"`.
5. **STUCK** — everything else: ≥1 live tied session, all idle/waiting, no
   gate.

Comment at `hung_scan.go:199-222` documents this was deliberately restructured
(agent-teams-6rru.13) so a gated-but-dead-looking initiative reports
AWAITING-HUMAN, not DEAD.

## 2. What "busy" means — what's actually probed

hung-scan's only session-liveness signal is `claude agents --all --json`
(`defaultAgentsJSONAll`, `internal/verbs/messaging.go:296-303`, invoking
`runAgentsJSON` at line 306 which execs the `claude` CLI as a subprocess).
The parsed `agentSession` struct (`messaging.go:279-288`):

```go
type agentSession struct {
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"`      // "interactive" | "background"
	Status    string `json:"status"`    // "busy" | "idle" | "waiting"
	Name      string `json:"name"`
	State     string `json:"state"`     // unreliable; do not branch on this
	ID        string `json:"id"`
	PID       *int   `json:"pid"`       // nil => tracked-but-dead
	SessionID string `json:"sessionId"`
}
```

**hung-scan never reads the transcript file, never checks mtimes, and never
inspects transcript content.** "Busy" is entirely the Claude Code CLI's own
opaque `Status` field, sourced from wherever `claude agents` gets its
liveness/activity signal (outside this repo — not verifiable from here).
hung-scan treats it as ground truth with no corroboration. This means any
process that makes the CLI briefly report `Status=="busy"` — including a
short mail-processing burst that produces a handful of assistant turns and
then returns to idle — is indistinguishable, to hung-scan, from the session
having actually resumed durable forward progress.

`isActivelyWorking` (`status.go:196-209`) is a *different* verb's
(`execution-status`) predicate over the same session list, matched by CWD
rather than by tied session-id; it is not used by hung-scan's
classification path (which uses `matchSessionsForInitiative` /
`matchSessionByWorktree`, session-id- or Name-keyed — see `match.go:160-182`
and `messaging.go:350-...`). Two independent, slightly different matchers
exist across the two verbs.

## 3. Stuck-since anchoring — every reset path

Persisted at `<StewardHome>/hung-state.json` (`hungStatePath`,
`hung_scan.go:122-124`) as `map[initiativeID]hungAnchor`:

```go
type hungAnchor struct {
	StuckSince   string
	AlertedAt    string
	WakeAttempts int
	LastWakeAt   string
}
```

`scanHung` (`hung_scan.go:281-362`) is the single classification+anchor
engine, called by both the read-only CLI verb (`persist=false`) and the tick
(`persist=true`, `hung_tick.go:213`). The anchor lifecycle, every tick:

- `prevAnchors := loadHungState(statePath)` — load prior anchors (line 283).
- For each open initiative, classify. **Only if `class == hungClassStuck`**
  (line 326) does the code carry the anchor forward into `newAnchors`
  (creating a fresh one with `StuckSince = now` if none existed, lines
  327-331) and compute elapsed/Hung.
- **`newAnchors` is built fresh every call, and only ever receives entries
  for ids classified STUCK *this scan*** (lines 293, 331). The trailing
  comment is explicit about the consequence (lines 344-348):

  > "Anchors for any id not re-added above (non-STUCK this scan, or the
  > initiative closed and dropped out of the open list entirely) are
  > dropped by construction — newAnchors only ever holds this scan's STUCK
  > ids, which is exactly the 'cleared on any non-STUCK observation'
  > contract the bead asks for."

- `saveHungState` (persist=true only) overwrites the whole file with
  `newAnchors` (line 356), so a dropped anchor is not just absent from
  memory — it's **erased from disk** on the next tick.

**Every code path that resets the anchor** is exactly: *any single tick*
where `classifyInitiative` returns anything other than `STUCK` for that
initiative — i.e.:
- a live tied session reports `Status=="busy"` or `State=="working"`
  (WORKING), OR
- the initiative's labels carry `human` + (`gate:question`|`gate:review`)
  (AWAITING-HUMAN), OR
- no live tied session at that instant (DEAD, e.g. a session mid-restart), OR
- the worktree directory is momentarily unreadable (DEAD).

There is no debounce, no minimum-observation-count, no "still basically the
same episode" grace window — one non-STUCK snapshot at a 5-minute-spaced
tick is enough to zero out however much STUCK-time had already accumulated
for that episode. There is also no separate accounting of *how many times*
the anchor has been reset for the same underlying stall — from hung-scan's
point of view, tick 1's STUCK and tick 40's STUCK (after 39 anchor-resets)
look identical: a brand new episode starting at `now`.

## 4. Tick cadence and invocation

- `hungTickInterval = 5 * time.Minute` (`hung_tick.go:32`).
- `runHungTick` (`hung_tick.go:276-291`) is started as a **goroutine inside
  the single `ateam relay` OS process** (`relayKong.Run`, `relay.go:210`:
  `go runHungTick(ctx, t)`), immediately before `relay` blocks forever on
  `t.Receive(...)` (Telegram long-poll). It never returns; ticks forever on
  `time.NewTicker(hungTickInterval)`.
- **There is no separate scheduler, cron, or launchd job for hung-scan** —
  it lives and dies with the singleton `ateam relay` process. I found **no
  launchd job, no persistent daemon manager, and no relay process currently
  running** on this machine (`launchctl list` has no `ateam`/`relay` entry;
  `ps aux` shows none running now). `ateam relay` appears to be started
  ad hoc (e.g. from within a session) rather than supervised — so hung-scan
  (and its wake ladder) only runs when *something* has a `relay` process
  alive, with no restart guarantee if it dies or the machine sleeps/restarts.
- The read-only `ateam hung-scan` CLI verb (`hungScanKong.Run`,
  `hung_scan.go:380-396`) is a separate, on-demand path (`persist=false`);
  it never writes state and is not itself a scheduled thing — it's a manual
  inspection tool.

## 5. Wake ladder

`nextHungLadderAction` (`hung_tick.go:65-76`), applied only to entries where
`scanHung` set `Hung=true` (i.e. STUCK continuously since ≥15 min,
`hungStuckThreshold`, `hung_scan.go:52`):

- Not yet alerted, `WakeAttempts < hungWakeAttemptsBeforeDirectAlert` (=2,
  `hung_tick.go:39`): increment `WakeAttempts`, set `LastWakeAt`, action =
  **wake** → `sendHungWakeEnvelope` → `ateam mail send steward --file <f>
  --sender hung-scan` (`defaultHungWakeSend`, `hung_tick.go:88-93`).
- Once 2 wakes have been sent: action = **alert** → `postHungAlert`
  (`hung_tick.go:165-183`) posts a canned, LLM-free message directly into
  the initiative's own Telegram topic (resolved via its `thread:` label),
  exactly once (`AlertedAt` gates it from firing again this episode).

Because the tick interval is 5 minutes, once `Hung` first flips true the
ladder can in principle exhaust 2 wakes and fire the direct alert within
~10 minutes of first crossing 15-minute-STUCK — i.e. the ladder itself is
fast. **The ladder firing (sending mail) and the Steward actually acting on
it are two different things**: the wake is only a `mail send` into the
Steward's inbox; whether/when the Steward session notices depends on the
Steward's own polling loop (`wake-watcher.sh`/`inbox-drain.sh` doorbell
mechanism) being alive and running. See incident findings below — this gap
matters here.

## 6. Incident reconstruction (at-pp7z, night of 2026-07-23→24)

### What's directly observable

**`hung-state.json` is currently `{}`** (`~/.agent-teams/steward/hung-state.json`,
confirmed by `cat`) — the file is a single current-value snapshot that is
fully overwritten every persisting tick, and per §3 the anchor is dropped
entirely the instant an initiative stops being STUCK. **This means the
actual historical anchor timeline for at-pp7z (every StuckSince value it
ever held that night, every reset) is unrecoverable from this file** — it
holds no history, only whatever the last write happened to contain, and by
now (initiative long past that episode) it holds nothing for at-pp7z at all.

**`~/.agent-teams/debug/hooks.log` has zero mentions of "hung-scan" or
"hung tick"** anywhere in the file (grep confirmed, 0 hits). doHungTick's own
logging (`transport.Logf`, `hung_tick.go:243,247`) only fires on wake/alert
*send failure*, not on every tick or every classification — so even a
perfectly healthy tick loop leaves no trace here. Also, hooks.log is fed by
the wake-watcher/inbox-drain *hook scripts*, a completely separate logging
pipe from whatever `ateam relay`'s stderr goes to (no relay log file, no
launchd job, no running process was found on this machine — see §4). There
is **no durable, append-only log of what hung-scan observed or decided at
any tick that night** — the design has no audit trail for this at all,
only the single mutable state file and (if it happens to arrive) mail into
the Steward's box.

### What's reconstructable from the session transcript

`~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-quoting-api-x-appetite-api-integration-hard/2cc31bf0-e672-4496-a1b6-4301388fd1b1.jsonl`
(610 lines, 466 timestamped events, session start 23:13:17Z). Grouping
`assistant`/`user`/`queue-operation` timestamps into contiguous windows,
this session had real (not merely heartbeat) activity in four bursts that
night, each preceded by a `queue-operation` entry (an inbound mail/doorbell
being delivered into the session's own input queue):

| Window (UTC) | Trigger | Duration |
|---|---|---|
| 23:13:17 – 23:15:06 | session start | ~2 min |
| 23:32:39 – 23:46:58 | queue-operation @ 23:32:39 | ~14 min |
| 00:18:01 – 00:56:54 | queue-operation @ 00:18:01 (doorbell) | ~39 min, with sub-gaps |
| 02:59:00 – 03:03:17 | queue-operation @ 02:59:00 (doorbell) | ~4 min |
| 03:17:41 – 03:30:36 | queue-operation @ 03:19:14 (doorbell) | ~13 min |
| *(silence)* | | last real turn 03:23:09; next event 04:23:38 is queue-operation only, no assistant reply |

Cross-referencing `~/.agent-teams/debug/hooks.log`'s `wake-watcher.sh`
entries for `at-pp7z`, the `doorbell-seen`/`doorbell-fired` events land at
**00:18:01** and **02:59:00** exactly, matching two of the transcript
bursts above — confirming these are real inbound-mail wake events, not
transcript-parsing artifacts. (The task brief's "~23:23" and "~02:23–03:15"
approximate the same set of events; the precise doorbell timestamps from
the hook log are 23:32:39 [an inbox-drain without a prior wake-watcher
doorbell line — likely delivered while the watcher was mid pidfile-takeover,
see hooks.log lines 45325-45341], 00:18:01, and 02:59:00, plus a further
mail-processing burst starting ~03:17-03:19 that doesn't show a distinct
`doorbell-seen` line in the excerpted window but does show `queue-operation`
entries in the transcript and a flurry of `pidfile-takeover` events
(03:18:44, 03:23:13) consistent with churn during mail delivery.)

The `~/.agent-teams/steward/ledger.jsonl` has exactly one at-pp7z entry in
this window: `{"ts":"2026-07-24T02:59:00.665579Z","category":"plan-approval",
"initiative":"at-pp7z","recommendation":"Revised plan (appetite→quote as v1
loop-closer) + shared-dependency fork handling","verdict":"accepted"}` —
i.e. the 02:59 burst was a **plan-approval gate cycle**, which independently
supports the second contributing mechanism in the Verdict: during (some
portion of) that burst, at-pp7z plausibly carried `gate:question`, which
would have forced AWAITING-HUMAN and pre-empted STUCK/anchor logic entirely
regardless of session `Status`.

**The task's stated fact that hung-scan anchored STUCK only at 04:08** is
consistent with this reconstruction: the last real activity is 03:23:09
(assistant) / 03:30:36 (a bare `system` event, likely a housekeeping/
compaction check, not real work) — some tick after that first observed
`Status` genuinely idle with no gate and a live PID, and 04:08 is the first
such tick the incident record credits with actually anchoring. The ~38-minute
gap between last real activity (03:30) and the credited anchor time (04:08)
is not explained by any artifact still available (no historical
`hung-state.json`, no tick log) — plausible causes given the code (a late
`DEAD` reading from a momentarily-absent PID during the pidfile-takeover
churn at 03:18:44/03:23:13, a lagging `Status` field in `claude agents`
itself, or simply an unlucky tick-boundary alignment) are all speculative
and I flag them as such rather than asserting one.

**Steward wake at 05:28**: I found **no `doorbell-seen` event for the
Steward's own inbox anywhere between 02:58:39 and past 05:45:16** (the tail
of the available hooks.log window) — the Steward's `wake-watcher.sh` shows
only routine `alive` heartbeats in that stretch (e.g. 04:07:39, 04:23:38,
04:40:05, 04:57:18, 05:26:24 — all `note alive`, none `doorbell-seen`). If
hung-scan's wake ladder had fired `ateam mail send steward` at ~04:23 and
~04:28 (15 min after a 04:08 anchor, +5/+10 min per the 5-minute tick), I
would expect a `doorbell-seen` for `steward` shortly after. **I did not find
one in the retained log window.** This is consistent with (not proof of)
the relay/hung-tick process not actually running through that stretch —
which matches the current-state finding that no `ateam relay` process, log
file, or launchd job exists on this machine today. I cannot confirm from
available artifacts whether relay was running that specific night; I can
only report that its absence today is the kind of gap that would fully
explain a wake ladder never firing at all, and that the Steward's own inbox
shows no incoming doorbell in the relevant window. The 05:28 wake Eric
observed may have come through a different path entirely (e.g. a periodic
Steward loop noticing on its own, not a mail delivery this tick) — I cannot
verify the mechanism of that 05:28 event from what's left in these artifacts.

### What's overwritten/unrecoverable — stated plainly

- `hung-state.json`: current-value-only, now `{}` for at-pp7z; no history.
- No hung-scan/hung-tick tick-by-tick classification log exists anywhere
  (hooks.log has 0 hits; doHungTick only logs on send-failure).
- No `ateam relay` process log/file was found on this machine, so there is
  no way to confirm relay's uptime/liveness through the incident window
  from artifacts alone.
- bd issue labels are current-state only (not versioned), so the exact
  `gate:question`/`gate:review` label timeline on at-pp7z that night cannot
  be reconstructed — only inferred from the notes-field narrative and the
  ledger's single plan-approval timestamp.

## 7. Inputs hung-scan already has cheaply vs. what it ignores

**Already probed, cheaply, from outside any session:**
- `claude agents --all --json` snapshot: CWD, Kind (interactive/background),
  Status (busy/idle/waiting), Name, State, ID, PID, SessionID — one
  subprocess call, no session cooperation needed.
- `bd list --status=open --json` for every open initiative: full
  description (worktree:, session: lines), labels, title, notes.
- Filesystem: whether the registered worktree directory still exists
  (`defaultDirExists`, `reap_orphans.go:115`).
- Its own prior anchor state (`hung-state.json`).

**Currently ignored, despite being cheap and already-adjacent:**
- **Worktree git state** — no check of uncommitted changes, last commit
  time, `git status`, or branch divergence. A session that's actually dead
  but left a clean worktree looks identical to one mid-crash.
- **bd issue update/comment recency** — `updated_at`/notes-append timestamps
  are already in the same `bd list --json` payload hung-scan already
  fetches, but classification never reads them. A tick that flips WORKING
  because of a 4-minute mail-processing blip has no way to notice "and yet
  the bd notes/commits haven't moved in 5 hours."
- **Transcript content vs. mtime** — hung-scan never opens
  `~/.claude/projects/.../<session>.jsonl` at all. It has no way to
  distinguish "briefly busy processing an inbound reply, immediately
  idle again" from "genuinely back to doing the initiative's real work" —
  both look like one `Status=="busy"` tick. Transcript mtime (or even a
  coarse "did the transcript grow substantively, not just by a queued
  mail turn, since last STUCK observation") is a cheap, already-on-disk
  signal that's completely unused.
- **Wake-watcher's own heartbeat/doorbell log** (`hooks.log`) — a
  completely separate liveness signal (per-initiative "alive elapsed=Ns",
  doorbell-seen/fired) already being written continuously by a different
  subsystem, never cross-referenced by hung-scan.

## 8. Mode (bg vs interactive) handling

**Not handled at all in classification.** `agentSession.Kind` ("interactive"
| "background") is parsed from `claude agents --json` and is a field on the
struct (`messaging.go:281`), but `classifyInitiative` never reads `Kind` —
only `Status`, `State`, and `PID`. A `mode: bg` initiative (like at-pp7z,
whose bd description literally has `mode: bg`) and an interactive
foreground session are classified by the exact same busy/idle/waiting rule,
with no adjustment for the fact that a bg DRI session idling overnight
waiting on a human is a completely normal/expected state (it's supposed to
sit there), whereas an interactive session going STUCK for 15 minutes might
mean something different. There is also no distinct threshold, ladder pace,
or wake-target logic keyed on `Kind`/mode anywhere in `hung_scan.go` or
`hung_tick.go`.

## Files referenced

- `internal/verbs/hung_scan.go` — classification, anchor persistence, `scanHung`
- `internal/verbs/hung_tick.go` — tick loop, wake ladder, alert
- `internal/verbs/relay.go` — singleton process hosting the tick goroutine
- `internal/verbs/messaging.go:279-360` — `agentSession`, `claude agents` probing, worktree matcher
- `internal/verbs/match.go:149-182` — `matchSessionsForInitiative` (session-id-based tied-session resolution)
- `internal/verbs/status.go:196-209` — `isActivelyWorking` (sibling verb's independent CWD-based matcher, not used by hung-scan)
- `internal/verbs/steward_seams.go` — `StewardHome`, wake envelope helpers
- `~/.agent-teams/steward/hung-state.json` — currently `{}`
- `~/.agent-teams/debug/hooks.log` — wake-watcher/inbox-drain trail for at-pp7z and steward, 0 hung-scan mentions
- `~/.agent-teams/steward/ledger.jsonl` — single at-pp7z plan-approval entry at 02:59:00
- `~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-quoting-api-x-appetite-api-integration-hard/2cc31bf0-e672-4496-a1b6-4301388fd1b1.jsonl` — session transcript, activity-burst evidence
