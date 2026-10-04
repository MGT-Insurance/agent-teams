
### Apply (batch write, then cleanup)

Create a unique session-scoped temp directory so parallel condense runs cannot clobber each other:

```bash
DIR=$(mktemp -d)
```

For each entry in your decided hot set, write to a file under `$DIR` and promote:

```bash
printf '%s' "<hot body>" > "$DIR/<slug>.txt"
ateam learn <role> hot:<slug> --file "$DIR/<slug>.txt"
```

After ALL hot entries are written, handle cold cleanup:
- DEMOTE stale hot items to cold: `ateam learn <role> cold:<slug> --file <f>` then `ateam forget <role> hot:<slug>`.
- Within cold: MERGE duplicates or REWRITE for brevity via `ateam learn <role> cold:<slug> --file <f>`; EVICT truly-dead items via `ateam forget <role> <slug>`.
- LEAVE IN COLD any learning not promoted (the long tail stays searchable, not injected).
- EVICT ONLY exact duplicates or clearly-superseded items. When in doubt, keep in cold. Conservative: NO eviction floor, but evict little.
