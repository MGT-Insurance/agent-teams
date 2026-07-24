# Hung-scan stall tripwire — redesign plan (at-jolk)

Phase-1 deliverable: evidence-backed characterization + proposed design.
No implementation until this plan is approved. Evidence lives in three
companion docs in this directory (`mechanism-verification.md`,
`signal-survey.md`, `transcript-analysis.md`); this doc states the decisions
and keeps the numbers in the appendix.

## What the evidence showed

Three distinct mechanism gaps — all confirmed, none a threshold-tuning
problem:

1. **Busy-blind.** The at-pp7z session almost certainly reported `busy` all
   night — the DRI was waiting on a tool call from a child that died at the
   22:30 machine sleep. STUCK requires *idle*, so the ladder structurally
   could never fire. hung-scan's only liveness input is the opaque
   `claude agents` status; it reads no work product at all.
2. **Anchor amnesia.** One non-STUCK tick erases all accumulated stuck time
   (`hung_scan.go:326`, deliberate per its own comment). The overnight mail
   blips (23:32, 00:18, 02:59, 03:17 UTC — each a real multi-minute burst)
   each reset the clock. No debounce, no episode memory.
3. **DEAD is never escalated.** Only STUCK feeds the wake ladder. at-gusm's
   ~2h stall classified DEAD (cwd-drift, since fixed by at-ps11) and nothing
   mechanical ever escalates DEAD — a crashed session with an intact
   worktree still has no ladder today.

Two ambient findings that shape the design:

- **Mode is parsed but never used** — bg and interactive sessions are
  classified identically (the at-2bkv false-wake class).
- **No audit trail, and the detector runs stale.** hung-scan keeps one
  mutable state file (now `{}`) and logs nothing per tick — this incident
  was nearly unreconstructable. [Corrected after steward review: an earlier
  draft claimed relay wasn't running and the ladder may not have fired.
  Wrong — relay IS running (PID 20444; the process check missed it because
  the binary is named `ateam-darwin-arm64`), and the ladder DID fire (steward
  received the wake at 10:27Z). The lateness was entirely the mechanism gaps
  above.] The true operational finding: relay is an unsupervised singleton
  that has been running the same stale binary since Wed 9am — predating
  0.42.31/32 — so shipped fixes don't take effect until someone manually
  restarts it, and nothing restarts it if it dies.

## Design decisions

**D1 — Add a work-product clock as the primary stall signal.**
Per bg initiative, per tick: measure time since the last work-product
change — git index mtime + `git status --porcelain` hash change on the
initiative's worktrees, last commit time, and bead `updated_at` (already in
the payload hung-scan fetches). Near-free (two file stats + one local git
subprocess per worktree; no global-DB contention). This is the only signal
that catches the busy-forever mode: it would have flagged at-pp7z by ~23:00,
six hours before the actual recovery.

**D2 — Fire on flatline only when the initiative is in an implementation
phase.** Trip condition: work-product flat for 30 min AND mode is `bg` AND
no `human`+`gate:*` labels AND the project repo has a claimed in-progress
bead. The bead gate is load-bearing: planning/investigation phases
legitimately flatline for hours (at-pp7z's own planning phase, the at-gusm
root-cause track). Before waking, corroborate against the transcript tail:
real assistant work turns (content-typed — not queue/system/heartbeat
records) in the last 30 min downgrade the trip.

**D3 — Anchor on durable timestamps, not tick observations.** Stalled-for is
computed from wall-clock artifact timestamps (index mtime survives machine
sleep and detector downtime), and a busy blip no longer resets it — only an
actual work-product change, a gate, or a close does. This deletes the
anchor-amnesia bug rather than debouncing it: a 39-minute failed-recovery
burst that produces no commit no longer looks like recovery.

**D4 — Put DEAD-with-worktree-present on the same ladder.** A dead tied
session whose worktree still exists gets the same durable anchor and
escalation as STUCK. This closes the at-gusm class independently of the
session-tie fix.

**D5 — Scope all mechanical escalation to `mode: bg`.** Interactive
initiatives leave the tripwire entirely (steward-eyeball only). A human
idling 16 minutes is normal; no automated ladder should alert a human about
their own pacing. (Resolves the concern noted on agent-teams-zalv.15.)

**D6 — Ladder pacing for the work-product path: steward wake at 30 min
flatline, second wake per existing ladder, direct Telegram alert only if the
flatline persists past 2 h.** False positives (long test suites, long reads
mid-implementation) hit the steward — cheap, triageable — not Eric. at-pp7z
under this ladder: first wake ~23:00, direct alert ~00:30, vs the actual
05:28.

