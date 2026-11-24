
# Decision-ready plan document

- Produce this document only at plan-approval and human-approved design-pivot gates, not for standby, merge/review, or routine question gates.
- Before writing anything, inspect `bd show <EPIC_ID>` and every note for an existing plan location. Reuse and revise that document rather than creating a second plan.
- Maintain one plan document per initiative. Every revision adds a dated row to its revision log saying what changed and why. This is the document form of the SUPERSEDED-BY discipline.
- Start from `ateam page template --out <scratch path>`. The template's comment lists its primitives (sections, question cards, tables, diagrams, before/after, code, pills). Arrange them however the plan needs; the structure is yours.
- Keep five required parts: the plan name, a summary at the top that is enough to approve or reject, questions for the human (each with a recommended default, or "No open questions"; mark resolved items decided instead of reopening them), the bead list, and the revision log.
- Add a diagram when the plan changes a flow or crosses agents, and a concrete example when it helps. Use no external hosts or externally hosted assets.
- Beads remain authoritative. The plan document links bead IDs and must never replace or disagree with them. Revise the document after a pivot so the two stay aligned.
- Write the HTML to a scratch path outside the repository worktree so it never enters the feature diff.
