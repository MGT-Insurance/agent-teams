# Plugin versioning

The Claude and Codex plugins use coordinated semantic versions:
`MAJOR.MINOR.PATCH`.

## Invariants

- `agent-teams` (Claude) and `agent-teams-codex` always have the same
  `MAJOR.MINOR` pair.
- The Claude marketplace version and Claude plugin version are always exactly
  identical.
- `PATCH` records runtime-specific releases. It only increases for the affected
  runtime while `MAJOR.MINOR` stays fixed.
- A release that changes both runtimes advances the shared `MINOR` and resets
  both patches to zero.

## Choosing the next version

| Change | Claude next version | Codex next version |
| --- | --- | --- |
| Shared CLI or both plugins change | Advance the shared minor; set patch to `0` | Same version as Claude |
| Claude-only plugin change | Increment Claude patch | Leave unchanged |
| Codex-only plugin change | Leave unchanged | Increment Codex patch |
| Intentional major release | Advance both to the same major; reset minor and patch to `0` | Same version as Claude |

For example, starting from Claude `0.53.1` and Codex `0.53.2`:

- another Codex-only release becomes Codex `0.53.3`; Claude stays `0.53.1`;
- a change shipping in both becomes `0.54.0` for both.

Do not decrement, reuse, or reset a patch within the same shared minor line.
Resetting patch to zero is allowed only when the shared minor or major advances.
Even when two unrelated runtime-specific changes happen in one release, both
plugins changed, so treat it as a shared release and advance the minor.

## Version locations

- Claude marketplace: `.claude-plugin/marketplace.json` at
  `.metadata.version`
- Claude plugin: `plugins/agent-teams/.claude-plugin/plugin.json` at `.version`
- Codex plugin: `plugins/agent-teams-codex/.codex-plugin/plugin.json` at
  `.version`

The two Claude values must match exactly. All three values must obey the
coordination rules above.
