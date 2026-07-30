# Condense contract — frozen shared decisions

Freezes cross-track decisions for `agent-teams-s610` so tracks s610.2 (agents),
s610.3 (dri), s610.4 (steward), and s610.12 (hook marker) don't independently
diverge on duplicated-rule ownership. Produced for bead `agent-teams-s610.1`.
Source plan: `/Users/erlloyd/.claude/jobs/f73b09e9/tmp/condensation-plan.md`
("the plan"). Line numbers below are verified against the actual repo state at
authoring time, not copied blind from the plan.

This bead does NOT condense prose itself — it only freezes who-keeps-what so
the three implementer tracks can proceed in parallel without collision.

---

## 1. THE KEEPER MAP (plan §2, clusters A–N)

### Cluster A — the "Conventions" block, ×4 (implementer/planner/reviewer/tester)

**No single keeper — KEEP IN ALL FOUR, independently compressed in place. Do
NOT extract to a shared `agents/CONVENTIONS.md` or any other shared file.**
Agent `.md` files have no reference-loading mechanism; extracting mandatory
content to a file only a hook or a human would read turns a free inline cost
into a tool call plus a silent-skip failure mode. This is the load-bearing
call in the whole cluster — do not revisit it per-track.

Locations (current): `implementer.md:38–51`, `planner.md:45–57`,
`reviewer.md:37–49`, `tester.md:61–74`.

Compression targets (apply independently in each of the 4 files, same shape
each time):
- **A6 (MEMORY ROUTING, 5 lines → 1):** collapse to one bullet stating: never
  write MEMORY.md or a Claude `memory/` file; role/process learnings →
  `ateam learn <role> <slug> --file <tmpfile>`; user/cross-project prefs →
  `ateam learn user <slug>`; repo-shared project facts → `bd remember`;
  default to `ateam learn`.
- **A7 + A8 (searching / contributing learnings) merge into one "Learnings"
  bullet** (3 lines → 1) — same store, RULE/TRIGGER/APPLY shape survives.
- **A5 (team comms):** compress the ~700-char line to ~380 chars, keeping
  every clause (peer-direct comms, no DRI relay, what escalates, go idle,
  honor shutdown).
- **A2 (CARDINAL two-DBs):** keep the CARDINAL sentence and the "never raw
  `bd -C`" clause verbatim; cut the third restatement ("never redirect
  `bd create` at the global workspace") — it repeats the first clause.

**A3 fix (not a condensation — a gap):** `reviewer.md:41` files discovery
beads with `--label=discovery` and no `--parent`; `planner.md:49`'s own
example does the same, contradicting the parenting rule `planner.md:21`
itself states. **Ruling 5 (GATE ANSWERED): ADD the `--parent <rootEpicId>`
clause to BOTH `reviewer.md` (missing the epic-grouping bullet entirely, per
A3) and `planner.md:49`'s example.** This also resolves Q6. Not optional, not
a judgment call — do it in both files.

### Cluster B — concentric/loop-closing methodology, stated 3×

