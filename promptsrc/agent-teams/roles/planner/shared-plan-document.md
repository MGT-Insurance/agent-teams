
# Decision-ready plan document

- Produce this document only at plan-approval and human-approved design-pivot gates, not for standby, merge/review, or routine question gates.
- Before writing anything, inspect `bd show <EPIC_ID>` and every note for an existing plan location. Reuse and revise that document rather than creating a second plan.
- Maintain one plan document per initiative. Every revision must preserve a dated REVISION LOG at the top with what changed and why. This is the document form of the SUPERSEDED-BY discipline.
- The document order is mandatory: REVISION LOG, then exactly these five sections:
  - **S1 SUMMARY:** three to five sentences stating what is being built, why, and the shape of the approach. It must be sufficient by itself to approve or reject.
  - **S2 QUESTIONS FOR THE HUMAN:** visually distinct and near the top. Give each question a recommended default. Mark resolved items DECIDED instead of reopening them.
  - **S3 DIAGRAMS:** Mermaid diagrams in `<pre class="mermaid">`. Use a flowchart for the path being changed, a sequence diagram for cross-agent or process flow, and a graph for architecture. Do not use external hosts or depend on externally hosted assets.
  - **S4 CONCRETE EXAMPLE:** a before/after, sample invocation, or worked trace. This section is required.
  - **S5 DETAIL LAST:** the bead-by-bead decomposition, tracks, dependencies, file lists, and acceptance criteria. Keep it below the decision-ready material.
- Beads remain authoritative. The plan document links bead IDs and must never replace or disagree with them. Revise the document after a pivot so the two stay aligned.
- Write the HTML to a scratch path outside the repository worktree so it never enters the feature diff.
