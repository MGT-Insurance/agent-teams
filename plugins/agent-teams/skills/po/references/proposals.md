# Recommending new work

You turn what you learn into work that should happen. Where it goes depends on one question: is it inside the charter?

## Inside the charter

Work counts as inside the charter when it serves a goal in the charter, touches a feature it names, stays in its repos, and crosses none of its fences. Propose it to the steward, who checks for duplicates and launches it. You do not start DRIs yourself.

Before you propose, read what is already in flight (`ateam list`, `ateam show`) and what you have already said (`ateam po state list <area> --prefix said:`). If the same work is already running or already proposed, say nothing new.

A proposal is a mail bead assigned to `agent:steward`. The body is exactly this shape, one field per line:

```
kind: po-proposal
repo: <repo the work belongs in>
problem: <what is wrong or missing, in one or two sentences>
goal: <the charter goal id this serves>
evidence: <what you saw, and where>
po: <your area>
---
<free text: what you would have the DRI do, what done looks like, anything the steward should know>
```

The first line, the field names, their order and the `---` line are fixed. The steward reads them by name. Do not rename, reorder or add fields. Put everything else in the free text after the line.

Make the proposal specific enough that a DRI could start from it. State the problem and the evidence, not a task list for the DRI's team. The DRI decides how.

After sending, record what you said in `po:<area>:said:<initiative-id>` when an initiative exists, or in your ledger if it does not yet. Write a ledger row with category `new-work`, with what you recommended. When Eric or the steward later corrects the call, record that too.

## After the steward launches it

The steward tells you when the work starts. Look at the new initiative. If it is a clear fit for a charter feature and goal, claim it: write the claim with `ateam po state set <area> claim:<initiative-id>` (fit `clear`, a reason, and lead true when your charter names the feature), and record a `claim` ledger row. Say in your reason what in the charter ties it to your area. If the fit is only possible, claim it as `possible` and say what you are unsure of.

From then on you follow it like any claimed work. You are never a gate on it.

## Outside the charter

If the work serves no charter goal, touches a feature the charter does not name, or crosses a fence, do not send it to the steward. It becomes a conversation with Eric in your topic, the same as a bold proposal (see `vision.md`): the idea, the evidence, the cost, the first small test. If he agrees, the charter changes first, by a pull request he merges. Only then does the work become in-charter and go to the steward.

Small and outside the charter is still outside. Ask.

## Ledger

Record in-charter recommendations with category `new-work`. Record bigger ideas with `bold-proposal` and charter drafts with `charter-change`. Record the verdict, accepted or corrected, when the answer arrives, and save a lesson with `ateam learn` when you were corrected.
