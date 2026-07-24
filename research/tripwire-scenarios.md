# Tripwire walkthrough scenarios (at-jolk)

Five concrete end-to-end walkthroughs of the proposed design. Each shows
what the detector observes, what fires, when, and who hears about it.
Ticks are every 5 minutes. "Work-product clock" = time since the last git
change (index/status), commit, or bead update on the initiative's worktrees.
Thresholds: 15 min for a dead session, 30 min for a work-product flatline,
direct alert to Eric past 1 h (set by Eric at plan approval).

## Scenario 1 — the at-pp7z overnight stall, replayed

Bg initiative, implementer working in a track worktree, bead claimed.

| Time | What happens | Old design | New design |
|---|---|---|---|
| 22:00 | Implementer working; stages files every few min | WORKING | Clock resets with each git change — quiet |
| 22:30 | Machine sleep kills the implementer; staged work sits; DRI session waits forever on the dead child and reports **busy** | WORKING (busy = fine) — forever | Clock starts running from the last git change (22:30) |
| 23:00 | Tick: clock at 30 min; mode bg, no gate, bead claimed, no real assistant turns in last 30 min | still WORKING | **Steward wake #1** with evidence: "flat 30 min, bead claimed, session busy" |
| 23:23 | Background tasks report `status=killed` in the transcript | invisible | Failure tokens found → wake note upgraded to urgent |
| 23:30 | Still flat | still WORKING | **Steward wake #2** — steward nudges/wakes the DRI; recovery likely starts here (in reality recovery took ~12 min once woken) |
| 23:30 | If somehow still flat at 1 h | still WORKING | **Direct Telegram alert to Eric** |

Actual incident: Eric's system noticed at 05:28. New design: steward acting
by ~23:05, Eric (worst case) at 23:30 — and in the likely case Eric never
needs to be woken at all.

Key point: the session was **busy** all night, so the old idle-based STUCK
check could never fire no matter the threshold. The work-product clock
doesn't care what the session claims — staged work sitting untouched is the
signal. And the mail blips at 00:18/02:59 don't reset anything, because only
an actual git/bead change resets the clock.

## Scenario 2 — healthy long test run (the false-positive case, on purpose)

Bg initiative, implementer launches a 50-minute test suite inside a claimed
bead. Test output is gitignored — no work-product change while it runs.

| Time | What happens |
|---|---|
| 0:00 | Implementer kicks off the suite, goes quiet; session busy |
| 0:35 | Tick: flat 35 min → **steward wake #1**. Steward looks: session busy, transcript shows a test command launched 35 min ago, zero failure tokens → replies "healthy, watching" and does nothing |
| 0:50 | Suite passes; implementer commits → clock resets → episode over |
| — | **Eric never hears about it** |

This is the accepted cost of the design: an occasional steward wake on long
quiet-but-legit work. The steward absorbs it; Eric is only alerted if a
flatline survives a full hour, which this test run doesn't. (A legit quiet
stretch over 1 h does cost Eric one dismissable ping — his chosen
tradeoff at approval.)

## Scenario 3 — dead session, worktree intact (the at-gusm class)

Bg initiative; the DRI or implementer session dies outright (crash, machine
sleep), worktree still on disk.

| Time | What happens | Old design | New design |
|---|---|---|---|
| 0:00 | Session dies | classified DEAD — **and DEAD is never escalated**; sits until a human notices (at-gusm: ~2 h) | DEAD-with-worktree gets its own durable anchor |
| 0:15 | Tick: dead 15 min | nothing | **Steward wake #1** — steward confirms (SendMessage fails = dead), resumes or respawns |
| 0:20–0:30 | — | nothing | Second wake / direct alert ladder if unhandled |

Machine-sleep wrinkle: if the whole machine slept, the detector slept too —
but the clock is wall-clock timestamps on disk, so the **first tick after
wake** sees the full elapsed time and fires immediately, instead of starting
a fresh count.

## Scenario 4 — initiative parked on a gate (nothing fires)

Bg initiative delivered a PR, raised the review gate; session idles for two
days awaiting Eric's merge.

Every tick: labels carry `human` + `gate:review` → the initiative is
AWAITING-HUMAN and the tripwire never consults the clock at all. Zero
wakes, zero alerts, for question gates and review gates alike. Identical to
today's behavior — the gate exemption is checked first, before anything new.

## Scenario 5 — interactive session, human steps away (excluded by design)

Eric is driving an interactive `/dri` session and leaves for lunch; session
idle 45 min, no gate raised.

Old design: STUCK anchors at 15 min → two steward wakes → a canned Telegram
alert **to Eric, about Eric being away from his own session**.

New design: `mode: interactive` initiatives are outside the mechanical
ladder entirely — nothing anchors, nothing wakes, nothing alerts. The
steward can still see the session in its normal hung-scan snapshot and use
judgment, but no automated escalation ever targets a human's own pacing.
