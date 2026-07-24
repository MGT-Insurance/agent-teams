# Transcript-mining analysis: what a stall looks like, quantitatively

Initiative: at-jolk (epic agent-teams-sgr5). Read-only transcript forensics —
no repo code touched, no `~/.agent-teams` bd writes. Complements two existing
docs in this same `research/` dir:

- `mechanism-verification.md` — code-level analysis of `hung_scan.go`
  (why the anchor resets on any non-STUCK tick) and a UTC-timestamped
  reconstruction of the same pp7z incident, cross-referenced against
  `hooks.log` doorbell events.
- `signal-survey.md` — ranks candidate signals; recommends git-worktree
  state (index mtime / `git status --porcelain`) as the primary fix, with
  transcript content flagged only as a secondary/cautionary corroboration.

This doc's job is different and narrower: **mine transcripts only** (no git,
no bd) and produce hard numbers — gap distributions, event-type mixes,
growth rates — that test which transcript-only signatures actually separate
a confirmed stall from healthy/parked/human-away sessions, and which don't.

**Timezone note**: all session JSONL timestamps are UTC (`Z` suffix). This
machine's local zone is `CDT` (`UTC-5`, confirmed via `date`). The task
brief's approximate incident times ("~10:30pm", "~11:23pm", "~2:23–3:15am",
"steward wake 5:28am") are **local CDT**, not raw UTC digits — converting
transcript UTC timestamps by `-5h` reproduces every one of those times to
within a minute (verified below). `mechanism-verification.md` instead prints
raw UTC digits without converting (e.g. its "23:32" = this doc's "18:32
CDT"); both are internally consistent, just different display conventions.
This doc gives both where it matters.

Scratch scripts: `/Users/ericlloyd/.claude/jobs/a844e26a/tmp/parse.py`
(JSONL → per-event summary: type, role, tool names, text length, keys) plus
inline analysis snippets (not persisted — see Method below).

---

## 1. Case at-pp7z — minute-by-minute reconstruction of the overnight stall

Transcript: `~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-quoting-api-x-appetite-api-integration-hard/2cc31bf0-e672-4496-a1b6-4301388fd1b1.jsonl`
(605 lines, 461 timestamped events, session spans 2026-07-23T23:13:17Z →
2026-07-24T13:46:42Z = 18:13 CDT Jul23 → 08:46 CDT Jul24, 14.56h).

### 1.1 Timeline (CDT), 19:30–06:00

| Local (CDT) | idx | Type | What it is |
|---|---|---|---|
| 19:39–22:23 | 172–313 | assistant/user, Bash/Edit/SendMessage tool_use | Normal active work: DRI drives implementer subagent, edits, checks. Continuous, sub-minute gaps. |
| 20:00:00 | 238 | system `away_summary` | "Awaiting your approval via the steward... **No code started.**" — a *legitimate parked-gate* moment. |
| 21:59:00 | 239–260 | queue-op → user → Bash bursts | Mail arrives (approval), work resumes. |
| 22:00:02 | 261 | assistant `Agent` tool_use | Spawns the contract-implementer subagent. |
| 22:22:45–22:23:09 | 303–313 | assistant text | DRI explicitly self-checks and asserts the implementer is "actively running gates right now" — **a live false-negative, 7 minutes before the actual death.** |
| **22:30:36** | 320 | system `away_summary` | "the contract bead is implemented and running its final lint/typecheck gates." Last event before the outage. **This is the ~22:30 "died" instant the task brief cites.** |
| **23:23:38** | 321–322 | queue-operation (×2), `<task-notification>...<status>killed</status>` | Two background Bash tasks report **killed**. First hard evidence of death, 53 min after last real activity. |
| 23:23:38 → **02:23:25** | 322→323 | **(nothing)** | **180.0 min (3.0h) of total silence** — zero events of any kind. |
| 02:23:25 | 323–325 | queue-operation, "Stop hook... heartbeat re-arm ... no new mail, do nothing" | A *scheduled* self-check heartbeat fires. |
| 02:23:37–02:23:43 | 328–333 | assistant Bash: `git log`, `bd show`, **`ps aux \| grep pp7z-impl-contract`** | Self-diagnostic. `ps aux` **returns nothing** — subagent process confirmed dead — but the session does not conclude "I need to alert." |
| 02:23:58 | 338 | assistant Bash: `git add -A && git commit ...` | Attempts to take over the commit itself. |
| **02:40:56** | 339 | tool_result | **`Exit code 143` / `Command timed out after 10m 0s`** — the commit attempt hangs and is killed. |
| 02:41:07–02:41:21 | 341–347 | assistant Bash: re-check `git log`/`status` (still shows old HEAD + `M` uncommitted files), launches a **new background gate chain** (`build:packages && typecheck && test`) | Second recovery attempt. |
| 02:41:22 | 354 | queue-operation, `<task-notification>...<status>failed</status>` | The background gate chain **fails within ~1 second of being launched.** |
| 02:41:27 | 355 | assistant text | Claims: "Gate chain... is running in the background... When it reports `ALL_GATES_PASS`, I commit..." — **already false**; the notification one line earlier already said `failed`. |
| 02:41:36–02:41:42 | 360–364 | assistant Bash: tail build logs | Inspects log tail. |
| **02:58:25** | 365 | assistant text | **"API Error: Connection closed mid-response. The response above may be incomplete."** — a real API-level failure. |
| **03:15:16** | 366 | system `away_summary` | "Building the appetite gate... **the contract bead is done but uncommitted. Quality gates are running in the background now; once green I commit it**..." — **stale/false relative to the ground truth one cycle earlier** (failed gate, API error). This is the exact "3:15am" endpoint the task brief cites. |
| 03:15:16 → **05:28:21** | 366→369 | **(nothing)** | **133.1 min (2.2h) of total silence.** |
| **05:28:21** | 369–377 | queue-operation (dequeue), user "Continue from where you left off." | **Steward wake — matches the task's "5:28am" exactly.** |
| 05:39:51–05:40:44 | 384–425 | assistant Bash: git/bd checks, a further failed 10m Bash attempt (idx398/399 also short), background gate relaunch | Real recovery finally begins. |
| **05:40:43 (local git commit time)** | — | `git commit 45008237ab` in `pp7z-impl-contract` worktree | **The actual fix lands — 7h10m after the 22:30 death, and only ~12 minutes after the steward wake.** Confirmed via `git log -1 --format=%ci 45008237ab` in the still-extant worktree at `/Users/ericlloyd/.agent-teams-worktrees/pp7z-impl-contract`. |

**Net: from 22:30 death to 05:28 steward wake = 6h58m.** Two silent gaps
(180min + 133min = 313min = 5h13m) bookend one ~35-minute burst of *failed*
recovery activity (six Bash tool_use calls, one 10-minute command timeout,
one background-task failure, one API connection error) plus a stale
`away_summary` claiming progress that had already failed. **Real forward
progress (the commit) did not happen until human/steward intervention.**

### 1.2 Whole-session stats (pp7z)

- 605 total lines, 461 timestamped, 14.56h span, 1,297,409 bytes.
- Bytes/hr = 89,127; events/hr = 41.6 (whole-session average — **see §4,
  this average is not discriminating**).
- Gaps (460 of them): median 0.85s, p90 14.6s, p99 2655s, max 10786s (180min).
- Top 5 gaps (min): 44.3, 53.0, 119.0, 133.1, **179.8**.
- Tool mix: Bash 61, SendMessage 10, Agent 6, Read 2, Edit 2, ToolSearch 1.
- Failure-token grep counts across the whole file: `status>killed<` ×2,
  `status>failed<` ×6, `Exit code 143` ×2, `command timed out` ×2,
  `Connection closed` ×12.

---

## 2. Case at-gusm — implementer death + DRI response, and where the transcript actually ends

Transcript: `~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-at-gusm-track-1/f908544f-8ef3-418a-9f53-8ff81db38231.jsonl`
(663 lines, 473 timestamped, 2026-07-22T23:14:11Z → 2026-07-23T14:40:30Z =
18:14 CDT Jul22 → 09:40 CDT Jul23, 15.44h). **`isSidechain` count = 0** —
this file is the single DRI-level orchestrator session; no separate
implementer-only transcript exists in `~/.claude/projects` (the "implementer"
is a `Task`/`Agent`-tool subagent whose crash is visible only through the
DRI's own status checks and `idle_notification` teammate-messages, not a
transcript of its own).

### 2.1 How the transcript *ends* — not abruptly, because it got resumed and finished cleanly

The file's **last ~40 lines are a completely healthy, graceful close-out**:
`ateam mail inbox` → PR #4341 confirmed merged (`gh pr view ... mergedAt`) →
superseded draft PR closed → `ateam clear-gate` → bead closed → an
`away_summary` at 09:40:30 CDT stating "Nothing pending here." **This is an
important negative finding**: an abrupt/dangling transcript ending is *not*
a reliable stall signature by itself, because a session that died mid-step
can be silently resumed (new `SessionStart:resume`) hours later and finish
completely normally in the same file. The file *contains* an abrupt death —
just not at EOF.

### 2.2 The actual death + recovery, found mid-transcript

Biggest gap: **idx179→182, 21,212s = 353.5 min = 5.9h**, from
2026-07-22T20:44:55 CDT ("Test tooling... idled while its background test
run completes... not intervening yet") to 2026-07-23T02:38:27 CDT (a
`SessionStart:resume` hook firing — **not** a heartbeat; the machine
physically slept and no scheduled event fired at all during the gap, unlike
pp7z where heartbeats still fired on schedule while the process stayed
alive). Detail:

- idx227 (07:39:25 CDT, i.e. right after resume): DRI's own words —
  **"The machine slept ~8h mid-run; your background test run died and
  nothing was committed."**
- idx228: `SendMessage` to the dead implementer fails outright
  (`"No agent named 'impl-gusm-1' is reachable."`).
- idx232: DRI spawns a **fresh** implementer (`impl-gusm-2`) with an explicit
  prompt: "A predecessor implementer wrote the full fix but its session died
  before running gates/committing... verify diff matches contract... finish
  gates+commit."
- idx239 (07:45:24 CDT, **6 minutes after spawn**): `git log` shows the new
  commit `0773198d4e` already landed, bead `midgard-f7uz.1` already `CLOSED`.

**Recovery was fast once triggered** (~6 minutes from fresh-implementer spawn
to committed+closed) because the DRI verified the stale diff was still
correct and simply re-ran gates — contrast pp7z, where the DRI *tried* the
same kind of self-check (`ps aux`, `git log`) but its own remedial actions
kept failing (timeout, failed background gate) rather than delegating to a
clean fresh subagent.

### 2.3 A clean parked-gate example, in the same file

idx436→438: **exactly 240.0 min (4.0h)**, 03:08:48→07:08:49 CDT, bounded by a
`"heartbeat re-arm for initiative at-gusm — no new mail, do nothing"` on
both ends — the PR (#4341) was up for review; nothing to do until a human or
CI acts. Resolved in a single trivial assistant turn ("Heartbeat re-arm
only — nothing to act on. Initiative at-gusm remains delivered and parked on
the review gate for PR #4341"), **zero tool_use calls**, <15s from trigger
to reply. A `pr-shepherd` bot nudge arrives later (idx450, "PR ... awaiting
review for 4h"), the DRI verifies via `gh pr view` (real corroborating tool
call) and reports back accurately — still parked, still healthy. Eric's own
reply ("wtf. My goal was...") lands 61 minutes later (idx463→465) — ordinary
human latency.

### 2.4 Whole-session stats (gusm)

- 663 lines, 473 timestamped, 15.44h, 1,213,215 bytes.
- Bytes/hr 78,583; events/hr 42.9.
- Gaps: median 0.65s, p90 13.2s, p99 2513s, max 21,212s (353.5min).
- Top 5 gaps (min): 41.9, 61.3, 121.3, **240.0**, **353.5**.
- Tool mix: Bash 55, SendMessage 7, Agent 6, Read 1, ToolSearch 1.
- Failure-token grep counts: **all zero** (`status>killed<`, `status>failed<`,
  `Exit code 143`, `command timed out`, `Connection closed` — none appear).
  The gusm death was a *silent* machine-sleep, not a live error cascade — it
  leaves no error-string trace, only the giant silent gap + `SessionStart:resume`.
- `SessionStart:resume` count: 3 (some of these are ordinary reconnects, not
  all death-related — see §4, this token alone is not a clean binary either).

---

## 3. Healthy / contrast cases

### 3.1 at-ps11 — healthy long bg run that delivered

`~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-at-ps11-track-matchers/0a1b82d1-45a7-43cb-91d5-eba761a844b3.jsonl`
— 872 lines, 626 timestamped, 12.25h, 1,618,934 bytes.

- Bytes/hr 132,131; events/hr 71.2.
- Gaps: median 0.65s, p90 12.4s, p99 249.8s, **max 14,402s = 240.0 min**
  (twice: idx820→821 and idx830→831).
- Both 240-minute gaps are the *exact same signature* as gusm §2.3: a
  `heartbeat re-arm for initiative at-ps11 — no new mail, do nothing`
  queue-operation on both ends, resolved by ONE assistant text turn
  ("Heartbeat only — no new mail, nothing to act on. Awaiting the delta-fix
  implementer's termination... otherwise parked at awaiting-merge on PR
  #125"), **19–23 seconds start-to-reply, zero tool_use calls**.
- Tool mix: Bash 92, SendMessage 12, Agent 8, Edit 3, Read 3, ToolSearch 1,
  Skill 1 — dense, real edit/commit activity in the working stretches.
- Failure tokens: all zero.

**Key finding: at-ps11's own biggest gap (240 min) is longer than pp7z's
biggest silent gap (180 min).** Raw max-gap-length is *not* a valid stall
signature on its own — see §4.

### 3.2 at-9qfb — second healthy long bg run

`~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-at-9qfb-track-impl/04ad2e17-62dc-4cc9-a2ec-7539024aade4.jsonl`
— 576 lines, 404 timestamped, 17.21h, 1,149,655 bytes.

- Bytes/hr 66,788; events/hr 33.5 — **lower than pp7z's 89,127/41.6**, despite
  being the healthy case. Confirms whole-session averages don't separate
  cases (§4).
- Gaps: median 0.90s, p90 19.5s, p99 503.4s, max 14,402s (240.0 min) —
  again a heartbeat-cadence gap, same shape as ps11/gusm.
- Failure tokens: all zero.

### 3.3 "socotra" session — interactive, human present with normal pauses

`~/.claude/projects/-Users-ericlloyd--agent-teams-worktrees-new-socotra-tag-apply-default-on-api-create/492825c2-f453-4c81-ad81-46d6b15cba40.jsonl`
— 915 lines, 708 timestamped, 5.52h, 1,613,239 bytes. (References initiative
`at-2bkv` 29 times in its content; used as the human-in-the-loop contrast
since a standalone `at-2bkv`-named project directory was not found under
`~/.claude/projects` — likely reaped after the worktree was cleaned up. All
project files sampled carry `"sessionKind":"bg"` uniformly — this field does
not distinguish interactive-vs-bg in this install, so identity is by content
reference, not by a session-kind flag.)

- Bytes/hr 292,281; events/hr 165.8 — **much higher** than every bg case:
  dense back-and-forth, short human-response latency.
- Gaps: median 1.17s, p90 21.3s, p99 468.3s, max 5955s = **99.3 min** —
  notably the single longest gap here is *shorter* than any of the 240-min
  heartbeat-cadence gaps in the bg cases, and it is a genuine "human stepped
  away" pause, not a heartbeat.
- Top 5 gaps (min): 11.6, 15.7, 19.3, 20.9, **99.3** — a much tighter,
  smoothly-decreasing distribution than the bg cases' "cluster of small gaps
  + one giant 240min outlier" shape.
- Failure tokens: all zero. `SessionStart:resume` count: 2 (ordinary
  reconnects across the human's own session boundaries, not stalls).

---

## 4. Cross-case comparison table

| Case | Duration | Bytes/hr | Events/hr | Max gap (min) | What bounds max gap | Failure tokens | Verdict |
|---|---|---|---|---|---|---|---|
| **pp7z (confirmed stall)** | 14.56h | 89,127 | 41.6 | 179.8 | killed-task notif → (silence) → heartbeat that triggers a failing recovery burst | **22 hits** (killed/failed/timeout/conn-closed) | STALL |
| pp7z, 2nd-longest gap | — | — | — | 133.1 | stale away_summary → (silence) → steward wake | (same file) | STALL |
| gusm (real death, clean recovery) | 15.44h | 78,583 | 42.9 | 353.5 | idle-wait note → (total silence, machine asleep) → `SessionStart:resume` | **0 hits** | recovered fast (~6 min once triggered) |
| gusm, gate-parked window | — | — | — | 240.0 | heartbeat → (silence) → heartbeat, single trivial reply | 0 | HEALTHY (parked gate) |
| ps11 (healthy long bg run) | 12.25h | 132,131 | 71.2 | 240.0 | heartbeat → (silence) → heartbeat, single trivial reply | 0 | HEALTHY |
| 9qfb (healthy long bg run) | 17.21h | 66,788 | 33.5 | 240.0 | same heartbeat shape | 0 | HEALTHY |
| socotra (interactive, human present) | 5.52h | 292,281 | 165.8 | 99.3 | genuine human-away pause, resolved by a real human reply | 0 | HEALTHY (human latency) |

Assistant-turn tool_use ratios (candidate signature #1, tested):

| Case | assistant turns | with tool_use | text-only | empty |
|---|---|---|---|---|
| pp7z | 183 | 82 (45%) | 35 (19%) | 66 (36%) |
| gusm | 176 | 70 (40%) | 40 (23%) | 66 (38%) |
| ps11 | 270 | 120 (44%) | 51 (19%) | 99 (37%) |
| 9qfb | 151 | 64 (42%) | 37 (25%) | 50 (33%) |
| socotra | 274 | 107 (39%) | 63 (23%) | 104 (38%) |

**These ratios are nearly identical across every case, stall or not (39–45%
tool_use, 19–25% text-only).** "Fraction of assistant turns carrying a
tool_use" is **not a usable stall signature** in aggregate — it's a fixed
shape of how the model narrates+acts regardless of whether the work is
actually landing.

---

## 5. Candidate signatures — tested, with verdicts

1. **Presence/absence of `tool_use` events, or raw tool_use ratio.**
   ✗ **Rejected.** See table above — near-identical across all 5 cases.
   The pp7z stall's own "recovery" burst at 02:23–02:58 CDT is *packed* with
   Bash tool_use calls (6 of them) — tool_use presence alone would have
   scored that window as healthy.

2. **Assistant-text-only turns as a fraction of activity.**
   ✗ **Rejected**, same data — 19–25% everywhere, no separation.

3. **Transcript byte/event growth rate, whole-session average.**
   ✗ **Rejected.** pp7z (confirmed stall) sits at 89,127 bytes/hr / 41.6
   events/hr — squarely inside the healthy range (66,788–292,281 bytes/hr,
   33.5–165.8 events/hr). Averaging over the whole session dilutes stall
   time and heartbeat-idle time identically; both healthy and stalled bg
   sessions spend the large majority of wall-clock time silent by design.

4. **Raw single-longest-gap length.**
   ✗ **Rejected as a standalone threshold.** pp7z's confirmed-bad longest
   gap (179.8 min) is *shorter* than both known-healthy bg sessions' longest
   gap (240.0 min, ps11 and 9qfb) and far shorter than gusm's own
   longest gap (353.5 min, machine-sleep — also recovered cleanly). A naive
   "> N minutes triggers alert" rule set to catch pp7z's 180min gap would
   have already been firing constantly on ps11/9qfb's routine 240min
   heartbeat cadence, and missed nothing extra on gusm since 353min > 180min
   anyway — i.e. gap length is **necessary context, not sufficient signal**.

5. **What bounds the gap — the event immediately before/after — is where the
   real separation lives.** ✓ **Confirmed, with concrete shape:**
   - **Healthy heartbeat/gate-parked gap**: both ends are a
     `"heartbeat re-arm... no new mail, do nothing"` (or equivalent PR-gate
     nudge) queue-operation; resolves in **one** assistant turn, **zero**
     tool_use calls, **<25 seconds** trigger-to-reply, and the reply is an
     accurate terminal statement ("parked", "nothing to act on") — confirmed
     in ps11 (idx821→828, 19s), gusm (idx821→828-equivalent, 240min gate),
     and 9qfb (same 240min shape).
   - **Stall "resolution"**: the same trigger shape (heartbeat / new-mail
     doorbell) instead launches a **multi-tool-call burst that fails to
     converge** — pp7z's 02:23–02:58 CDT episode: 6 Bash calls across 35
     minutes, containing a **10-minute command timeout (`Exit code 143`)**,
     a **background task reporting `status="failed"`** within 1 second of
     being launched, and an **`API Error: Connection closed mid-response`**
     — and it still ends with a stale, uncorroborated positive self-report
     (the 03:15 `away_summary`) before returning to multi-hour silence.

6. **Grep-able failure/error tokens in the tail activity window.**
   ✓ **Strongest single signature found, cleanly binary in this sample.**
   `status>killed<`, `status>failed<`, `Exit code 143`/`command timed out`,
   `API Error: Connection closed` — **0 occurrences** across all 4 contrast
   transcripts (ps11, 9qfb, socotra, gusm — despite gusm containing a *real*
   implementer death), and **22 occurrences** inside the one confirmed-bad
   pp7z transcript (2 `status>killed<` + 6 `status>failed<` + 2
   `Exit code 143` [same 2 events also match `command timed out`, not summed
   twice] + 12 `Connection closed` — exact per-token counts in §1.2). This
   is cheap (substring grep over the transcript tail, or since the last
   known-good state), requires no timezone/gap-math, and would have flagged
   pp7z's stall the moment the first `Exit code 143` landed (02:40:56 CDT —
   **2h18m into the stall**, vs. the ~7h it actually took to surface).
   Caveat: this is a small sample (n=1 confirmed stall); it needs validation
   against more incident transcripts before being trusted as sufficient on
   its own, and it is a *lagging* indicator relative to git-worktree state
   (per `signal-survey.md`, git-flatline would have fired by ~22:45–23:00,
   ~2h before the first `Exit code 143`).

7. **Self-report staleness — comparing an `away_summary`'s claim against the
   most recent concrete status token in the same transcript.** ✓ **Real,
   novel finding, worth flagging even though not fully quantified here.**
   pp7z's 03:15 `away_summary` ("quality gates are running... once green I
   commit") directly contradicts the `status="failed"` notification and the
   API-error text that immediately preceded it in the same file. Gusm's
   terminal `away_summary` ("Nothing pending here") is, by contrast, fully
   corroborated by the `gh pr view`/`bd show` calls just before it. A
   tripwire that diffed "the DRI's own most recent narrative claim" against
   "the last hard status token (`status=`, exit code, `bd show` state) it
   actually observed" could catch stalls the DRI itself is unknowingly
   misreporting — but this requires structured parsing of narrative text
   against status tokens, higher implementation cost than #6.

8. **Abrupt/dangling transcript ending.**
   ✗ **Rejected as a detector of "this session died".** gusm's transcript
   contains a real mid-stream death (5.9h total silence) but *ends* on a
   perfectly clean, fully-resolved close-out 15 hours later, because the
   session got silently resumed and finished the work. Do not use "does the
   file just stop" as a signal; a resumed-and-recovered session looks
   identical at EOF to one that never had a problem.

---

## 6. What would actually separate pp7z from the three healthy contrasts, using transcript content alone

Ranked by strength, transcript-only (no git/bd — see `signal-survey.md` for
why git-worktree state is still the recommended *primary* signal; this is
what's left if you want a corroborating check sourced purely from the
session's own JSONL):

1. **Grep the tail of the transcript (since the last classified-STUCK tick,
   or the last N minutes) for a small fixed set of failure tokens**:
   `status="killed"`, `status="failed"`, `Exit code 143` / "command timed
   out", `API Error`. Zero false positives across 4 contrast transcripts;
   present 22 times in the 1 confirmed stall. Cheapest, most mechanical,
   directly implementable as a substring scan — no timezone math, no
   session-kind lookup.
2. **Classify the trigger→response shape of each new-mail/heartbeat cycle**:
   healthy = 1 turn, 0 tool_use, <30s, terminal "parked" claim; unhealthy =
   multiple tool_use calls, >2 min elapsed, and/or a failure token from #1
   appears inside the burst. This generalizes #1 into a repeatable pattern
   rather than a one-off grep, at the cost of needing to detect
   turn/cycle boundaries.
3. **Do not use**: raw max-gap length, tool_use presence/ratio, text-only
   ratio, whole-session bytes/hr or events/hr, or "does the file stop." All
   five were empirically indistinguishable or actively inverted between the
   confirmed stall and the healthy contrasts in this sample.
4. Self-report staleness (§5.7) is promising but underdeveloped here —
   flagged as a follow-up, not a v1 recommendation, given the small sample
   and higher parsing cost.

## Surprising findings worth calling out

- **The DRI in pp7z explicitly told itself "not stalled" seven minutes
  before the death** (22:22:45 CDT: "Not stalled — it's actively running
  gates right now.") — a live false-negative in the model's own narration,
  not just a mechanical monitor blind spot.
- **The two known-healthy long bg sessions (ps11, 9qfb) both have a
  240-minute max gap** — longer than pp7z's own longest silent gap (180min).
  Any tripwire keyed on raw gap length without also checking what bounds it
  will either miss pp7z or false-positive on routine 4-hour heartbeat
  cadences.
- **A real implementer death (gusm) can be completely silent** — zero
  failure tokens, because the cause was the physical machine sleeping, not
  a live process throwing errors — while a *different* real death (pp7z)
  produces a rich trail of `killed`/`failed`/`timeout`/`API Error` tokens
  because the process stayed alive and kept trying (and failing) to
  recover. **A stall tripwire needs both signals**: total-silence-longer-
  than-normal-heartbeat-cadence (catches the silent/machine-sleep mode) AND
  failure-token-density-in-recent-activity (catches the live-but-stuck
  mode) — neither alone covers both observed failure modes.
- **An abrupt transcript ending is neither necessary nor sufficient
  evidence of a stall** — gusm's file ends in a picture-perfect close-out
  despite a real mid-stream death; conversely pp7z's stall shows up as
  *silence*, not truncation, since the session process itself never exited.
- **`away_summary` self-reports can be confidently wrong.** The pp7z
  03:15 CDT recap claims active progress one heartbeat cycle after the
  concrete evidence (a failed background task, an API error) already
  contradicted it. Any design that leans on the DRI's own narrated status
  (including a future "ask the model if it's stuck" self-check) must be
  cross-checked against a harder signal, not trusted at face value.
