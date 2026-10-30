
## 3. Dispatch through Claude

If the human did not say whether to run the DRI in the cloud or locally, ask "cloud or local?" and wait for the answer before dispatching.

Run exactly one creation call. For a local run:

```bash
ateam dispatch --problem "<one-line problem statement>" --body-file <tmpfile> [--repo <abs-path>] [--base-branch <branch>] [--standby]
```

For a cloud run, add `--cloud` and drop `--standby`:

```bash
ateam dispatch --cloud --problem "<one-line problem statement>" --body-file <tmpfile> [--repo <abs-path>] [--base-branch <branch>]
```

A cloud run registers the initiative in the hosted workspace and files a run bead with the Dispatcher, which starts the DRI in a VM from the base branch as pushed to `origin`. It creates no local worktree and no local session, so the controls below don't apply. Its prerequisites are the `cloud_home`, `dispatcher_beads`, and `dispatcher_actor` keys in `$AGENT_TEAMS_HOME/config.toml`, and a repository owned by `MGT-Insurance`. If dispatch reports a missing key, relay the error to the human instead of working around it. `--cloud` doesn't combine with `--no-launch`, `--launch-prompt`, `--standby`, `--topic`, `--model`, or `--advisor`, and supports the Claude runtime only.

`--problem` is the one-line title. `--body-file` carries the additional verbatim human context; dispatch writes schema lines first and appends that context. Omit it only when no additional human context exists. Do not use a no-launch mode. `ateam dispatch` handles slug creation, worktree creation, initiative registration, and the background `/dri` launch. It fails without prompting for an invalid repository, empty slug, worktree collision, or unreadable body file.

The Claude launch runs with `--permission-mode bypassPermissions` because no human is attached to answer prompts. Bypass mode requires one-time interactive acceptance on the machine. Dispatch only well-scoped work, and confirm with the human before dispatching sensitive tooling or infrastructure.
