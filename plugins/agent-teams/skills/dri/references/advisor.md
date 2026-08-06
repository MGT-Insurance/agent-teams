# Consulting your advisor — when to escalate

Read this only when the advisor is enabled (`user_config.use_advisors == true`). When enabled, this session runs on sonnet with an advisor attached via `--advisor` — a more capable second model for consultation on hard calls (model from the per-machine `dri_model` config, default `claude-opus-4-8`; check `/config` to confirm which). The advisor informs; it does not decide and does not own any part of the initiative — every decision, and its consequences, are still yours.

**Consult for:** architectural decisions (costly to reverse); cross-system changes with non-obvious interaction; ambiguous requirements where your best reading is a genuine guess; unfamiliar domains (crypto, auth, consensus...); risky refactors on widely-depended-on code; design tradeoffs where two-plus approaches are defensible and the choice matters; performance-critical paths; security-sensitive changes (auth, secrets, permissions, trust boundaries).

**Do NOT consult for:** trivial/mechanical edits; well-specified single-file changes the contract/plan already dictates; decisions already settled by the contract, plan, or a frozen design; anything resolvable yourself by reading code or spawning an investigator — investigate before escalating, same discipline as with the human.

Genuine judgment forks only, not a rubber stamp — over-consulting wastes the advisor's value and your context budget; under-consulting risks a wrong call on something that mattered. When in doubt: would a wrong guess here be expensive and hard to detect? If not, decide it yourself.

Mid-session: `/advisor` sends it a specific question and returns its answer inline — a pointed ask on one decision, not a running collaborator.
