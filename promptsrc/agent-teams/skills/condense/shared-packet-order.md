
### Step 2 — Condense (per FIRE role)

For each role whose verdict was `FIRE`:

```bash
ateam condense <role>
```

> **⚠️ Ordering is load-bearing — `ateam condense` runs FIRST, and `ateam fresh-drain` runs LATER, inside the procedure, after the batch write.**
>
> The packet ships FULL bodies only for entries still tagged `hot:`/`fresh:`; cold entries arrive as key + summary only. So draining first destroys the one signal that separates just-served, un-curated material from long-settled archive: those entries would arrive as summaries, shape-identical to cold, and you would be making the promote-vs-archive call on the highest-stakes entries in the packet without ever seeing their bodies. That is drain-then-stop — the exact failure the promotion rule below exists to prevent. Do not "tidy" this ordering back.
>
> Full mechanism and why printing the drain's key list wouldn't rescue a drain-first order: references/drain-ordering.md.
