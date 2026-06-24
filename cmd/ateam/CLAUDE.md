# cmd/ateam — source of the `ateam` binary

This package (plus `internal/`) is the SOURCE of the `ateam` CLI. The binary that
actually runs when the plugin is installed is the **committed** per-platform build
in `plugins/agent-teams/bin/` — your local `go build` and source edits here have
**zero effect on installed sessions** until those committed binaries are rebuilt.

## 🚨 After ANY change that affects CLI behavior, you MUST ship it

A change "affects CLI behavior" if it adds/renames/removes a verb or flag, or
changes output that callers parse. When it does, before the change is done:

1. **Rebuild the committed binaries:** `sh scripts/build-binaries.sh` (builds all
   4 platforms into `plugins/agent-teams/bin/`), then **commit** `plugins/agent-teams/bin/`.
2. **Bump the version** in BOTH `.claude-plugin/marketplace.json` and
   `plugins/agent-teams/.claude-plugin/plugin.json` — keep them identical.
   `claude plugin update` keys off the version; no bump = installed sessions keep
   the cached old copy and silently never see your change.

**No rebuild = the deployed `ateam` silently lacks your change.** A PR that edits
this package but leaves `plugins/agent-teams/bin/` and the version untouched is
INCOMPLETE — verify the rebuilt binary carries your change
(`plugins/agent-teams/bin/ateam-<os>-<arch> <verb>`) before delivering.

## Adding a verb (mechanics)

Implement in `internal/verbs/<verb>.go` as a kong struct with a `Run(*cli.Context) error`
method and struct tags for flags/args/help. Wire it in `RegisterAllKong` (see
`internal/verbs/kong_converted.go`). kong generates help from struct tags — no
separate UsageText entry is required.