Locations: `planner.md:21` (~2,100 chars), `dri/SKILL.md:70` (~850 chars,
plus a rationale pointer), `dri/references/execution.md:57–59` ("## Concentric
methodology (loop-closing-set-first)").

- **Keeper (DRI-side): `dri/SKILL.md:70`**, compressed to two sentences,
  folded in. **DELETE `execution.md:57–59` entirely** (pure duplicate of its
  own SKILL.md), and **delete SKILL.md:70's rationale pointer** (`(rationale:
  references/execution.md, "Concentric methodology")`) — pointing at a
  3-line section that says what the sentence already said is a wasted tool
  call.
- **Keeper (planner-side): `planner.md:21`**, compressed to two sentences.
  Cannot point at `dri/references/*` (different skill, agents have no
  reference loader); planner is the party actually performing the
  decomposition. Keep verbatim: "filing or starting an enhancement before the
  loop closes is a process violation, not a judgment call."

### Cluster C — live-verification procedure, stated across DRI files

Locations: `dri/SKILL.md:84–86` (LOOP CLOSED checkpoint), `execution.md:61–69`
("## Live verification procedure"), `execution.md:41` (one-line restatement).
(`tester.md:25–59`, `implementer.md:21–26`, `planner.md:22` are each in their
own lane — not duplication, out of this cluster's scope.)

- **Keeper: `dri/SKILL.md:84–86`.** This is the loop-closure bar; a DRI must
  not have to open a file to know what "loop closed" means.
- **DELETE `execution.md:61–69` entirely.** Fold its one novel clause
  (provision the tester's worktree first if it lacks live env) into
  `SKILL.md:86` as ~7 words, and delete `SKILL.md:86`'s "Full
  spawn/env-provisioning procedure: references/execution.md" pointer only if
  it becomes redundant after the fold — otherwise leave it (it still points
  at genuinely separate spawn/worktree mechanics execution.md retains).
- **`execution.md:41` — ONE canonical loop-closed equation (cross-check
  addendum item 1).** `dri/SKILL.md:84` is canonical. Compress
  `execution.md:41` to name Step 1 (automated gates) and defer Step 2 (live
  verification / the loop-closed definition) to **`dri/SKILL.md` ("LOOP
  CLOSED checkpoint") by name** — do not restate the definition in
  execution.md's own words. This is also Q11's answer (uncontested plan
  recommendation, no gate needed).

### Cluster D — condense procedure, stated 3× (2× in scope)

Locations: `dri/references/wind-down.md:12` (item 8, ~10 lines),
`dri/references/memory.md:31–49` ("## Condensing (autonomous)" through the
"Wind-down touchpoint" paragraph, ~19 lines). `skills/condense/SKILL.md` is
out of scope and unmodified — verified authoritative for the procedure
itself.

- **Keeper: `condense/SKILL.md`** (out of scope) owns the procedure.
- **`wind-down.md:12` keeps the checklist slot only** — compress to one line:
  run `/agent-teams:condense` (no arg) — lock-guarded all-roles drain+condense
  sweep; skips cheaply when nothing is over threshold.
- **DELETE `memory.md:31–49` in full** (both the "## Condensing (autonomous)"
  section and its "Wind-down touchpoint" paragraph) — replace with one line
  pointing at the skill.
- Verified: every path that reaches this material invokes `condense/SKILL.md`
  by name; neither in-scope copy is ever executed directly by the DRI, so
  deleting both loses no behavior.

### Cluster E — the six empty specimen slots + `topic-open`

Location: `steward/references/message-style.md` — "## Not yet calibrated"
section header at line 114; header/rule prose at lines 9–11 (near the top of
the file, not 114 — that's where the operative "do not fabricate" sentence
actually lives); six empty slots (`hung-escalation`, `reply-ack`,
`direct-answer`, `briefing-post`, `briefing-ack`, `anomaly-flag`) at lines
119–158 in the current file, each with a 2-line gloss + `_(No calibrated
specimen yet — awaiting Eric.)_`; `topic-open` at lines 160–168 carries real
content.

- **Keeper: the file's own header rule at lines 9–11** ("Do not fill them in
  from taste, inference, or a draft that has not come back from him — a
  fabricated specimen that later contradicts his taste is worse than a blank
  slot.") — verbatim, it is the operative rule and already states what the
  six per-kind glosses restate.
- **DELETE the six per-kind glosses** (lines 119–158); replace with a single
  one-line list of the six names so the vocabulary stays complete (e.g. "Not
  yet calibrated: hung-escalation, reply-ack, direct-answer, briefing-post,
  briefing-ack, anomaly-flag — awaiting Eric.").
- **KEEP `topic-open` (lines 160–168), compressed** — it carries real content
  (machine-authored at `dispatch.go:331`, deliberately absent from §5's
  table per Eric's own ruling, never the Steward's to compose).

### Cluster F — end-state "do not self-stop"

Locations: `dri/SKILL.md:127` and `dri/references/wind-down.md:20` (item 10)
— near-verbatim, same three terminal conditions, same "do NOT call `claude
stop`."

- **Keeper: `dri/SKILL.md:127`.** Two of the three terminal states never run
  a wind-down at all, so wind-down.md is never opened on those paths;
  SKILL.md is the only copy guaranteed to fire.
- **`wind-down.md:20` → one line:** "10. End the turn — do not self-stop
  (SKILL.md Phase 6, 'End-state')." The checklist slot survives; the
  restated text does not.

### Cluster G — close-out + `update-local-main.sh`

Locations: `dri/SKILL.md:94–98` (Phase 5) and `dri/references/wind-down.md`
item 9 (lines 13–17) — same bash snippet, same fail-soft caveat, same
clear-gate-then-close ordering.

- **Keeper: `dri/SKILL.md:94–98`** — Phase 5 is where the merge happens and
  where the snippet must be to hand.
- **`wind-down.md` item 9 keeps the close-out DECISION only** (close ONLY on
  merge or explicit human close; annotate a long pause, don't close) and
  points at Phase 5 for the command: "On merge, run the Phase 5 close-out
  sequence (clear-gate → close → update-local-main.sh)."

### Cluster H — steward learnings tiers

Locations: `steward/SKILL.md:209` (1 line, already states both facts) and
`steward/references/operations.md:49–51` ("## Learnings tiers", 3 lines).

- **Keeper: `steward/SKILL.md:209`.**
- **DELETE `operations.md:49–51`** ("## Learnings tiers" section) in full;
  fold its one novel clause ("that is what the startup load and the
  SubagentStart hook inject") as one clause into `operations.md:33`'s
  existing "Why the startup load is not enough on its own" paragraph, which
  already makes the adjacent point.
- **Fix `SKILL.md:209`'s dangling pointer:** drop the trailing "Tier
  mechanics: references/operations.md." sentence — there is no longer a tier
  section there, and the line already states the mechanics itself.

### Cluster I — execution-state reassurance

Locations: `dri/SKILL.md:108` (~380 chars of rationale) and
`dri/references/gate-protocol.md:18–31` ("## The review gate and
execution-state", full priority-ordered computation).

- **Keeper: `gate-protocol.md:18–31`.**
- **`SKILL.md:108` compresses to one clause** plus its existing pointer
  (already correctly sectioned: `references/gate-protocol.md ("The review
  gate and execution-state")` — no pointer-format fix needed here, only
  prose compression).

### Cluster J — the cardinal two-databases rule

Locations: `dri/SKILL.md:29` (operative statement + "Full invariant:
references/registry.md"), `dri/references/registry.md:3–5` (the intro
paragraph restating the same invariant before adding audit-enforcement
detail).

- **Keeper: `dri/SKILL.md:29`, PRESERVE VERBATIM** — safety-critical ("NEVER
  create a work bead in the global workspace"); this is what the DRI must
  state to every agent it spawns.
- **`registry.md:3–5` compresses to the audit-enforcement mechanics only**
  (drop the restated invariant sentence itself — SKILL.md:29 is now the sole
  keeper of the invariant statement). This also delivers pointer-contract fix
  item 1 below (new heading `## Audit enforcement`).

### Cluster K — `ateam` tool preamble

Locations: one line each in `implementer.md:6`, `planner.md:6`,
`reviewer.md:6`, `tester.md:6`, `dri/SKILL.md:27`, `steward/SKILL.md:16`
(each correct to keep — small, different loaded context each time); expanded
at `steward/references/operations.md:5–7` ("## The `ateam` binary", 3 lines).

- **Keep all six per-file preamble lines, unchanged.**
- **DELETE `operations.md:5–7` entirely** — pure rationale for a formatting
  choice ("why SKILL.md calls it bare"), read by nobody who needs it.

### Cluster L — gate-escalation spec, stated 3×

Locations: `steward/SKILL.md:174` (the calibrated spec), `SKILL.md:186` (§5
table row — adds the banned list + required first line, novel), and
`steward/references/message-style.md:26–27` (quotes the spec as the rule its
specimen illustrates).

**KEEP ALL THREE, NO CHANGE.** Correct layering, not duplication — a specimen
file that doesn't state the rule it illustrates is unusable.

### Cluster M — steward zero-authority, stated 3× (Q4)

**KEEP ALL THREE, VERBATIM, NO CHANGE.** Deliberate redundancy on an absolute
constraint; ~400 chars total. Not explicitly re-gated by Eric beyond the
plan's own recommendation, but this is single-track (steward only) and
carries no cross-track coordination risk — freezing "keep as-is" here.

### Cluster N — standby reader rule, stated 2× (do-not-touch)

Locations: `dri/SKILL.md:58–62` + `dri/references/registry.md:26–39`.

**KEEP BOTH, NO CHANGE.** Deliberate duplication — `registry.md:34` states
explicitly why both copies exist (writer and reader both copy the rules
verbatim). Do-not-touch list item.

---

## 2. THE POINTER CONTRACT

Every cross-file pointer must be `references/<file>.md ("<exact section
heading>")`, preceded by enough inline text that the reader can decide
whether to open it WITHOUT opening it. Exception (cross-check addendum item
4): a pointer that enumerates its own topics inline is fine even with no
section heading — do not force a heading onto those.

**CORRECTED fix list (supersedes the bead description's original list of
`dri/SKILL.md:78, :80, :86; steward/SKILL.md:209, :225` — that list was
miscalibrated before the cross-check addendum). Fix exactly these six:**

1. `dri/SKILL.md:29` — `"Full invariant: references/registry.md."` →
   `references/registry.md ("Audit enforcement")`. (New heading — see
   Cluster J: wrap the compressed audit-mechanics paragraph in
   `registry.md` with `## Audit enforcement`.)
2. `dri/SKILL.md:46` (Phase 1) — `"command details: references/registry.md"`
   → `references/registry.md ("Commands")`.
3. `dri/SKILL.md:60` (standby gate) — `"command form:
   references/gate-protocol.md"` → `references/gate-protocol.md ("Raising a
   gate")`.
4. `dri/SKILL.md:66` (Phase 2) — `"full field constraints:
   references/gate-protocol.md"` → `references/gate-protocol.md ("Structured
   ask form (primary)")`.
5. `dri/SKILL.md:80` (Phase 4 integration) — `"details:
   references/execution.md"` → `references/execution.md ("Integration
   (DRI-owned)")`.
6. `steward/SKILL.md:225` — `"Full mechanics: references/operations.md."` →
   this one spans the whole file (launch/singleton mechanics, startup-order
   rationale, hung-scan field list, ledger CLI, disabling) — no single
   heading fits. Use the topic-enumeration exception: replace with
   `"Launch/singleton mechanics, hung-scan's full field list, and ledger
   CLI: references/operations.md."` (no heading needed — topics named
   inline).

**WITHDRAWN from the fix list — do NOT re-add, do NOT touch these pointers:**
- `dri/SKILL.md:78/86` — `"Full spawn/worktree/worktree-setup mechanics +
  bypass guardrails: references/execution.md."` — already names its topics,
  fine as-is.
- `steward/SKILL.md:209` — `"Tier mechanics: references/operations.md."` —
  **this exact sentence is separately DELETED by Cluster H** (dangling once
  operations.md's tier section goes), which is a different reason than
  "vague pointer." Do not treat its removal as a pointer-contract fix; it's
  a Cluster H fix.

---

## 3. THE DO-NOT-TOUCH LIST (verbatim, from the bead description)

- The four `model:` frontmatter lines — `hooks/scripts/block-model-divergence.sh`
  awk-parses the FIRST `---` block of `agents/*.md` for the first `^model:`
  line.
- All four never-push/never-merge statements: `implementer.md:35`,
  `planner.md:8`, `reviewer.md:8`, `tester.md:8`.
- The steward Do NOT list (`SKILL.md:10–14`) and §3 Authority
  (`SKILL.md:160–162`).
- The `pr:` field format block (`dri/SKILL.md:112–119`) — pr-shepherd greps
  it.
- The registry description schema (`registry.md:7–18`) — greped by the
  compaction hook, parsed by `internal/initiative`.
- The standby reader rule (`dri/SKILL.md:58–62` + `registry.md:26–39`) —
  deliberately duplicated, `registry.md:34` says so explicitly (= Cluster N).
- `tester.md:45`'s two Playwright gotchas (`--filename=`, YAML snapshot
  refs) — externally cited by `setup-agent-teams/SKILL.md:318`.

---

## 4. THE SIX DESCRIPTION REWRITES

Optimized for shortest-string-that-still-routes-correctly — not an arbitrary
character target. **Over-cutting a description until it stops routing the
skill/agent correctly is a net loss, worse than the tokens saved; if a cut
below feels too aggressive on read-through, prefer the longer form.**

**implementer** (214 chars, was 309):
> Ephemeral implementation agent for agent teams. Claims a beads work item,
> implements it with a few core-path verification tests, runs quality gates,
> and commits — strictly within its assigned worktree. Stops on any design
> ambiguity. Never pushes or merges.

**planner** (261 chars, was 309):
> Expert software planner for agent teams. Investigates a codebase, surfaces
> clarifying questions, and decomposes work into a beads plan with parallel,
> file-disjoint tracks implementers can execute cleanly. Never writes feature
> code. Persistent — stays available for follow-up design questions.

**reviewer** (244 chars, was 259):
> Independent review agent for agent teams. Reviews the full diff against the
> beads spec, hunts duplication, edge cases, security issues, and silent
> failures, and runs the CI-equivalent gate including a real build. Reports
> findings — never fixes code itself.

**tester** (168 chars, was 252):
> Verification agent for agent teams. Runs test suites, authors edge-case and
> E2E tests (implementers write only core-path tests), and owns live
> verification of the running app. Never exposes secrets.

**dri** (404 chars, was 406):
> Act as DRI (directly responsible individual) to deliver a feature or
> initiative end-to-end with a background agent team. Use when asked to "act
> as DRI", "deliver <feature>", "own this initiative", when invoked as /dri
> <problem statement>, or when resuming work in a worktree with an open
> registered initiative. Drives to a pushed branch and an opened PR; merges
> only with the human's explicit confirmation.

(dri's rewrite is effectively unchanged from current — it was already close
to minimal for what it must route on; not worth cutting further at the risk
of losing a trigger phrase.)

**steward** (339 chars, was 611 — placeholder RESOLVED per ruling 2, cut the
zero-authority sentence):
> Act as the Steward — a persistent, machine-scoped background persona that
> watches DRI sessions across every initiative, gates plan/scope/merge/
> design-fork/unblock decisions through Eric, and nudges stalled work. Use
> when invoked as /agent-teams:steward, when running as the machine's steward
> session (cwd carries the steward marker), or when woken by mail addressed
> to the reserved "steward" handle.

All six measured via `node measure.js <file>` (§5) after installing frontmatter
in place — treat the numbers above as the frontmatter `description:` field's
own rendered length, not the whole-file rendered length.

---

## 5. THE MEASUREMENT COMMAND

```
node /Users/erlloyd/.claude/jobs/f73b09e9/tmp/measure.js <file>...
```

Rendered UTF-16 length, **not** `wc -c` — `wc -c` measures disk bytes, which
diverges from the rendered prompt in three ways at once: frontmatter is
stripped from the render (not counted), multibyte UTF-8 runes count as 1 code
unit each (not their byte width), and a `Base directory for this skill:
<abs path>` line (~115 chars) is prepended before rendering. Measure the
rendered value; treat `wc -c` as a loose upper bound only, never as the pass/
fail number.

**The cliff:** truncated iff `Math.round(len/4) > 5000`, i.e.
`rendered_utf16 >= 20002`.

**Today's numbers** (per the bead description, at contract time):
- `dri/SKILL.md`: 19,331 body / ~19,446 rendered (headroom to cliff: 556).
- `steward/SKILL.md`: 18,975 body / ~19,090 rendered (headroom to cliff: 912).

**Hard targets (both SKILL.md bodies):**
- Body `<= 15,885` (rendered `<= 16,000`).
- A 16,000 target leaves `>= 4,000` units = 20% of the 20,002 cap = roughly 8
  substantive plugin changes of runway at the observed growth rate (dri grew
  +474 units in one day, 2026-07-29 → 2026-07-30).

Re-measure after every edit in tracks s610.2/.3/.4 — do not trust the numbers
above once any track has landed changes; they are the pre-condensation
baseline only.

---

## 6. THE HOOK MARKER STRING

Two tracks share this seam: `agent-teams-s610.12` makes
`hooks/scripts/subagent-prime-learnings.sh` EMIT these markers around its
`"$ATEAM" learnings "$role"` call (currently `subagent-prime-learnings.sh:43`
— the marker printed before goes immediately before that line, the marker
printed after goes immediately after it); `agent-teams-s610.2` makes the four
agent role definitions TEST FOR them. If either track picks its own spelling,
detection silently never fires while both tracks' own local checks stay
green — freezing the literal here is the whole point of this section.

**Marker printed BEFORE the learnings output** (`$role` substituted by the
hook with the actual resolved role, e.g. `implementer`):

```
<<<agent-teams-learnings-hook-start role:$role>>>
```

**Marker printed AFTER the learnings output:**

```
<<<agent-teams-learnings-hook-end role:$role>>>
```

**Miss-report line** (printed by an agent when the start/end markers are
absent from its own priming — per the bead's own literal):

```
[learnings-hook-miss] <role>
```

Rationale for this shape: it matches the existing `<<<kind field:value>>>`
envelope convention already used elsewhere in this plugin (steward mail
envelopes — see `steward/references/envelopes.md`), so it reads as a
recognized sentinel rather than a novel format; the `agent-teams-` prefix and
triple-angle-bracket delimiters make collision with ordinary learnings-body
prose (which is RULE/TRIGGER/APPLY shaped, never bracket-delimited) or
ordinary agent output effectively impossible; and the literal greps cleanly
in a transcript.

**THE MATCHING RULE (agent side, `agent-teams-s610.2`).** The frozen string
above contains `$role`, which the hook expands. An agent therefore must NOT
look for that string as written. It matches on the role-independent prefix:

```
<<<agent-teams-learnings-hook-start
```

Presence of that prefix anywhere in the agent's own priming context = the hook
ran; skip the manual `ateam learnings <role>` call. Absence = run it and print
the miss-report line. The end marker exists to bound the learnings block for a
human or a transcript grep; the agent's skip/run decision keys on the START
marker alone, so a truncated context that drops the tail cannot flip the
decision. Both tracks depend on this rule: the hook must emit the prefix
byte-identically, and no agent may hard-code its own role into the test.

**Both markers MUST print unconditionally, including the zero-learnings
case** — a role with nothing stored still gets both `hook-start` and
`hook-end` printed (with empty or minimal content between them). Absence of
the markers must mean **"the hook did not run,"** never **"the role had
nothing stored."** This is the entire point of the feature: an unprimed spawn
must be distinguishable from a primed-but-empty one.

---

## 7. THE DEV-SERVER SENTENCE + HARMONIZED WORKTREE-SETUP WORDING

**Policy (ruling 6):** only the DRI starts dev servers; testers must not.
Eric's verbatim intent: "I only want a DRI to start a dev server. Testers
shouldn't start dev servers right now, it's too hard to find, see, and manage
these."

**THE FROZEN DEV-SERVER SENTENCE** — quote this exact sentence verbatim in
both `agents/tester.md` and the DRI's execution mechanics
(`dri/references/execution.md`, live-verification procedure):

> Only the DRI starts a dev server; testers never start one — they drive and
> observe an instance the DRI has already brought up.

**THE HARMONIZED WORKTREE-SETUP SENTENCE** (ruling 6: harmonize
`implementer.md` and `tester.md` to the SAME wording, CONDITIONAL on the task
actually needing live verification — this supersedes the plan's own
"harmonize UP to the stronger/unconditional wording" recommendation; Eric
chose conditional instead). Quote this exact sentence verbatim in both files,
replacing `implementer.md:13`'s existing conditional wording and
`tester.md:31–37`'s existing unconditional/"mandatory, not optional" wording:

> When the work needs live env — a dev server, creds-dependent validation, or
> a pre-commit hook that requires it — provision the worktree first: `ateam
> worktree-setup <worktree-abs-path>` (after installing dependencies). This
> is the only sanctioned way to run the repo's setup hook; never invoke a raw
> setup script directly, even one a project memory names. Skip it entirely
> when the task needs no live env.

Both sentences may be adapted only for surrounding punctuation/indentation to
fit each file's existing bullet style — the sentence itself must not be
reworded, shortened, or reordered between the two files.

---

## UNRESOLVED — none

Every keeper decision above either traces to an explicit GATE ANSWERED ruling
(1–7, verbatim in the initiative registry `at-zrjt`) or to an uncontested
plan recommendation carrying no cross-track coordination risk (Q3, Q4, Q5,
Q7/Q8 as overridden by ruling 7, Q11, Q12). Nothing here required guessing.

Two items were deliberately NOT re-litigated because Eric already closed
them explicitly: **Q10 ("scaffolding" homonym) STANDS** — implementer.md's
don't-commit-what-you-FOUND rule and tester.md's don't-commit-what-you-MADE
rule are different rules that happen to share a word; keep both, and drop the
shared word in favor of "pre-existing files you did not create" (implementer)
vs. "temporary files you created while working" (tester) — do not merge, do
not pick one file to own the word "scaffolding." **Q9 and Q11 (of the earlier
addendum, i.e. the discovery-bead `--parent` question) are ANSWERED by
rulings 5 and 6** — the earlier "preserve all four files' wording exactly
as-is" instruction on those two points is WITHDRAWN.
