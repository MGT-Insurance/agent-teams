
### Step 3 — Release the lock

After ALL role processing is complete (whether roles were skipped or condensed), release the lock:

```bash
ateam condense-lock release
```

Release on error paths too — do not leave the lock held. (Exception: the held-skip path in Step 0 never acquired the lock, so no release is needed there.)

If you performed an `ateam sync` (Dolt push) at any point, that sync must also occur within the lock window, before release.
