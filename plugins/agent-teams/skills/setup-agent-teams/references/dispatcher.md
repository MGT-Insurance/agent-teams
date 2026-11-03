# Dispatcher remote live tests (optional setup)

Ask the human: "Do you use Dispatcher for remote live tests?" Default is skip. On skip, write nothing and return to the setup steps; live tests stay local and the plugin behaves as it did before this step existed. Run the rest of this file only on an explicit yes.

1. **Folder.** Default to `~/.config/ateam/dispatcher-beads`. Refuse any path under `~/.agent-teams`: that folder is the workspace git repo, and this folder is not git-ignored there. Ask for the Dispatcher database host, port (3306 unless told otherwise), user, database name, and issue prefix.
2. **Password check.** Confirm that `~/.config/beads/credentials` has a `[<host>:<port>]` section for that host and port, without printing the file. If the section is missing, stop and tell the human to add it. Never ask the human to paste the password into chat.
3. **Init.** Create the folder, then run one `bd init` call whose environment alone carries the password, read from that section's `password` line. Do not echo it, print it, export it into the session, or write it anywhere. `bd -C` fails on a folder that is not yet a beads project, so run init with the folder as the working directory, in one command, with the password as a prefix assignment on `bd init` only:

   ```bash
   (cd <folder> && BEADS_DOLT_PASSWORD=<value read from the section> bd init --server --external \
     --server-host <host> --server-port <port> --server-user <user> --database <db> \
     --server-tls --remote '' --prefix <prefix> --non-interactive --skip-hooks)
   ```

4. **Verify with no password set.** `BEADS_DOLT_SERVER_TLS=1 bd -C <folder> list --limit 1` must succeed. If it fails, report the error text and stop; do not write the config.
5. **Config.** Write `dispatcher_beads = <folder>` and `dispatcher_actor = <the engineer's email>` to `~/.agent-teams/config.toml`. Confirm with `ateam ui-test file --help`.
