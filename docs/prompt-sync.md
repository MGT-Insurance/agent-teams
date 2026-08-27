# Prompt synchronization

Shared Claude and Codex agent-team prompts have one deterministic source and a
strict drift check. The contract is in `docs/prompt-sync-contract.md`.

## Source and edit rules

- Edit canonical fragments under `promptsrc/agent-teams/`. Do not hand-edit a
  generated role, agent TOML, paired skill, or paired reference.
- Keep the role and skill manifests under `promptsrc/agent-teams/manifests/`
  exhaustive. Each discovered prompt surface must be paired or explicitly
  classified as runtime-only with a reason.
- Put shared behavior in shared fragments. Put serialization, tool names,
  session mechanics, install paths, and other runtime adapters in runtime
  fragments.
- A singleton does not get a synthetic counterpart. Keep a runtime-specific
  skill or reference classified as runtime-only until a product decision adds
  a real counterpart. An unclassified singleton is drift and fails the check.

## Render and check

Render checked-in outputs after changing a canonical fragment or manifest:

```sh
go run ./cmd/prompt-sync write
```

Then run the strict, read-only comparison:

```sh
go run ./cmd/prompt-sync check
```

Strict mode is the release and CI mode. The `--allow-unmigrated` option exists
only for an active migration loop; do not use it in the workflow or for a
release. The check also rejects missing, duplicate, unknown, unsafe, or
nondeterministic surfaces, so adding a new prompt file requires a manifest
classification in the same change.

Pull requests and pushes to `main` run the strict check in
`.github/workflows/prompt-sync.yml`. A failing check reports the logical entry
and output path. Repository branch-protection settings determine whether that
failing status also blocks a merge.

## Release rules

1. Render outputs and require the strict check to pass.
2. Run the prompt-sync and setup tests that cover the changed surfaces.
3. Run `scripts/build-binaries.sh`. One deterministic build per supported
   platform populates the Claude bin tree, copies it to the Codex bin tree, and
   verifies every platform binary and wrapper byte-for-byte.
4. Choose plugin versions with `docs/plugin-versioning.md`. Update the three
   version-owning manifests together when the release affects both runtimes.
5. Commit the canonical inputs, manifests, rendered outputs, plugin manifests,
   and both binary trees as one complete release.

Never publish a source-only prompt change or rebuild only one plugin's binary
tree. The embedded Codex agent definitions ship inside `ateam`, so stale
binaries can otherwise reinstall old prompts even when repository text is
current.
