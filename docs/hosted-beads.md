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
- `<workspace>/.beads/config.yaml` includes `events-journal: true`. The
  `issue-prefix` comes from the database. `bd init` leaves `# issue-prefix`
  commented out in `config.yaml`, and new ids still get the database's prefix.

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

Both halves use the `eric_lloyd_ext` login. Its credentials text is the
`credentials` field of Dispatcher's `GET /api/dolt/databases/credentials`
response, saved as `~/.config/beads/ring1-ext.json`. The sandbox gets it as
`~/.config/beads/credentials`. The laptop gets it as a mode-600 file under
`~/.agent-teams-ring1` (`ext.credentials`), and every laptop process sets
`BEADS_CREDENTIALS_FILE` to that path, so the ring-1 kit never reads or writes
the laptop's `~/.config/beads/credentials`.

V (bd 1.3.0, hosted server): `bd init --server --external` authenticates only
through `BEADS_DOLT_PASSWORD`. It ignores `BEADS_CREDENTIALS_FILE` and
`~/.config/beads/credentials`, so on a fresh directory it fails with MySQL
error 1045. Every other bd command reads the credentials file. The kit's
`hosted_bd_init` (`scripts/sandbox/lib-hosted.sh`) bridges this: it parses the
password for the `[host:port]` section from the file bd uses afterwards and sets
`BEADS_DOLT_PASSWORD` for the `bd init` command only. The password is never in
argv, never printed, and never exported. A missing section or password stops
the script with a message that names the file, not the value.

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

Hosted-only (DRI amendment, ring 1): registration writes the `where:` line
(both `ateam dispatch` and `ateam register`; `register` appends it to the
caller's body only when the body has no `where:` line, first-wins, and
`Host() != ""`), and
`mail send` and `resume` apply the REMOTE rule, only when
`bd.HostedServer(<ATH>)` is ok. On an embedded or local-server workspace both
behave exactly as before, even if a description carries a `where:` line. This
keeps a laptop whose kernel hostname changes with the network (macOS
`os.Hostname()`) from reading its own initiatives as remote. Every ring-1 host
sets a fixed `AGENT_TEAMS_HOST` for the same reason.

## F7 Doorbell filter

One follower per host (DRI amendment, ring 1, replaces the per-session
follower). A journal row `r` is a MESSAGE row iff `r.op == "create"` AND
`r.issue.issue_type == "message"`. Its recipient is `r.issue.assignee`.

The follower rings recipient `ID` iff `ID` is LOCAL to this host:

- `ID == "steward"` -> local.
- Otherwise look up initiative `ID` in `<ATH>` and read `Where` with
  `initiative.Of`. Local iff `Where` is empty, or `Where` is not `host:...`,
  or the F6 REMOTE rule says not remote (`Host() == ""` or the rest equals
  `Host()`).
- Lookup fails (no such initiative, bd error) -> local. A stray doorbell is
  harmless; lost mail is not.

Action: `touch <ATH>/mailbox/<ID>.wake`, the same file `ateam mail send`
touches (`internal/verbs/messaging.go:99`).

V: bd 1.3.0 journal row for `bd create --type=message --assignee=at-xyz` on a
scratch db:
`{"seq":1,"op":"create","issue_id":"jp-wisp-7hw","issue":{"issue_type":"message","assignee":"at-xyz","labels":["delivery:pending"],"ephemeral":true,...}}`.
Message beads are ephemeral wisps.

## F8 Checkpoint

`<ATH>/mailbox/host.seq` holds the highest seq processed (decimal text,
written by rename after every row of any op). One file per host, not per
initiative.

