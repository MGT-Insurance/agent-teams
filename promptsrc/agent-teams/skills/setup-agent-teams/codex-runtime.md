
1. Resolve this installed plugin's bundled wrapper from Codex's own plugin
   inventory and pin bare `ateam` to this active install. Do this even when
   `command -v ateam` succeeds; another harness or an older plugin version may
   own that symlink:

   ```bash
   PLUGIN_ATEAM="$(python3 -c 'import json, os, subprocess; d=json.loads(subprocess.check_output(["codex","plugin","list","--json"])); p=next(x for x in d["installed"] if x["name"]=="agent-teams-codex"); print(os.path.expanduser("~/.codex/plugins/cache/{marketplaceName}/{name}/{version}/bin/ateam".format(**p)))')"
   test -x "$PLUGIN_ATEAM"
   mkdir -p ~/.local/bin
   ln -sf "$PLUGIN_ATEAM" ~/.local/bin/ateam
   ~/.local/bin/ateam ws
   ```

   If inventory lookup or the executable check fails, stop and report the
   plugin installation problem. Do not substitute raw `bd` commands. The
   trusted SessionStart hook repeats this link repair on every Codex session.
   If `~/.local/bin` is not on PATH, tell the human to add it and use the
   absolute `~/.local/bin/ateam` path for the remaining checks in this run.
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
5. Verify all five files exist under `${CODEX_HOME:-$HOME/.codex}/agents/`:
   `agent-teams-planner.toml`, `agent-teams-implementer.toml`,
   `agent-teams-tester.toml`, `agent-teams-reviewer.toml`, and
   `agent-teams-investigator.toml`.
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
