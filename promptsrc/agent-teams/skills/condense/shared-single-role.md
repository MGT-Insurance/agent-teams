
### Step 0 — Acquire the condense lock

```bash
ateam condense-lock acquire
```

If this exits with **code 5** (lock held by another session), log:

```
condense in progress elsewhere — skipping, fresh flushes next run
```

Then **exit cleanly** — nothing was acquired, so nothing to release. Do NOT block or retry.

### Step 1 — Condense

On successful lock acquisition, emit the packet for the ONE named role (no gate — an explicit invocation always condenses):

```bash
ateam condense <role>
```

Then apply the condense procedure (Design hot set → Apply batch → **Drain fresh** → Verify → Emit summary) exactly as described in **Condense procedure** below.

**Do NOT run `ateam fresh-drain <role>` here.** The drain is a stage INSIDE that procedure, after the batch write. Running it before `ateam condense` silently blinds the promotion decision — see **Ordering is load-bearing** in the all-roles Step 2 for the mechanism.

### Step 2 — Release the lock

```bash
ateam condense-lock release
```

Release in ALL exit paths (success and error). The held-skip path (Step 0 exit-5) never acquired the lock, so no release is needed there.
