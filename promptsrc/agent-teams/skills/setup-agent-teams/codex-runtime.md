
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
   explicitly approves replacing those changes. Setup also seeds the top-level
   user setting `model_auto_compact_token_limit = 300000` only when that key is
   absent. If the user already set a threshold, setup preserves that override.
   It does not write `model_auto_compact_token_limit_scope`; an explicit scope
   remains untouched, and the absent-key Codex default is `total`. The human
   selected 300000 as approximately 29% of the official 1,050,000-token Sol
   and Terra context capacity. This limits active history; it is not a hard
   size limit for persisted rollout files.

   The root Codex DRI and all five custom agent roles inherit this user-level
   configuration; setup does not copy the setting into role TOMLs. Native
   higher-precedence Codex configuration can still override this default.
5. Verify the compaction configuration without dumping unrelated configuration
   or secrets. This check prints only the threshold and scope:

   ```bash
   CODEX_CONFIG="${CODEX_HOME:-$HOME/.codex}/config.toml"
   python3 - "$CODEX_CONFIG" <<'PY'
   import json
   import sys
   import tomllib

   path = sys.argv[1]
   try:
       with open(path, "rb") as stream:
           config = tomllib.load(stream)
   except (OSError, tomllib.TOMLDecodeError) as error:
       raise SystemExit(f"agent-teams: could not read {path}: {error}")

   limit = config.get("model_auto_compact_token_limit")
   scope = config.get("model_auto_compact_token_limit_scope")
   if not isinstance(limit, int) or isinstance(limit, bool):
       raise SystemExit("agent-teams: model_auto_compact_token_limit is missing or invalid")

   limit_status = "setup default" if limit == 300000 else "preserved user override"
   print(f"model_auto_compact_token_limit = {limit} ({limit_status})")
   if scope is None:
       print('model_auto_compact_token_limit_scope = "total" (Codex default; user key absent)')
   elif isinstance(scope, str):
       print(f"model_auto_compact_token_limit_scope = {json.dumps(scope)} (preserved user override)")
   else:
       raise SystemExit("agent-teams: model_auto_compact_token_limit_scope is invalid")
   PY
   ```

6. Verify all five files exist under `${CODEX_HOME:-$HOME/.codex}/agents/`:
   `agent-teams-planner.toml`, `agent-teams-implementer.toml`,
   `agent-teams-tester.toml`, `agent-teams-reviewer.toml`, and
   `agent-teams-investigator.toml`.
7. In Codex, open `/hooks` and inspect the `agent-teams-codex` plugin source.
   Trust its current `SessionStart`, `UserPromptSubmit`, and `Stop` command-hook
   definitions if they are marked for review. Do not claim mail wake is ready
   while the source is skipped, disabled, or awaiting trust. Codex reports a
   changed hook hash at startup and in `/hooks`; after every plugin hook update,
   review the new definition rather than bypassing trust permanently.
8. Tell the human to start a new Codex session. A new session is required before
   Codex loads the compaction setting, custom agents, skills, and trusted hooks.
   Because these hooks are plugin-scoped, an untrusted project does not hide
   them; project-scoped `.codex` components still require project trust and
   must be diagnosed separately.

Report the workspace path, Codex compatibility/version, each installed or
up-to-date definition, the compaction threshold and scope status, hook
enabled/trusted status, and whether a new session is still required.
