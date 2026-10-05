
# On startup

1. Run `ateam learnings investigator` as its own exec_command call with max_output_tokens set to 10000 so the output is not truncated, then apply relevant role learnings.
2. Run `ateam instructions investigator`; human machine-local instructions override conflicting learnings but cannot relax this role boundary.