Missing file -> baseline to the journal head with
`bd sql --json "SELECT COALESCE(MAX(seq),0) AS head FROM bd_events_journal"`
(table and column names UNVERIFIED, keystone checks; if the query fails,
replay from `--since 0` and ring only rows whose `ts` is at or after the
follower's start).

`events_journal_truncated` -> continue from floor-1 and touch the doorbell of
every initiative with a live watcher pidfile once (the gap may hide mail for
any of them).

## F9 Follower verb

`ateam mail follow` (no arguments), registered in
`internal/verbs/mail_register.go`. The per-initiative form
`ateam mail follow <initiative-id> --parent-pid N` is removed.

- `AGENT_TEAMS_HOST_FOLLOW=off` in the process env -> exit 0 at once, no
  output, no child process, checked before F3 (DRI amendment). Dispatcher's
  ateam-dri job sets it: a run reads mail at turn end while busy and gets a
  comment poke while parked, so a per-sandbox follower would hold a journal
  connection for nothing. The ring-1 hand-run sandbox (Runbook) leaves it
  unset; its checkpoint needs the follower.
- Not hosted (F3 false) -> exit 0 at once, no output, no child process.
- Hosted -> start a detached child (new session, stdin /dev/null, output
  appended to `<ATH>/debug/mail-follow.log`) and exit 0 at once. The
  caller never waits on the follower.
- The child takes an exclusive non-blocking `flock` on
  `<ATH>/mailbox/host-follow.lock`. Lock held by another process -> exit 0.
  The lock is the singleton; it frees itself when the holder dies.
- Holding the lock: write `<ATH>/mailbox/host-follow.pid`, then run
  `bd -C <ATH> events tail --follow --json --since <checkpoint>`; on any exit
  of bd, back off 1 s, 2 s, 4 s ... capped at 30 s (reset after a row) and
  re-run from the checkpoint.
- Lifetime: every 1 s, count live watchers (`<ATH>/mailbox/*.watcher.pid`
  whose pid passes `kill -0`). After 3 consecutive checks with zero, run the
  EXIT HANDSHAKE: remove `host-follow.pid`, release the lock, then count
  watchers once more. Zero -> exit 0. Non-zero -> try the lock again; got it
  -> rewrite the pidfile and keep following; lock taken -> exit 0 (a newer
  follower owns it).
- SIGTERM -> kill the bd child, remove the pidfile, exit 0.
- Never writes a bead.

Why the handshake closes the start/exit race: a watcher writes its own pidfile
BEFORE it runs `ateam mail follow` (F10). So either the exiting follower's
final count sees that pidfile and keeps running, or it released the lock
before the new child tried it and the new child takes over.

## F10 Watcher wiring

`wake-watcher.sh`, right after it writes its own pidfile, runs
`"$ATEAM" mail follow </dev/null >>"$ATH/debug/mail-follow.log" 2>&1 || true`
in the foreground (it returns at once, F9). The watcher does NOT kill the
follower on exit; the follower outlives any one watcher and exits by the F9
lifetime rule. No hosted check in the shell; F9 makes it a no-op on
non-hosted workspaces.

Restart (DRI amendment): the follower can die while watchers live (killed,
OOM, crash), and nothing else would restart it until a new watcher arms. So
the poll loop also re-checks every `FOLLOW_CHECK_SECS` (default 60) of AWAKE
time, using the loop's existing sleep-gap accounting: if
`<ATH>/mailbox/host-follow.pid` is missing or its pid fails `kill -0`, run the
same `"$ATEAM" mail follow ...` call again. The losing child of any race exits
on the lock (F9). On a non-hosted workspace the pidfile never exists, so this
is one `ateam` exec per minute per watcher that exits at once with no output.
Doorbell and heartbeat exits need no extra call: the re-armed watcher makes
the arm-time call.

When `AGENT_TEAMS_HOST_FOLLOW=off` is set, the watcher makes neither the
arm-time call nor the restart check (F9).

## F11 Ring-1 databases and homes

VALUE PENDING the DRI/human ruling on the global-DB fork; the names below are
the recommended default.

The databases are created by hand in Dispatcher **Settings**, "Additional
databases" (short names `at`, `agent_teams`, `at_dev`; Dispatcher names them
`beads_eric_lloyd_<name>`). Each starts EMPTY, with no beads schema. Two logins
hold grants on them; ring 1 uses only `eric_lloyd_ext`, on both the laptop and
the sandbox (it has no access to `beads_eric_lloyd`). The first
`bd init --server --external ...` against a database (F1) gives it the schema and
mints its `_project_id`. Run against a database that already has the schema, the
same command adopts the existing `_project_id` and changes nothing, so every
workspace setup runs it and none needs a separate "is it initialised" check.
`hosted_workspace` prints `initialized` or `adopted` for each.

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

