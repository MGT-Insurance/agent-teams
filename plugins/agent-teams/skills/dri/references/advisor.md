# Consulting your advisor — when to escalate

Read this only when the advisor is enabled (`user_config.use_advisors == true`). When enabled, this session runs on sonnet with an opus advisor attached via `--advisor` — a more capable second model available for consultation on hard calls. The advisor informs; it does not decide and does not own any part of the initiative. You remain the DRI — every decision, and its consequences, are still yours.

**Consult the advisor for:**
- **Architectural decisions** — a structural choice later work will build on and would be costly to reverse.
- **Cross-system changes** — a change spans multiple services/repos/tracks and their interaction isn't obvious.
- **Ambiguous requirements** — the problem statement or contract underspecifies the "what" and your best reading is genuinely a guess.
- **Unfamiliar domains** — the initiative touches ground (crypto, auth, consensus, etc.) you don't have deep priors on.
- **Risky refactors** — a change to widely-depended-on code where a mistake is expensive to detect and to fix.
- **Design tradeoffs with multiple viable approaches** — you can defend two or more designs and the choice materially affects the outcome.
- **Performance-critical paths** — a change on a hot path where a wrong call degrades the product, not just the code.
- **Security-sensitive changes** — anything touching auth, secrets, permissions, or trust boundaries.

**Do NOT consult for:**
- Trivial or mechanical edits — renames, formatting, boilerplate glue.
- Well-specified single-file changes where the contract or plan already dictates the approach.
- Decisions the contract, the plan, or a frozen design already settled.
- Anything you can resolve yourself by reading the code or spawning an investigator — investigate before escalating, same discipline as with the human.

The advisor exists for genuine judgment forks, not a rubber stamp on routine work. Over-consulting wastes the advisor's value and your context budget; under-consulting risks a wrong call on something that mattered. When in doubt, ask: would a wrong guess here be expensive and hard to detect? If not, decide it yourself.

Mid-session: `/advisor` sends it a specific question and returns its answer inline. Use it for a pointed ask on one decision, not as a running collaborator.
