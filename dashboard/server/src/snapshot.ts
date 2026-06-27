// Assembles SnapshotEvent from CLI data and manages the 2s poll loop.

import { existsSync } from "node:fs";

import type { SnapshotEvent } from "@agent-teams/shared";
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

export class SnapshotManager {
  private latest: SnapshotEvent | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;
  private onSnapshot: ((event: SnapshotEvent) => void) | null = null;

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
      const event = await buildSnapshot();
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
export async function buildSnapshot(): Promise<SnapshotEvent> {
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
  // buildInbox consumes the already-built nodes (which carry needsHuman) to avoid re-deriving state.
  const inbox = buildInbox(nodes);

  return { initiatives: nodes, unmatchedSessions, inbox, ts: Date.now() };
}