## Runbook: ring-1 sandbox and the loop-closed checkpoint

This section is hand-run. The kit lives in `scripts/sandbox/`. Run every
command from the root of the worktree that holds the ring-1 branch. The
contract items above (F1, F2, F5, F11, F12) define what the scripts build.

Names used below, from F11: global database `beads_eric_lloyd_at` (prefix
`at`), project database `beads_eric_lloyd_agent_teams` (prefix
`agent-teams`), DB login `eric_lloyd_ext` for both the sandbox and the laptop,
laptop home `~/.agent-teams-ring1`. The sandbox name is `ring1`, so its host
identity is `sandbox-ring1` (F5).

Never read, print, or commit a credentials file, `ring1-ext.json`, the pulled
Vercel env file, or a token. The scripts move the credentials by path or stdin
and never echo them. `scripts/sandbox/.gitignore` covers `.env*`,
`node_modules`, `*.tgz`, and `credentials*`.

### Before you start

1. Create the databases in Dispatcher **Settings**, "Additional databases",
   with the short names `at` and `agent_teams` (`at_dev` is optional). They
   start empty; the kit runs `bd init` on first use (F11). Then, signed in to
   Dispatcher, save the response of
   `https://dispatcher.mgtinsurance.ai/api/dolt/databases/credentials` as
   `~/.config/beads/ring1-ext.json` (mode 600). Its `user` must be
   `eric_lloyd_ext`, and its `credentials` must be the
   `[mgt-spike.dbs.hosted.doltdb.com:3306]` section. `put-credentials.sh`
   checks both and stops with a message if either is wrong.
2. Confirm the join bead is done: `sh scripts/build-binaries.sh` ran after the
   T1-T3 changes and the rebuilt `plugins/agent-teams/bin/` is committed.
3. The laptop half uses the same `eric_lloyd_ext` login as the sandbox.
   `laptop-ring1.sh prepare` reads `ring1-ext.json` (the same validation as
   `put-credentials.sh`), writes a mode-600 `~/.agent-teams-ring1/ext.credentials`,
   and exports `BEADS_CREDENTIALS_FILE` pointing at it. It never reads or writes
   `~/.config/beads/credentials`. Set `RING1_EXT_JSON` to read another file.
   `bd init` ignores the credentials file (F2); the kit passes the password to
   that one command through `BEADS_DOLT_PASSWORD` and nowhere else.
4. Install the SDK once:

   ```sh
   npm install --prefix scripts/sandbox
   ```

5. Pull the OIDC token from the test-only Vercel project `agent-teams-sandboxes`
   in the MGT Insurance team. It has no env vars of its own. Delete it after
   the test. Link a scratch directory to it, then pull its development env into
   the file the kit reads, which is gitignored:

   ```sh
   vercel env pull <worktree>/scripts/sandbox/.env.pulled --environment=development
   ```

   Use an absolute path for `<worktree>`. `--cwd` resolves a relative path
   against the linked directory, not your worktree. The file holds only
   `VERCEL_OIDC_TOKEN`, and it expires after about 12 hours. If `sb.mjs` fails
   with an auth error, pull again.

   Dispatcher creates its own sandboxes with its own deployment's OIDC token
   (dispatcher `docs/plan.md:109`). This project serves the hand-run kit only.

6. Print the network policy and check it:

   ```sh
   node scripts/sandbox/sb.mjs policy --print
   ```

   The output must list `mgt-spike.dbs.hosted.doltdb.com` under `allow`, a
   `subnets.allow` entry with one `/32` per A record (F12), and
   `<redacted>` wherever a token goes. It must not show a token value.

### Step 1: start the sandbox and run a DRI

