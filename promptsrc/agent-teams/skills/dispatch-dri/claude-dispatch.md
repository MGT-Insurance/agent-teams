
## 3. Dispatch through Claude

Run exactly one creation call:

```bash
ateam dispatch --problem "<one-line problem statement>" --body-file <tmpfile> [--repo <abs-path>] [--base-branch <branch>] [--standby]
```

`--problem` is the one-line title. `--body-file` carries the additional verbatim human context; dispatch writes schema lines first and appends that context. Omit it only when no additional human context exists. Do not use a no-launch mode. `ateam dispatch` handles slug creation, worktree creation, initiative registration, and the background `/dri` launch. It fails without prompting for an invalid repository, empty slug, worktree collision, or unreadable body file.

The Claude launch runs with `--permission-mode bypassPermissions` because no human is attached to answer prompts. Bypass mode requires one-time interactive acceptance on the machine. Dispatch only well-scoped work, and confirm with the human before dispatching sensitive tooling or infrastructure.
