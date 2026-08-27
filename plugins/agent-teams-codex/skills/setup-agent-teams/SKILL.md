---
name: setup-agent-teams
description: Install and verify agent-teams for Codex, including the standalone managed app-server requirement and the planner, implementer, tester, and reviewer custom agent definitions. Use on a new Codex machine, after installing or updating the agent-teams-codex plugin, or when a custom agent type is missing.
---

# Set up agent-teams for Codex

1. Resolve this installed plugin's bundled wrapper from Codex's own plugin
   inventory. If `~/.local/bin/ateam` is absent, create a symlink to the
   resolved wrapper. If any filesystem entry occupies that path, including a
   dangling symlink, leave it untouched:

   ```bash
   PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"
   test -x "$PLUGIN_ATEAM"
   ATEAM_LINK="$HOME/.local/bin/ateam"
   if [ -e "$ATEAM_LINK" ] || [ -L "$ATEAM_LINK" ]; then
     printf 'agent-teams: left occupied path untouched: %s\n' "$ATEAM_LINK"
   else
     mkdir -p "$HOME/.local/bin"
     ln -s "$PLUGIN_ATEAM" "$ATEAM_LINK"
     printf 'agent-teams: created symlink: %s\n' "$ATEAM_LINK"
   fi
   "$PLUGIN_ATEAM" ws
   ```

   If inventory lookup or the executable check fails, stop and report the
   plugin installation problem. Do not substitute raw `bd` commands. The
   trusted SessionStart hook follows the same create-only behavior at every
   Codex session boundary. Report whether setup created the symlink or left an
   occupied path untouched. If `~/.local/bin` is not on PATH, tell the human to
   add it. Use the resolved `PLUGIN_ATEAM` wrapper, not the possibly preserved
   `~/.local/bin/ateam` path, for all remaining setup checks in this run.
2. Use `"$PLUGIN_ATEAM" ws` and report the workspace path. If the workspace is
   not initialized, use the workspace create/clone procedure from the
   agent-teams repository before continuing.
3. Run the shared required compatibility check:

   ```bash
   "$PLUGIN_ATEAM" runtime check codex
   ```

   Stop on failure. Codex initiatives require the official standalone Codex
   installer; an npm or wrapper installation is not compatible with the
   managed app-server contract.
4. Install the bundled custom agent definitions:

   ```bash
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