**D7 — Failure-token corroborator (severity, not trigger).** Grep the
transcript tail for `status="killed"`, `status="failed"`, `Exit code 143`/
timeout, `API Error: Connection closed`: 22 hits in the one confirmed stall,
0 across all four healthy contrasts. n=1, so v1 uses it to upgrade
wake-urgency and enrich the wake note with evidence — not as a standalone
trigger.

**D8 — Append-only tick journal.** One JSONL line per tick per non-WORKING
initiative (classification, clock values, ladder action), size-capped. This
incident was nearly unreconstructable; the next one shouldn't be.

**D9 — Track-worktree registration.** Implementer worktrees (where the
at-pp7z flatline actually lived) aren't discoverable from the registry
today. Extend the at-ps11 pattern: DRIs record a `track-worktree:` line when
spawning implementers; the work-product probe unions registered worktrees.
Fallback heuristic (worktree path contains the initiative id) covers legacy
sessions.

**Out of scope — filed separately, flagged now because it caps everything
above:** `ateam relay` lifecycle. hung-scan only exists inside the relay
process, which is an unsupervised singleton currently running a binary from
before the last two releases — new detection code ships dead until relay is
manually restarted, and nothing restarts it if it dies or the machine
reboots. Recommend a sibling initiative (supervision + restart-on-upgrade,
e.g. launchd or a steward-side relay-version/liveness check). The
durable-timestamp design (D3) at least makes detection resume correctly
after any downtime instead of restarting the clock.

## Tradeoffs, stated plainly

- **False positives:** an implementer legitimately quiet for 30+ min inside
  a claimed bead (long test run, long read) wakes the steward. Accepted:
  wake cost is low, triage is the steward's job, and Eric is only alerted
  after 2 h. The transcript corroborator (D2) suppresses the common cases.
- **False negatives:** stalls during planning/investigation (no claimed
  implementation bead) don't trip the work-product clock — only the existing
  idle-STUCK path and D7 tokens can catch those. Accepted for v1.
  Interactive stalls: out of scope by design (D5).
- **Failure tokens are n=1** — hence corroborator, not trigger.
- **Subagent liveness bookkeeping** (durable parent→child ties) is the
  deeper fix for busy-forever and is deliberately deferred; D1 catches the
  observed failure mode without new bookkeeping beyond D9.

## Implementation sketch (post-approval loop-closing set)

1. Contract bead: state schema (progress timestamps replacing hungAnchor),
   journal format, classification additions (D3/D4 semantics).
2. hung-scan: work-product probe + phase/mode/gate gating (D1/D2/D5).
3. hung-tick: work-product ladder pacing + DEAD ladder + journal (D4/D6/D8).
4. Skill-doc + matcher change for `track-worktree:` (D9).
5. Tests + live verification (simulated flatline drives a real steward
   wake); release protocol (rebuild binaries, version bump).

Single track, file-disjoint from nothing — one implementer.

## Appendix — the load-bearing numbers

- at-pp7z: death 22:30 CDT; silent gaps 180 min + 133 min around one
  35-minute *failed* recovery burst (10-min command timeout, background gate
  failed in 1 s, API connection error); commit finally at 05:40, ~12 min
  after the 05:28 steward wake. Implementer worktree git-flat from 22:30;
  staged work sat uncommitted ~7 h. Bead timestamps bookend the incident
  with zero interior visibility.
- Healthy contrasts: ps11/9qfb max gaps are 240 min (routine heartbeat
  cadence) — *longer* than the stall's worst gap, killing raw gap-length as
  a signal. Whole-session rates, tool_use ratios, text-only ratios: all
  non-separating (stall sits mid-range on every one).
- Separating signatures: heartbeat cycles resolving in 1 turn/0 tools/<25 s
  with accurate "parked" self-report (healthy) vs multi-tool non-converging
  bursts with failure tokens (stall); failure tokens 22 vs 0; work-product
  flatline 7 h vs commits minutes apart (ps11).
- Self-reports lie: pp7z's 03:15 away-summary claimed "gates running, will
  commit" one cycle after the gate had already failed and the API had
  errored. gusm's DRI said "not stalled" 7 minutes before the death. Any
  future self-check design must be artifact-corroborated.
- The two observed death modes look opposite: machine-sleep death is
  silent (zero failure tokens, one giant gap, `SessionStart:resume`);
  live-but-stuck death is noisy (rich failure-token trail, normal-looking
  activity rates). A tripwire needs both the silence clock (D1) and the
  token check (D7).
