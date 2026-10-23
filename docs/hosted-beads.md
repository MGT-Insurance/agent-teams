# Hosted beads (ring 1): frozen contract

Reference for ring 1 of the hosted-beads initiative (hosted beads in server
mode plus the journal doorbell). The tracks and the runbook cite these items
by number. Each value is either verified (V, with evidence) or marked
UNVERIFIED and owned by the keystone bead.

## F1 Hosted-mode selector

The selector is per workspace, in files, never in process env.

- `<workspace>/.beads/metadata.json` =
  `{"database":"dolt","backend":"dolt","dolt_mode":"server","dolt_server_host":"mgt-spike.dbs.hosted.doltdb.com","dolt_server_user":"<user>","dolt_database":"<db>","project_id":"<the hosted db's _project_id>"}`
- `<workspace>/.beads/dolt-server.port` = `3306`
- `<workspace>/.beads/config.yaml` includes `events-journal: true` and the
  db's `issue-prefix`.

Made by the Dispatcher-proven recipe (dispatcher origin/main
`src/lib/dolt/commands.ts`, `src/lib/bd.ts` `workspaceFiles`):

```sh
env -u BEADS_DIR bd init --server --external --server-host mgt-spike.dbs.hosted.doltdb.com --server-port 3306 --server-user <user> --database <db> --server-tls --remote '' --prefix <prefix>
env -u BEADS_DIR bd config set events-journal true
printf 3306 > .beads/dolt-server.port ; delete dolt_server_port from metadata.json
```

V: `BEADS_DOLT_SERVER_MODE=1` + `BEADS_DOLT_SERVER_HOST`/`PORT`/`USER` env flip
an EMBEDDED workspace to server mode (bd 1.3.0 `bd dolt show` on a scratch db
printed `Mode: per-project`, `Host: <env value>`). That is why env must NOT
select the mode: it is process-wide and would flip every embedded repo
(midgard) in the same session.

V: there is no env override for the database name (`BD_DOLT_DATABASE`
ignored); the name comes only from `metadata.json` `dolt_database`.

## F2 Secrets and process env

`BEADS_DOLT_SERVER_TLS=1` in the env of every process that runs bd against a
hosted workspace (claude inherits it to hooks). Process-wide is acceptable
only because, on a ring-1 host, every server-mode workspace is hosted.

Password: `~/.config/beads/credentials`, INI section
`[mgt-spike.dbs.hosted.doltdb.com:3306]`, `password = ...`, mode 600
(`bd dolt --help`: `BEADS_DOLT_PASSWORD` -> credentials `[host:port]` ->
empty). No agent reads, prints, or copies it; the human-run kit moves it.

V: the laptop already has that credentials section (section name checked,
value not read).

## F3 Detection, Go

`internal/bd/hosted.go`

```go
func HostedServer(home string) (host string, ok bool)
```

Reads `<home>/.beads/metadata.json`. ok iff `dolt_mode == "server"` AND
`dolt_server_host` is non-empty AND not one of `127.0.0.1`, `localhost`,
`::1`. Missing or unparseable file -> `("", false)`, which keeps today's
behavior.

## F4 Hosted behavior

Implemented by T1. When `HostedServer(home)` is ok:

- `ateam pull` -> runs no bd command; stderr
  `ateam pull: skipped: hosted server <host>; no local replica`; exit 0.
- `ateam sync` -> runs no commit, pull, or push; stdout
  `ateam sync: skipped: hosted server <host>`; exit 0.
- relay tick `doltPull` -> skipped (same as pull). `notify.go` thread-label
  dolt push -> skipped silently.
- The stale-pull guard (`probeInFlightPull`, `terminateHungPull`,
  `pull_stale_unix.go:175-224`) never runs.

## F5 Host identity, Go

`internal/workspace/host.go`

```go
func Host() string // $AGENT_TEAMS_HOST if non-empty, else os.Hostname(), "" on error.
```

The ring-1 sandbox kit sets `AGENT_TEAMS_HOST=sandbox-<sandbox name>`.

