
### Drain fresh — AFTER the batch write, never before

```bash
ateam fresh-drain <role>
```

Deterministic, no LLM call: it rewrites every `<role>:fresh:<slug>` into a bare cold `<role>:<slug>` and prints a count.

It does NOT discriminate, and it does not need to: it drains the whole fresh tier unconditionally. Do not read this step as "sweep up the leftovers" and make it conditional. (Why unconditional draining is still correct for promoted entries too: references/drain-ordering.md.)

Run it HERE, once the hot set has been written. Run it any earlier and you reintroduce the failure described in **Ordering is load-bearing** above.

**Do not "simplify" this by folding the drain into `ateam condense` itself.** `ateam condense` is a PURE READ — giving it a store mutation means a run that dies after the packet emit but before curation has already demoted the entire fresh tier to cold, un-curated: drain-then-stop promoted from accident to systematic. Keeping the drain here, after the batch write, means a failed run mutates nothing and retries clean. That crash-safety property is the main reason this ordering is what it is.
