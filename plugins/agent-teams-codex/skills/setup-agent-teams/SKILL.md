---
name: setup-agent-teams
description: Install and verify agent-teams for Codex, including the standalone managed app-server requirement and the planner, implementer, tester, and reviewer custom agent definitions. Use on a new Codex machine, after installing or updating the agent-teams-codex plugin, or when a custom agent type is missing.
---

# Set up agent-teams for Codex

## Shared workspace setup

Agent-teams requires `bd`; run `bd --version` and stop if it is missing. Resolve the workspace as `${AGENT_TEAMS_HOME:-$HOME/.agent-teams}` and use that literal path throughout. Do not improvise a store or use raw global-workspace `bd` as a replacement for `ateam` after setup.

Ask whether an existing private agent-teams memory remote already exists.

For an existing remote, clone it into the workspace, then initialize Beads from inside that checkout:

```bash
git clone <remote-url> <workspace>
(cd <workspace> && bd init --prefix at --non-interactive)
bd -C <workspace> memories dri
```

`bd init` detects the Git origin and bootstraps `refs/dolt/data`; do not run a separate `bd dolt pull`, which may use a stale configured remote.

For a fresh workspace, create a Git repository, initialize Beads, have the human create a private remote, make and push the initial Git commit, then configure and push the separate Dolt remote:

```bash
mkdir -p <workspace>
git -C <workspace> init
(cd <workspace> && bd init --prefix at --non-interactive)
git -C <workspace> remote add origin <url>
git -C <workspace> add -A
git -C <workspace> commit -m "init agent-teams workspace"
git -C <workspace> branch -M main
git -C <workspace> push -u origin main
bd -C <workspace> dolt remote add origin <url>
bd -C <workspace> dolt push
```

The Git remote carries repository files; the Dolt remote carries Beads data under its separate ref. Normal cross-machine synchronization uses `ateam sync` after the `ateam` wrapper is installed.

1. Resolve this installed plugin's bundled wrapper from Codex's own plugin
   inventory. If `~/.local/bin/ateam` is absent, create a symlink to the
   resolved wrapper. If any filesystem entry occupies that path, including a
   dangling symlink, leave it untouched:

   ```bash
   if ! PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"; then
     printf >&2 'agent-teams: could not resolve the installed agent-teams-codex wrapper; reinstall or update the plugin, then run setup again.\n'
     exit 1
   fi
   if [ ! -x "$PLUGIN_ATEAM" ]; then
     printf >&2 'agent-teams: installed wrapper is missing or not executable: %s\n' "$PLUGIN_ATEAM"
     printf >&2 'Reinstall or update the agent-teams-codex plugin, then run setup again.\n'
     exit 1
   fi
   ATEAM_LINK="$HOME/.local/bin/ateam"
   if [ -e "$ATEAM_LINK" ] || [ -L "$ATEAM_LINK" ]; then
     printf 'agent-teams: left occupied path untouched: %s\n' "$ATEAM_LINK"
   else
     if ! mkdir -p "$HOME/.local/bin"; then
       printf >&2 'agent-teams: could not create parent directory for %s; setup stopped.\n' "$ATEAM_LINK"
       exit 1
     fi
     if ln -s "$PLUGIN_ATEAM" "$ATEAM_LINK"; then
       if [ -L "$ATEAM_LINK" ] && [ "$(readlink "$ATEAM_LINK")" = "$PLUGIN_ATEAM" ]; then
         printf 'agent-teams: created symlink: %s\n' "$ATEAM_LINK"
       else
         printf >&2 'agent-teams: ln succeeded but did not create the requested symlink at %s; the path may have changed during setup. Setup stopped without cleanup.\n' "$ATEAM_LINK"
         exit 1
       fi
     else
       LN_STATUS=$?
       if [ -e "$ATEAM_LINK" ] || [ -L "$ATEAM_LINK" ]; then
         printf 'agent-teams: left path occupied during setup untouched: %s\n' "$ATEAM_LINK"
       else
         printf >&2 'agent-teams: could not create symlink at %s; setup stopped.\n' "$ATEAM_LINK"
         exit "$LN_STATUS"
       fi
     fi
   fi
   "$PLUGIN_ATEAM" ws
   ```

   If inventory lookup or the executable check fails, stop and report the
   plugin installation problem. Do not substitute raw `bd` commands. The
   trusted SessionStart hook follows the same create-only behavior at every
   Codex session boundary. Report whether setup created the symlink or left an
   occupied path untouched. If `~/.local/bin` is not on PATH, tell the human to
   add it. Use the resolved `PLUGIN_ATEAM` wrapper, not the possibly preserved
   `~/.local/bin/ateam` path, for every setup check. Each fenced command below
   repeats the resolution and validation because Codex runs fenced commands in
   fresh shells.
2. The first command block runs the validated wrapper's `ws` command. Report
   the workspace path. If the workspace is not initialized, use the workspace
   create/clone procedure from the agent-teams repository before continuing.
3. Run the shared required compatibility check:

   ```bash
   if ! PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"; then
     printf >&2 'agent-teams: could not resolve the installed agent-teams-codex wrapper; reinstall or update the plugin, then run setup again.\n'
     exit 1
   fi
   if [ ! -x "$PLUGIN_ATEAM" ]; then
     printf >&2 'agent-teams: installed wrapper is missing or not executable: %s\n' "$PLUGIN_ATEAM"
     printf >&2 'Reinstall or update the agent-teams-codex plugin, then run setup again.\n'
     exit 1
   fi
   "$PLUGIN_ATEAM" runtime check codex
   ```

   Stop on failure. Codex initiatives require the official standalone Codex
   installer; an npm or wrapper installation is not compatible with the
   managed app-server contract.
4. Install the bundled custom agent definitions:

   ```bash
   if ! PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"; then
     printf >&2 'agent-teams: could not resolve the installed agent-teams-codex wrapper; reinstall or update the plugin, then run setup again.\n'
     exit 1
   fi
   if [ ! -x "$PLUGIN_ATEAM" ]; then
     printf >&2 'agent-teams: installed wrapper is missing or not executable: %s\n' "$PLUGIN_ATEAM"
     printf >&2 'Reinstall or update the agent-teams-codex plugin, then run setup again.\n'
     exit 1
   fi
   "$PLUGIN_ATEAM" setup codex
   ```

   Do not use `--force` when a definition has local changes unless the human
   explicitly approves replacing those changes.
5. Verify all four files exist under `${CODEX_HOME:-$HOME/.codex}/agents/`:
   `agent-teams-planner.toml`, `agent-teams-implementer.toml`,
   `agent-teams-tester.toml`, and `agent-teams-reviewer.toml`.
6. In Codex, open `/hooks` and inspect the `agent-teams-codex` plugin source.
   Trust its current `SessionStart`, `UserPromptSubmit`, and `Stop` command-hook
   definitions if they are marked for review. Do not claim mail wake is ready
   while the source is skipped, disabled, or awaiting trust. Codex reports a
   changed hook hash at startup and in `/hooks`; after every plugin hook update,
   review the new definition rather than bypassing trust permanently.
7. Tell the human to start a new Codex session. Custom agent, skill, and trusted
   hook discovery occur at the session boundary. Because these hooks are
   plugin-scoped, an untrusted project does not hide them; project-scoped
   `.codex` components still require project trust and must be diagnosed
   separately.

Report the workspace path, Codex compatibility/version, each installed or
up-to-date definition, hook enabled/trusted status, and whether a new session
is still required.