## F6 where: line

Key `where`, single-valued, first-wins (`internal/initiative` frozen item 1).

Ring-1 value grammar: `host:<Host()>`. `run:<bead>` is RESERVED for ring 2 and
not interpreted in ring 1. (The plan page wrote `laptop:<host>`; `host:`
replaces it because a sandbox is a host too.)

`internal/initiative.Fields` gains `Where string`; `New` writes
`where: <value>` when non-empty; `Of` reads it. `JSONFields` includes
`"where"` only when non-empty, so existing golden output is unchanged.

REMOTE rule: initiative X is remote to this host iff `Where` starts with
`host:`, `Host() != ""`, and the rest `!= Host()`. Empty, `run:`, or anything
else is NOT remote in ring 1 (today's behavior).

## F7 Doorbell filter

Implemented by T2. A journal row `r` rings initiative `ID` iff
`r.op == "create"` AND `r.issue.issue_type == "message"` AND
`r.issue.assignee == ID`.

Action: `touch <ATH>/mailbox/<ID>.wake`, the same file `ateam mail send`
touches (`internal/verbs/messaging.go:99`).

V: bd 1.3.0 journal row for `bd create --type=message --assignee=at-xyz` on a
scratch db:
`{"seq":1,"op":"create","issue_id":"jp-wisp-7hw","issue":{"issue_type":"message","assignee":"at-xyz","labels":["delivery:pending"],"ephemeral":true,...}}`.
Message beads are ephemeral wisps.

## F8 Checkpoint

`<ATH>/mailbox/<ID>.seq` holds the highest seq processed (decimal text,
written by rename after every row of any op).

Missing file -> baseline to the journal head with
`bd sql --json "SELECT COALESCE(MAX(seq),0) AS head FROM bd_events_journal"`
(table and column names UNVERIFIED, keystone checks; if the query fails,
replay from `--since 0` and ring only rows whose `ts` is at or after the
follower's start).

`events_journal_truncated` -> continue from floor-1 and touch the doorbell
once (the gap may hide mail).

## F9 Follower verb

Implemented by T2: `ateam mail follow <initiative-id> [--parent-pid N]`,
registered in `internal/verbs/mail_register.go`.

- Not hosted (F3 false) -> exit 0 at once, no output.
- Hosted -> run `bd -C <ATH> events tail --follow --since <checkpoint>`; on
  any exit of bd, back off 1 s, 2 s, 4 s ... capped at 30 s (reset after a
  row) and re-run from the checkpoint.
- Exit 0 when `--parent-pid` is gone (checked every second) or on SIGTERM;
  kill its bd child first.
- Never writes a bead.

## F10 Watcher wiring

Implemented by T2: `wake-watcher.sh`, right after it claims its pidfile,
starts `"$ATEAM" mail follow "$match_id" --parent-pid $$ &` and kills it on
every exit path (trap). Always started; F9 makes it a no-op on non-hosted
workspaces, so the shell needs no hosted check.

## F11 Ring-1 databases and homes

VALUE PENDING the DRI/human ruling on the global-DB fork; the names below are
the recommended default.

- Global: hosted db `beads_eric_lloyd_at`, prefix `at`. Laptop uses
  `AGENT_TEAMS_HOME=~/.agent-teams-ring1`; the live `~/.agent-teams` is
  untouched in ring 1. Sandbox uses `AGENT_TEAMS_HOME=$HOME/.agent-teams`.
- Project: hosted db `beads_eric_lloyd_agent_teams`, prefix `agent-teams`,
  used by a sandbox clone and a laptop scratch clone of the agent-teams repo
  (never the main checkout in ring 1; its tracked `.beads/metadata.json`
  stays embedded).

## F12 Sandbox network

The `@vercel/sandbox` policy allows the domain AND `subnets.allow` with the
/32 of every A record of `mgt-spike.dbs.hosted.doltdb.com`. MySQL speaks
first, so a domain-only allow cuts the connection (dispatcher origin/main
`src/runner/sandbox.ts:150-160`, `egressSubnets`).
