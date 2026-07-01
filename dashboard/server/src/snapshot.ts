// Assembles SnapshotEvent from CLI data and manages the 2s poll loop.

import { existsSync } from "node:fs";

import type { SessionState, SnapshotEvent } from "@agent-teams/shared";
import {
  CliError,
  ateamListJson,
  ateamWs,
  claudeAgentsJson,
  bdHumanList,
  bdClosedInitiatives,
} from "./cli.js";
import {
  parseAteamListJson,
  parseClaudeAgents,
  parseBdList,
  buildInitiativeNodes,
  buildOrphanSessions,
  buildInbox,
} from "./parse.js";

const POLL_INTERVAL_MS = 2_000;

// Per-session transition bookkeeping (agent-teams-ni2y.8). sessionId -> last-seen
// (status, state) pair plus the epoch ms it last changed. Server-internal only —
// never serialized, resets on restart (acceptable graceful degradation).
export type TransitionEntry = { status?: string; state?: string; lastTransitionAt: number };
export type TransitionMap = Map<string, TransitionEntry>;

// Pure helper: given this tick's sessions and the previous tick's transition map,
// return a sessionId -> lastTransitionAt lookup and MUTATE prev in place to become
// the new "previous" map for next tick (first-sighting / flip / unchanged / prune).
export function stampTransitions(
  sessions: SessionState[],
  prev: TransitionMap,
  now: number,
): Map<string, number> {
  const lookup = new Map<string, number>();
  const seen = new Set<string>();

  for (const session of sessions) {
    seen.add(session.sessionId);
    const prior = prev.get(session.sessionId);
    let lastTransitionAt: number;
    if (prior === undefined) {
      // First sighting: use startedAt to avoid a restart thundering-herd where
      // every session would otherwise stamp "now" and the sort collapses.
      lastTransitionAt = session.startedAt ?? now;
    } else if (prior.status !== session.status || prior.state !== session.state) {
      // (status, state) changed since last tick — the transition Eric wants to surface.
      lastTransitionAt = now;
    } else {
      // Unchanged — keep the prior stamp (no spurious rise).
      lastTransitionAt = prior.lastTransitionAt;
    }
    prev.set(session.sessionId, { status: session.status, state: session.state, lastTransitionAt });
    lookup.set(session.sessionId, lastTransitionAt);
  }

  // Prune sessionIds that vanished from this tick's snapshot (bounded memory).
  for (const sessionId of prev.keys()) {
    if (!seen.has(sessionId)) prev.delete(sessionId);
  }

  return lookup;
}

export class SnapshotManager {
  private latest: SnapshotEvent | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;
  private onSnapshot: ((event: SnapshotEvent) => void) | null = null;
  // Cross-poll continuity for stampTransitions (agent-teams-ni2y.8).
  private readonly transitions: TransitionMap = new Map();

  // Kick off the poll loop and immediately build the first snapshot.
  start(onSnapshot: (event: SnapshotEvent) => void): void {
    this.onSnapshot = onSnapshot;
    void this.poll();
    this.timer = setInterval(() => void this.poll(), POLL_INTERVAL_MS);
  }

  stop(): void {
    if (this.timer !== null) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }

  getLatest(): SnapshotEvent | null {
    return this.latest;
  }

  private async poll(): Promise<void> {
    try {
      const event = await buildSnapshot(this.transitions);
      this.latest = event;
      this.onSnapshot?.(event);
    } catch (err) {
      // Poll failures are logged but do not crash the server.
      // The latest valid snapshot remains available.
      if (err instanceof CliError) {
        console.error(`[snapshot] CLI error: ${err.message}`);
      } else {
        console.error("[snapshot] unexpected error:", err);
      }
    }
  }
}

// Build one SnapshotEvent by calling all CLIs in parallel where possible.
// transitions: the SnapshotManager's cross-poll map (agent-teams-ni2y.8). Omitted by
// ad-hoc/endpoint-fallback callers (index.ts, before the first poll) -> buildInbox
// degrades gracefully to lastActivityAt = updated_at.
export async function buildSnapshot(transitions?: TransitionMap, now = Date.now()): Promise<SnapshotEvent> {
  // ateam ws is a prerequisite for bdHumanList, so fetch it first.
  const ws = await ateamWs();

  // Fetch open initiatives, closed initiatives, agents, and human-gated list in parallel.
  const [listJsonRaw, closedJsonRaw, agentsRaw, humanRaw] = await Promise.all([
    ateamListJson(),
    bdClosedInitiatives(ws),
    claudeAgentsJson(),
    bdHumanList(ws),
  ]);

  const openInitiatives = parseAteamListJson(listJsonRaw);
  const closedInitiatives = parseAteamListJson(closedJsonRaw);
  const sessions = parseClaudeAgents(agentsRaw);
  const humanGatedRaw = parseBdList(humanRaw);
  const humanGatedIds = new Set(humanGatedRaw.map((b) => b.id));

  // Concat open + closed before building nodes. Dedup defensively by id (a bead is
  // either open or closed, never both, but guard anyway — open wins). Closed
  // initiatives derive delivery="merged" -> needsHuman=false, so they never reach
  // the inbox; they exist so the Initiatives tab can offer a "show closed" toggle.
  const seen = new Set<string>();
  const initiatives = [...openInitiatives, ...closedInitiatives].filter((i) => {
    if (seen.has(i.id)) return false;
    seen.add(i.id);
    return true;
  });

  const nodes = buildInitiativeNodes(initiatives, sessions, humanGatedIds, existsSync);
  const unmatchedSessions = buildOrphanSessions(initiatives, sessions);
  // Session-transition-aware recency (agent-teams-ni2y.8): stamp this tick's transitions
  // when a map was threaded in (the poll loop), else leave undefined for graceful degrade.
  const sessionTransitions = transitions ? stampTransitions(sessions, transitions, now) : undefined;
  // buildInbox consumes the already-built nodes (which carry needsHuman) to avoid re-deriving state.
  const inbox = buildInbox(nodes, sessionTransitions);

  return { initiatives: nodes, unmatchedSessions, inbox, ts: Date.now() };
}
