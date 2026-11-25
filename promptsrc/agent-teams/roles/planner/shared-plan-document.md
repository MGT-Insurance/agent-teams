
# Decision-ready plan document

- Produce this document only at plan-approval and human-approved design-pivot gates, not for standby, merge/review, or routine question gates.
- Before writing anything, inspect `bd show <EPIC_ID>` and every note for an existing plan location. Reuse and revise that document rather than creating a second plan.
- Maintain one plan document per initiative. Every revision adds a dated row to its revision log saying what changed and why. This is the document form of the SUPERSEDED-BY discipline.
- Start from `ateam page template --out <scratch path>`. The template's comment lists its primitives (sections, question cards, tables, diagrams, before/after, code, pills). Arrange them however the plan needs; the structure is yours.
- Keep six required parts: the plan name, a summary at the top that is enough to approve or reject, questions for the human (each with a recommended default, or "No open questions"; mark resolved items decided instead of reopening them), at least one realistic end-to-end example, the bead list, and the revision log.
- Realistic examples are the most helpful part of a plan for Eric; diagrams and images are next. Give the example real values, not placeholders: the real input, what happens, and the output or failure. Add a before/after where behavior changes.
- Include diagrams when they help: a flowchart for a changed path, a sequence diagram for a cross-process or cross-agent flow, an architecture graph, a screenshot for UI. A plan that changes a mechanism with no diagram gives a one-line reason. For a screenshot, run `ateam page image <file> --alt <text>` and paste its output; images go inline as `data:` URIs, since a local file has no host and the Artifact tool blocks image hosts. The page limit is 900,000 characters. Use no externally hosted assets.
- Beads remain authoritative. The plan document links bead IDs and must never replace or disagree with them. Revise the document after a pivot so the two stay aligned.
- Write the HTML to a scratch path outside the repository worktree so it never enters the feature diff.