1. Pack the plugin and create the sandbox. Add `--gh` to `create` so the
   sandbox can clone and push to GitHub with your `gh` login (the token
   stays in the firewall transform):

   ```sh
   sh scripts/sandbox/pack-plugin.sh "$PWD" scripts/sandbox/plugin.tgz
   node scripts/sandbox/sb.mjs create --name ring1 --gh
   ```

   `create` applies the policy and extends the timeout to the 24-hour cap.

2. Copy the files in. `put-credentials.sh` reads `ring1-ext.json` with `jq`
   and pipes the `credentials` text in through stdin with mode `600`, so the
   password never appears in a command line or in output. To check the files
   without a sandbox, run
   `bash scripts/sandbox/put-credentials.sh --dry-run <dir>`; it writes into
   `<dir>` and prints no secret. Set `RING1_EXT_JSON` to read another file.

   ```sh
   node scripts/sandbox/sb.mjs put scripts/sandbox/plugin.tgz plugin.tgz
   node scripts/sandbox/sb.mjs put scripts/sandbox/lib-hosted.sh lib-hosted.sh
   node scripts/sandbox/sb.mjs put scripts/sandbox/install.sh install.sh
   node scripts/sandbox/sb.mjs put scripts/sandbox/start-dri.sh start-dri.sh
   node scripts/sandbox/sb.mjs put scripts/sandbox/resume-dri.sh resume-dri.sh
   bash scripts/sandbox/put-credentials.sh --name ring1
   ```

3. Install. `install.sh` moves the credentials to
   `~/.config/beads/credentials` (mode `600`) and takes the login from
   `ring1.user`. The last lines of the output are the check for F1 and F2. They
   must include one `data path OK: <workspace>` line for the global workspace
   and one for the project clone. Each line comes from a real `bd list` read.
   `bd dolt show` also prints `Server connection OK`, but on bd 1.3.0 it
   connects without TLS, so that line does not prove F2. If a `data path`
   check fails, `install.sh` exits non-zero and names the workspace:

   ```sh
   node scripts/sandbox/sb.mjs exec 'bash ~/install.sh ring1'
   ```

   `install.sh` installs bd 1.3.0, unpacks the plugin, writes the global and
   project workspaces per F1 (each prints `initialized` on the first run
   against an empty database, `adopted` after that), and writes
   `~/run-claude.sh`. The sandbox
   clone of the agent-teams repo comes from `main`; to use another branch,
   prefix the command with `REPO_BRANCH=<branch>`.

4. Start a DRI on a tiny initiative. Pick a task that ends in a PR on a
   scratch branch:

   ```sh
   node scripts/sandbox/sb.mjs exec "bash ~/start-dri.sh 'Fix a typo in README.md. Open the PR from the scratch branch ring1-scratch.'"
   ```

   To watch the session, run
   `node scripts/sandbox/sb.mjs exec 'tmux capture-pane -p -t lead | tail -40'`.

5. Wait for the planner, implementer, tester, and the PR. Record the
   initiative id the DRI registered; the next steps call it
   `<sandbox-initiative>`. Find it with:

   ```sh
   node scripts/sandbox/sb.mjs exec 'ateam initiatives'
   ```

Do not run `git add -A` or `git commit -a` in the sandbox clone. The kit marks
the hosted `.beads/metadata.json` and `.beads/config.yaml` as skip-worktree so
they stay out of git, but a forced add would commit them.

### Step 2: the laptop mails the sandbox

1. Prepare the laptop half. This creates `~/.agent-teams-ring1` and a scratch
   clone at `~/.agent-teams-ring1-work/agent-teams`, both pointed at the
   hosted databases. It does not touch `~/.agent-teams`, the main checkout,
   or the installed plugin:

   ```sh
   bash scripts/sandbox/laptop-ring1.sh prepare
   bash scripts/sandbox/laptop-ring1.sh status
   ```

