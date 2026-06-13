# Tester-agent improvement — spec (locked with Eric, 2026-06-13)

Goal: improve the agent-teams `tester` agent so it (and a DRI's team) can run and live-test apps like shadowfax/mithril with minimal human intervention, with knowledge placed in the right home so nothing goes stale or sprawls.

## The four homes (LOCKED — confirmed by Eric via concrete examples)

| Home | Holds | Test that decides it |
|---|---|---|
| **Tester agent definition** (this repo) | *How the tester works* | cross-project behavior/style |
| **Repo doc** (the app's own repo) | *How the app runs + how to invoke its features* | intrinsic, everyone-needs, lives with code |
| **Tester memory** `tester:<project>` (agent-teams workspace) | *How to operate as a tester against this app* | a coordination constraint / gotcha the tester acts on |
| **DRI / initiative** | *What "correct" means* (domain pass/fail) | per-feature, supplied at delegation or learned in-initiative |

Worked placements that fixed the model:
- "run-and-watch server output / reversible logging / Playwright MCP" → tester agent definition.
- "shadowfax :3000, single-instance only" → tester memory (coordination constraint).
- "nvm use && pnpm install && pnpm build:packages && pnpm dev:shadowfax; pnpm env:pull" → repo doc.
- "exercise Athena without Slack via /api/chat/athena" → repo doc (but see Build Targets: NOT doing this now).
- "valid quote rates > $0; $0 locally = Socotra callback reachability" → DRI/initiative (domain). The generic tester is DOMAIN-BLIND.

## Tester agent definition — behaviors to add (this is the bulk of the work)

1. **Owns running servers for real verification** — starts/manages a live instance, not just unit tests.
2. **Knows the repo's server cardinality** — some repos run N instances on N ports; others (e.g. midgard today) run exactly ONE at a time. (The specific cardinality is a per-project fact, in tester memory / repo — not hardcoded in the agent.)
3. **Port-occupancy = coordinate, don't clobber** — check the expected port first. If already up, do NOT blindly free-port/kill it (could be the human's instance or another agent's). Reuse if appropriate, else stop-and-surface. Only kill what the tester itself started. (Future: real cross-tester coordination.)
4. **Owns lifecycle cleanup** — tears down servers it started + orphaned test workers (kill by explicit PID, scoped to its own runs — never pkill-by-name; see global CLAUDE.md).
5. **Aggressive REVERSIBLE logging** — add logging liberally to diagnose, then REMOVE it before finishing (scoped logger or a single `[DEBUG-X]` prefix; ephemeral only).
6. **Log visibility is mandatory** — reads the **server process** output, and for web apps the **browser** console/network.
7. **Playwright MCP is REQUIRED for any work that might change a web app** — drive/observe the real UI through it. If the Playwright MCP isn't working in those cases, **flag to the human** — never silently skip or hand-roll around it (consistent with "request tools, don't work around them").
8. **Auth/setup when capable; stop-and-ask only at a real wall** — if it has the means (available creds, a documented non-interactive path), it authenticates / pulls env / starts services itself. It does NOT reflexively defer to the human. Stop-and-ask only when genuinely blocked: missing creds it can't obtain, or an interactive-only browser SSO it can't complete unattended. ("Human did setup" is an acceptable fallback, not a prohibition.)
9. **DOMAIN-BLIND** — the generic tester does not know what a "correct" result means for any domain. Pass/fail criteria come from the DRI/initiative.

## Operating model (the protocol the agent runs)
pre-flight (verify prereqs/services; satisfy what it can with available info/creds; stop-and-ask only at a real wall)
→ attach-or-start (reuse an already-running instance — often the human's, esp. under single-instance; else start one if cardinality allows; never kill what it didn't start)
→ test (Playwright MCP for web; aggressive reversible logging; read server + browser logs; pass/fail from the DRI — Layer "domain")
→ clean up only what it started (servers + orphaned test workers).

## Consult-your-sources protocol (how the tester gathers project knowledge)
On a project, the tester: (1) recalls `tester:<project>` memory (coordination lore + pointers to canonical repo docs), (2) reads the EXISTING repo run/test docs those pointers name, (3) takes domain pass/fail criteria from the DRI.

OPEN INTEGRATION DETAIL to resolve in implementation: how a freshly-spawned tester **auto-recalls** `tester:<project>` memory on spawn (lean: auto-recall so the tester is useful even if a DRI forgets; the DRI can add criteria on top) vs DRI-primed.

## Build targets

1. **agent-teams plugin (~/Code/agent-teams): extend the `tester` agent definition** with behaviors 1–9 + the operating model + the consult-your-sources protocol. THE BULK.
2. ~~New midgard run/test runbook~~ — **DROPPED as redundant.** A verification agent confirmed 5/6 run/test items are ALREADY adequately documented in midgard (start commands: root CLAUDE.md/README; env:pull: README + docs/agent-setup.md; ports: root CLAUDE.md map + ngrok/CLAUDE.md; e2e: packages/e2e-tests/README; backend: apps/mithril_backend/README + Makefile). The tester relies on these via its protocol; `tester:midgard` memory carries POINTERS to them. No new doc.
3. **workspace: seed `tester:midgard` memory** = coordination lore (single-instance / expect-already-running-server / port occupancy; flaky-auth-re-run; Playwright-MCP-hangs → close-mcp-chrome) + doc-location pointers (the canonical files in #2).
4. ~~"Invoke Athena without Slack" consolidation doc~~ — **LEFT OUT per Eric** (the one real repo gap, but deferred to whoever next touches mithril). The HTTP entrypoints exist scattered (Next.js /api/chat[+/api/athena/chat], backend /chat-athena, scripts/AGENT_RUNNER.md) if needed later.

## Scope / non-goals
- The tester assumes a human-provisioned machine is FINE (one-time auth/secrets/ngrok by the human is acceptable); bare-repo-to-running is a future ideal, not the bar.
- This initiative is independent of the specialty-quote-api PR #3551 (awaiting-merge); it touches ~/Code/agent-teams (tester definition) + the agent-teams workspace (seed tester:midgard memory). No midgard repo changes.
