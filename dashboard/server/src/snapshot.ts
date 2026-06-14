// Assembles SnapshotEvent from CLI data and manages the 2s poll loop.

import type { SnapshotEvent } from "@agent-teams/shared";
import { CliError, ateamListJson, ateamWs, claudeAgentsJson, bdHumanList } from "./cli.js";
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

  // Fetch initiatives, agents, and human-gated list in parallel.
  const [listJsonRaw, agentsRaw, humanRaw] = await Promise.all([
    ateamListJson(),
    claudeAgentsJson(),
    bdHumanList(ws),
  ]);

  const initiatives = parseAteamListJson(listJsonRaw);
  const sessions = parseClaudeAgents(agentsRaw);
  const humanGatedRaw = parseBdList(humanRaw);
  const humanGatedIds = new Set(humanGatedRaw.map((b) => b.id));

  const nodes = buildInitiativeNodes(initiatives, sessions, humanGatedIds);
  const unmatchedSessions = buildOrphanSessions(initiatives, sessions);
  const inbox = buildInbox(initiatives, humanGatedIds);

  return { initiatives: nodes, unmatchedSessions, inbox, ts: Date.now() };
}
