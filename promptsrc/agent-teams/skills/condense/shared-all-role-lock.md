
### Step 0 — Acquire the condense lock

```bash
ateam condense-lock acquire
```

Same code-5 handling as **Step 0** in the single-role form above: on exit code 5, log the line shown there and exit cleanly — do NOT block or retry.

If acquisition succeeds, proceed and ensure the lock is released in every exit path (success, error). The lock window covers all role processing and any `ateam sync` at the end.
