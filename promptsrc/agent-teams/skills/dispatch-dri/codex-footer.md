
### Codex managed-runtime controls

Also relay the event-log path. Give the human these runtime controls:

```bash
tail -f <event-log-path>
ateam runtime open codex
ateam show <initiative-id>
ateam human-list
ateam resume <initiative-id> --runtime codex
```

The managed daemon can outlive terminal processes. The durable thread ID is stored as `session:` on the initiative. Mail persists in Beads before waking or steering that same thread; a failed submission leaves the message unread and retryable.
