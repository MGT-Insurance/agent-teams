# Prompt synchronization contract

Agent and skill prompts shared by the Claude and Codex runtimes have one canonical repository source. Runtime files are checked-in products of that source, not independent authorities.

## Invariant

`prompt-sync check` deterministically computes the expected Claude and Codex artifacts without writing them. It fails when:

- a generated artifact differs from its canonical inputs;
- an agent, skill, or skill reference is missing, duplicated, or unclassified;
- rendering is unsafe or nondeterministic.

Pull-request CI runs this check as a required status. A shared prompt change cannot merge until every runtime output is current.

## Boundary

Canonical shared content owns role authority and methods, project-state routing, DRI gates and lifecycle, live-verification rules, dispatcher behavior, and decision-ready planner document semantics.

Runtime templates own serialization, model settings, tool and agent names, communication and session mechanics, installation paths, and publication adapters. Claude and Codex-specific content can therefore differ without weakening the shared invariant.

The manifest must enumerate every role definition, agent TOML, `SKILL.md`, and skill-reference Markdown as shared/paired or as an explained runtime-only surface. Adding an unclassified file is a CI failure.

## Verification

The check is valid only if both controls are observed:

1. Canonical inputs and generated outputs pass.
2. Deliberate mutations to a shared role and a shared skill fail with actionable paths, then pass after regeneration.

This contract governs implementation. Work beads can refine file ownership and sequencing but cannot weaken the invariant.
