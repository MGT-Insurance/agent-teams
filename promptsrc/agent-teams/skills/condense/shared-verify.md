
### Verify — re-measure, then iterate

```bash
ateam condense-check <role>
```

Compare the reported `hot_approx_tokens` against `hot_budget_tokens` from the packet. **Both are TOKENS, both come from Go, measured by the tool.** Do not measure bytes, and do not certify the landing against any byte figure.

If the role is still over budget, **iterate — do not accept-and-report**: apply the theme-first forced merge above again, rewrite the batch, re-run the check. Re-measure-and-iterate is the backstop; a run that lands over budget and notes it in the summary has not finished.

**Do NOT buy the budget by evicting curated signal.** If merging genuinely cannot fit the hot set inside the budget, the surplus goes to COLD — still searchable via `ateam recall`, just not injected — never dropped. Shrinking the landing below the budget does not buy meaningful condense frequency either: the frequency lever is the fresh-tier trigger, not the landing target. This has happened before: a run steering to the wrong number dropped entries carrying real signal.

Then confirm the served set and spot-check cold:

```bash
ateam learnings <role>
```

Confirm output shows only the hot entries (the fresh tier is empty after drain), framed by matching `[learnings <role>: ...]` header and trailer lines. If the two disagree, or the trailer is missing, you read a TRUNCATED payload — not a smaller one.

```bash
ateam recall <role> <key-of-an-entry-you-just-demoted>
```

Pass that entry's own `key` **verbatim** (the full `<role>:<slug>`, or just the slug). A healthy store answers `1 matches` with the key you asked for — that exact outcome is the proof, and nothing else is.

Do **not** invent a descriptive phrase. `recall` matches on any *single* token, so a plausible-sounding phrase matches most of the store and reads as a pass while proving nothing — check the **count**, not whether output appeared. Mechanism, the measured 218-of-243 case, and why a `nearest:` line is not a "did you mean": `references/recall-verification.md`.

### Emit summary line

Emit one line per role:

```
<role>: promoted N / merged M / evicted K / hot now X tokens / hot∪fresh Y tokens
```

Where:
- N = number of net-new hot entries (keys that did not previously have `hot:` form)
- M = number of cold entries merged into a single hot entry (count source entries collapsed)
- K = number of cold entries removed via `ateam forget`
- X = `hot_approx_tokens` from the `ateam condense-check <role>` you ran in Verify — read it off the tool, do not estimate it
- Y = `approx_tokens` (the `hot ∪ fresh` union) from that same output. **REPORTED ONLY — never branched on.** It is here so a persistently-high union is visible instead of silent — that condition routes to the aggregate hot-set problem, not to another condense run.

If a role returned zero memories from `ateam condense <role>`, skip it with: `<role>: no memories — skipped`.
