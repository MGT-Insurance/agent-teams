# Agent-Teams Dashboard

A local, single-user web dashboard for agent-teams initiatives: an inbox of things
needing you, a live constellation of all teams, and drill-in detail with logs + attach.

## Run it

```bash
cd dashboard
pnpm install   # first time only
pnpm dev       # starts backend (:4823) + frontend (:5173) together
```

Then open **http://localhost:5173**. One command runs both processes (labeled
`server`/`web` in the output); Ctrl-C stops both. The frontend proxies `/api/*`
to the backend, so you only ever visit `:5173`.

## Test it locally

**Try the app** — with `pnpm dev` running, open http://localhost:5173:
- **Inbox** (landing): initiatives needing you — human-gated decisions and PRs
  awaiting merge. Empty = "nothing needs you" (the good case).
- **Constellation**: every initiative as a live node — needs-human flares orange,
  busy pulses blue, delivered breathes green, idle/done dimmed. Click a node to
  drill in. Watch it update live as sessions change state (~2s).
- **Drill-in** (`/initiative/:id`, or click any node/inbox item): notes/history,
  live sessions, work beads, a raw log pane (xterm.js), and an **Attach** button
  that opens a real Terminal running `claude attach <session>`.

There are real initiatives running right now, so the views populate with live data.

**Run the automated tests**:
```bash
cd dashboard
pnpm test        # all suites (server + web) — 115 tests
pnpm typecheck   # strict typecheck across packages
```

---

## API Spec

This document is also the authoritative API surface. The server implements these
endpoints; the web app consumes them.

---

## Endpoint Table

| Method | Path | Response type | Notes |
|--------|------|---------------|-------|
| GET | `/api/snapshot` | `SnapshotEvent` | One-shot; same shape as SSE push. |
| GET | `/api/events` | `text/event-stream` | SSE; pushes `SnapshotEvent` ~2s cadence. |
| GET | `/api/initiatives/:id` | `DrillInDetail` | Full drill-in for one initiative. |
| GET | `/api/initiatives/:id/logs?session=<sessionId>` | chunked bytes | Raw `claude logs` output; pipe to xterm.js. **Do NOT strip ANSI.** |
| POST | `/api/initiatives/:id/attach` | `{ ok: true }` | Body: `{ sessionId }`. Backend shells out via macOS `open`/`osascript` to launch `claude attach` in a real Terminal window. Out-of-browser action. |

---

## SSE Event Catalog

Connection: `GET /api/events` (`Accept: text/event-stream`)

Each SSE frame:
```
data: <JSON-serialised SnapshotEvent>\n\n
```

The `type` field on the `DashboardSSEEvent` union is `"snapshot"`.
Clients reconnect automatically on drop (standard EventSource behaviour).

---

## CLI Data Shapes (Verified — Do Not Re-derive)

### `ateam list-json`
Returns a JSON array. Real JSON keys per element:

```
id, title, description, notes, status, priority, issue_type,
owner, created_at, updated_at, created_by, comment_count,
dependency_count, dependent_count
```

**CRITICAL: there is NO `labels` field** (verified: `jq has("labels") == false`).

Structured fields `repo / worktree / branch / team / mode` and narrative sections
`GOAL / PRIMARY VIEWS / etc.` are embedded as `key: value` TEXT lines inside
`description`. The backend **must parse description text** to extract them.

`notes` is a freeform append log; the latest note is the current phase narrative
and is the source for parked-question text.

### Needs-human source
`ateam human-list` has **no `--json`** (prints human-readable text; `--json` is
silently ignored). The structured path is:

```
bd -C <ateam-ws> list --label human --json
```

The `--label human` filter works server-side and returns human-gated initiatives
(returns `[]` cleanly when none). Note: `bd list --json` **also omits** the
`labels` field from output — the filter itself is the signal, not the field.
The parked question text lives in the latest `notes` entry of the initiative.

Use `ateam ws` to get the workspace path.

### `claude agents --json --all`
Returns an array. Fields present on **all** elements:

```
pid, cwd, kind ("background"|"interactive"), startedAt (epoch ms),
sessionId (uuid), status ("idle"|"busy")
```

Background-only fields: `id` (short), `name`, `state` ("working"|"blocked"|"done"|"stopped").
Interactive sessions have **no** `id / name / state`.

**Join key:** `session.cwd === initiative.worktree` (worktree parsed from description).
Background sessions are the live signal for constellation activity.
An initiative with no matching background session is considered idle.

### `claude logs <id|sessionId>`
Emits **raw TUI terminal output** — ANSI escapes + cursor positioning (screen
replay). Verified: contains `ESC[H`, `ESC[41B`, `ESC[?25l`, etc. `--json` is
ignored. Render with **xterm.js headless**; do NOT strip ANSI.

### PR link
Parse a GitHub PR URL via regex from `notes` / `description` text. There is no
structured field for this.

---

## Workspace Layout

```
dashboard/
  package.json            workspace root
  pnpm-workspace.yaml     declares packages: shared, server, web
  tsconfig.base.json      strict TS base extended by all packages
  README.md               this file
  shared/
    index.ts              barrel re-export
    types.ts              canonical shared types
    api.ts                endpoint constants + request/response types + SSE union
    package.json
    tsconfig.json
  server/                 Track A — CLI-exec + SSE + HTTP server
    package.json          depends on @agent-teams/shared workspace:*
    tsconfig.json         paths: @agent-teams/shared -> ../shared/index.ts
    _stub.ts              temporary import-resolution proof (Track A deletes)
  web/                    Track B — Vite/React app
    package.json          depends on @agent-teams/shared workspace:*
    tsconfig.json         paths: @agent-teams/shared -> ../shared/index.ts
    _stub.ts              temporary import-resolution proof (Track B deletes)
```

---

## Activity / Phase Heuristics (for `InitiativeNode`)

Priority order (first match wins):

1. `needs-human` — initiative appears in `bd list --label human` output
2. `delivered` — PR URL found in notes/description AND session `state === "done"`
3. `busy` — matched background session with `status === "busy"` or `state === "working"`
4. `idle` — matched background session that is not busy
5. `done` — initiative `status` field is closed/done with no live session

Phase token examples: `"executing"`, `"planning"`, `"investigating"`, `"parked"`, `"delivered"`, `"done"`.
Derive from latest `notes` entry text (heuristic keyword match); Track A owns the
parsing logic.
