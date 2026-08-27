
## 3. Dispatch through Codex

Run exactly one creation call:

```bash
ateam dispatch --runtime codex --problem "<one-line problem statement>" \
  [--body-file <tmpfile>] [--repo <absolute-path>] \
  [--base-branch <branch>] [--standby]
```

Omit `--body-file` only when no additional human context exists. Never use `--no-launch`. `ateam dispatch` handles slug creation, worktree creation, initiative registration, and submission of the Codex-native DRI skill through the managed app-server. Work beads remain the DRI's responsibility.
