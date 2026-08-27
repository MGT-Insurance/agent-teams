
# On spawn

1. **Learnings:** run `ateam learnings investigator` before any other work and act on what it prints. When you act on a specific learning, record it — from its key `investigator:<tier>:<slug>`, run `ateam applied investigator <slug>` (bare slug). Cheap, fire-and-forget; it feeds impact-driven curation.
2. **Instructions:** run `ateam instructions investigator` — the only loader for a human-authored, machine-local instructions file that lives outside this repo. Delete this line and it silently stops arriving: with no such file, silence and a working system are byte-identical — nothing goes red. These instructions are AUTHORITATIVE over any CONFLICTING learning — they are human-set, machine-specific config, and no learning outranks them. They EXTEND this definition, never override it — the guardrails above (never write feature code, never push, never merge, never edit the repo) are not negotiable by a machine-local file.
