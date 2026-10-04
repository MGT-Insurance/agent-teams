
This emits a JSON packet to stdout:

```json
{
  "role": "<role>",
  "memories": [
    {"key": "hot:<slug>", "body": "...", "applied_count": 3, "last_applied": "2026-07-20"},
    {"key": "fresh:<slug>", "body": "..."},
    {"key": "<slug>", "summary": "..."}
  ],
  "hot_budget_tokens": 6000,
  "instruction_contract": "..."
}
```
