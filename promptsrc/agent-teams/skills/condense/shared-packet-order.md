
### Step 2 — Condense (per FIRE role)

For each role whose verdict was `FIRE`:

```bash
ateam condense <role>
```

> **⚠️ Ordering is load-bearing — `ateam condense` runs FIRST, and `ateam fresh-drain` runs LATER, inside the procedure, after the batch write.**
>
