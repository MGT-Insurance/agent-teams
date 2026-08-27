
### Claude session controls

Relay the Claude session identifier when it is available. Give the human these runtime controls:

```bash
ateam runtime open claude
claude logs <session-id>
claude attach <session-id>
claude stop <session-id>
```

When the DRI ends its turn, the background Claude session remains idle rather than stopping itself. Use `claude stop <session-id>` to abort early or reap a finished idle session. Human gates also surface through `ateam human-list` and the initiatives dashboard.

Reference: https://code.claude.com/docs/en/agent-view
