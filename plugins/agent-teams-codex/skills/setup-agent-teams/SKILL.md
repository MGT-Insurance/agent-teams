---
name: setup-agent-teams
description: Install and verify agent-teams for Codex, including the standalone managed app-server requirement and the planner, implementer, tester, and reviewer custom agent definitions. Use on a new Codex machine, after installing or updating the agent-teams-codex plugin, or when a custom agent type is missing.
---

# Set up agent-teams for Codex

1. Verify `ateam` is available with `command -v ateam`. If absent, resolve this
   installed plugin's bundled wrapper from Codex's own plugin inventory:

   ```bash
   PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"
   test -x "$PLUGIN_ATEAM"
   mkdir -p ~/.local/bin
   ln -sf "$PLUGIN_ATEAM" ~/.local/bin/ateam
   ~/.local/bin/ateam ws
   ```

   If inventory lookup or the executable check fails, stop and report the
   plugin installation problem. Do not substitute raw `bd` commands. If
   `~/.local/bin` is not on PATH, tell the human to add it and use the absolute
   `~/.local/bin/ateam` path for the remaining checks in this run.
2. Run `ateam ws` and report the workspace path. If the workspace is not
   initialized, use the workspace create/clone procedure from the agent-teams
   repository before continuing.
3. Run the shared required compatibility check:

   ```bash
   ateam runtime check codex
   ```

   Stop on failure. Codex initiatives require the official standalone Codex
   installer; an npm or wrapper installation is not compatible with the
   managed app-server contract.
4. Install the bundled custom agent definitions:

   ```bash
   ateam setup codex
   ```

   Do not use `--force` when a definition has local changes unless the human
   explicitly approves replacing those changes.
5. Verify all four files exist under `${CODEX_HOME:-$HOME/.codex}/agents/`:
   `agent-teams-planner.toml`, `agent-teams-implementer.toml`,
   `agent-teams-tester.toml`, and `agent-teams-reviewer.toml`.
6. Tell the human to start a new Codex session. Custom agent discovery and
   plugin skill discovery occur at the session boundary.

Report the workspace path, Codex compatibility/version, each installed or
up-to-date definition, and whether a new session is still required.
