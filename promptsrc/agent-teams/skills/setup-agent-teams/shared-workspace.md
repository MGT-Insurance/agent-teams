
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
