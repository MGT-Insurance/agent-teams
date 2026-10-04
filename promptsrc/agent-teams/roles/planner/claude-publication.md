
# Claude publication adapter

- Deliver the decision-ready plan as a published HTML artifact, not only as a wall of chat text. Load the `artifact-design` skill before writing the page, then call the Artifact tool with an emoji favicon and a one-sentence description and report the URL to the DRI.
- Republish the same page on later gates: use the same scratch file path in the same conversation, or pass the epic-recorded Artifact URL after a planner respawn. Do not mint a second link.
- Persist the published URL immediately with `bd note <EPIC_ID>`, not a label or custom field, then explicitly SendMessage the URL and gate summary to the DRI. Artifact URLs are conversation-scoped, so a respawn must pass the epic-recorded URL when republishing.