2. Register a laptop initiative so step 3 has a recipient. Start a session
   with the rebuilt plugin and run `/dri` on a tiny task:

   ```sh
   bash scripts/sandbox/laptop-ring1.sh claude
   ```

   `claude` runs with `AGENT_TEAMS_HOME=~/.agent-teams-ring1`,
   `AGENT_TEAMS_HOST=laptop-ring1`, `BEADS_DOLT_SERVER_TLS=1`,
   `BEADS_CREDENTIALS_FILE=~/.agent-teams-ring1/ext.credentials`, and
   `--plugin-dir` set to this worktree's `plugins/agent-teams`.

   `AGENT_TEAMS_HOST` is fixed on purpose. The macOS kernel hostname can
   change with the network, and `Host()` (F5) would then report a different
   name, so the laptop's own initiatives would read as remote. The script
   exports the fixed value to the claude it launches, to every `ateam` it
   runs, and in the output of `env`. Override it with `AGENT_TEAMS_HOST`
   only if you keep the same value for the whole ring-1 run.

3. In another terminal, with the same env, send mail to the sandbox
   initiative:

   ```sh
   eval "$(bash scripts/sandbox/laptop-ring1.sh env)"
   echo 'ring1 hello from the laptop' > "$TMPDIR/ring1-msg.txt"
   ateam mail send --file "$TMPDIR/ring1-msg.txt" <sandbox-initiative>
   ```

4. Check three results and record each:
   - The send prints the remote-recipient line and does not resume anything
     locally (F6).
   - Within 2 seconds, the doorbell file appears in the sandbox:
     `node scripts/sandbox/sb.mjs exec 'ls -l ~/.agent-teams/mailbox/<sandbox-initiative>.wake'`
   - The sandbox DRI reads the mail. Look for it in the tmux pane.

### Step 3: the sandbox mails the laptop back

1. In the sandbox DRI session, ask the DRI to run `ateam mail send` to
   `<laptop-initiative>` with the text "ring1 hello from the sandbox".
2. On the laptop, check that
   `~/.agent-teams-ring1/mailbox/<laptop-initiative>.wake` appears within 2
   seconds and that the laptop session reads the message.

### Step 4: drop the connection and recover

1. Kill the follower's `bd` child in the sandbox. The follower restarts it
   from its checkpoint (F8, F9):

   ```sh
   node scripts/sandbox/sb.mjs exec 'pkill -f "bd.*events tail"; sleep 1; pgrep -fa "events tail"'
   ```

2. Within the first second of backoff, send mail from the laptop as in step 2.
3. Check that the doorbell file appears and the DRI reads the message. For a
   harder variant, stop the sandbox as in step 5 and send mail while it is
   stopped.

### Step 5: stop and resume the sandbox

1. Stop the sandbox. A stop snapshots the disk and ends every process,
   including teammates:

   ```sh
   node scripts/sandbox/sb.mjs stop --name ring1
   ```

2. Send mail from the laptop while the sandbox is stopped. The message waits
   in the hosted database.
3. Resume. `resume` re-extends the timeout, because a resume resets it to 2
   hours:

   ```sh
   node scripts/sandbox/sb.mjs resume --name ring1
   node scripts/sandbox/sb.mjs exec 'bash ~/resume-dri.sh'
   ```

4. Tell the DRI to respawn its teammates, because teammate processes don't
   survive a stop. Check that the DRI session shows the earlier context and
   that the mail sent during the stop arrives.

### Record the evidence

Add one note to the ring epic (`agent-teams-c4i9.20`) per step with the
command you ran and its output. Redact anything that looks like a secret.

### Teardown

1. Stop and delete the sandbox. A deleted sandbox can't resume:

   ```sh
   node scripts/sandbox/sb.mjs stop --name ring1
   node scripts/sandbox/sb.mjs delete --name ring1
   ```

2. Remove the laptop half:

   ```sh
   bash scripts/sandbox/laptop-ring1.sh teardown
   ```

3. Delete the pulled env and the plugin tarball:

   ```sh
   rm scripts/sandbox/.env.pulled scripts/sandbox/plugin.tgz
   ```

4. Confirm nothing secret is tracked:

   ```sh
   git status --porcelain -uall
   ```

The hosted databases keep the ring-1 beads. Dispatcher owns the databases, so
manage or remove them there.
