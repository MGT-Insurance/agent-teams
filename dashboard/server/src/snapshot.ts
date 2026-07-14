// Assembles SnapshotEvent from CLI data and manages the poll loop.

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

// 15s (not 2s): each tick fires 4 subprocess calls against the global bd
// workspace, which serializes on an advisory flock (at-6nj). 2s caused
// lock-contention pileups under concurrent load.
const POLL_INTERVAL_MS = 15_000;

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
  // Per-slice last-known-good raw CLI output, for partial-failure tolerance
  // (agent-teams-assa.1) — carried across ticks so a killed subprocess call
  // falls back to the previous good value instead of blanking that slice.
  private readonly lastGood: LastGoodSlices = {};
  // True while a poll() is awaiting buildSnapshot — guards against overlapping
  // ticks stacking under transient contention (agent-teams-assa.1).
  private inFlight = false;
  // Counts consecutive skipped ticks so the skip is logged once per streak,
  // not once per tick, while a backlog persists.
  private skipStreak = 0;

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
    if (this.inFlight) {
      this.skipStreak++;
      if (this.skipStreak === 1) {
        console.warn("[snapshot] tick skipped: previous poll still in flight");
      }
      return;
    }
    this.inFlight = true;
    this.skipStreak = 0;
    try {
      const event = await buildSnapshot(this.transitions, Date.now(), this.lastGood);
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
    } finally {
      this.inFlight = false;
    }
  }
}

// Per-slice last-known-good raw CLI JSON, keyed by slice name. Threaded through
// buildSnapshot the same way `transitions` is: an optional mutable map the
// caller owns and reuses across ticks (agent-teams-assa.1).
export type LastGoodSlices = {
  listJsonRaw?: string;
  closedJsonRaw?: string;
  agentsRaw?: string;
  humanRaw?: string;
};

// Raw-JSON fallback for a slice with no prior last-known-good value (cold
// start): every slice is a JSON array, so "[]" parses cleanly into an empty
// list rather than throwing and discarding the rest of the snapshot.
const EMPTY_JSON_ARRAY = "[]";

// Resolve one Promise.allSettled result to a raw JSON string: the fresh value
// on success (also recorded into lastGood for future ticks), or the previous
// last-known-good value (falling back to an empty array if none exists yet)
// on failure. A failed call must not blank an otherwise-healthy snapshot.
function resolveSlice(
  result: PromiseSettledResult<string>,
  cliName: string,
  lastGood: LastGoodSlices | undefined,
  key: keyof LastGoodSlices,
): string {
  if (result.status === "fulfilled") {
    if (lastGood) lastGood[key] = result.value;
    return result.value;
  }
  const reason = result.reason;
  if (reason instanceof CliError) {
    console.error(`[snapshot] ${cliName} failed, keeping last-known-good: ${reason.message}`);
  } else {
    console.error(`[snapshot] ${cliName} failed, keeping last-known-good:`, reason);
  }
  return lastGood?.[key] ?? EMPTY_JSON_ARRAY;
}

// Build one SnapshotEvent by calling all CLIs in parallel where possible.
// transitions: the SnapshotManager's cross-poll map (agent-teams-ni2y.8). Omitted by
// ad-hoc/endpoint-fallback callers (index.ts, before the first poll) -> buildInbox
// degrades gracefully to lastActivityAt = updated_at.
// lastGood: the SnapshotManager's cross-poll last-known-good raw slices
// (agent-teams-assa.1). Omitted by ad-hoc/endpoint-fallback callers -> a failed
// call in that one-shot degrades to an empty slice instead of failing outright.
export async function buildSnapshot(
  transitions?: TransitionMap,
  now = Date.now(),
  lastGood?: LastGoodSlices,
): Promise<SnapshotEvent> {
  // ateam ws is a prerequisite for bdHumanList, so fetch it first.
  const ws = await ateamWs();

  // Fetch open initiatives, closed initiatives, agents, and human-gated list in
  // parallel. allSettled (not all): one killed/failed subprocess call must not
  // discard the snapshot slices that did succeed (agent-teams-assa.1).
  const [listResult, closedResult, agentsResult, humanResult] = await Promise.allSettled([
    ateamListJson(),
    bdClosedInitiatives(ws),
    claudeAgentsJson(),
    bdHumanList(ws),
  ]);

  const listJsonRaw = resolveSlice(listResult, "ateam list-json", lastGood, "listJsonRaw");
  const closedJsonRaw = resolveSlice(closedResult, "bd closed initiatives", lastGood, "closedJsonRaw");
  const agentsRaw = resolveSlice(agentsResult, "claude agents", lastGood, "agentsRaw");
  const humanRaw = resolveSlice(humanResult, "bd human list", lastGood, "humanRaw");

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
