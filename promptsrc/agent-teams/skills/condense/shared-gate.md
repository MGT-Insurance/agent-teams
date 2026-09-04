
### Step 1 — Gate every role with ONE call

```bash
ateam condense-check
```

That single read-only call enumerates every learning role — skipping `user` and `applied` unconditionally — and prints an aligned table, one row per role. The verdict, `FIRE` or `SKIP`, is in the `VERDICT` column; read the trailing free-text `REASON` for what tripped. `--json` emits the same fields machine-readably. Exit code is 0 regardless of verdict: **the verdict is data, not an exit status.** The verb writes nothing.

(`user:` is served by `ateam prime`, not part of the hot/cold learnings model; `applied:` holds per-slug counters, not learnings, and must never be condensed.)

**Defer to the printed verdict. Do NOT recompute it.** The trigger and its threshold are defined exactly ONCE, in Go — see contract `agent-teams-0yd3.1`, SEAM 2. This file deliberately does not restate the arithmetic, and neither should you: no `wc -c`, no divisor, no threshold comparison of your own. **Prose restatements of a constant desynchronise from it; a printed verdict cannot.** Every token number here is a CLI-computed approximation (divisor frozen by contract `agent-teams-b2xr.2`) — read them off the tool, never re-derive them. (Why hand-recomputing this has already failed in practice: references/trigger-design.md.)

**What the gate measures: NEW MATERIAL, not total size.** A role fires on accumulation in its **fresh tier** — un-curated learnings written since the last condense. Total `hot ∪ fresh` size is **NOT** a trigger. It survives only as a reported number (see **Emit summary line** in the condense procedure below) and must never be branched on. A role whose reported union sits persistently high is an aggregate-hot-set problem, not a condense-frequency problem: surface it, do not condense at it. (Why total size cannot be a trigger — the clearability test, for anyone reconsidering this: references/trigger-design.md.)
