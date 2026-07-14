// Unit tests for stampTransitions — the session-transition bookkeeping helper
// behind session-transition-aware inbox recency (agent-teams-ni2y.8) — plus
// core-path coverage for the poll overlap-guard and partial-snapshot
// tolerance added in agent-teams-assa.1.

import { describe, it, expect, vi } from "vitest";
import { stampTransitions, buildSnapshot, SnapshotManager, type TransitionMap } from "./snapshot.js";
import type { SessionState } from "@agent-teams/shared";
import * as cli from "./cli.js";

vi.mock("./cli.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./cli.js")>();
  return {
    ...actual,
    ateamWs: vi.fn(),
    ateamListJson: vi.fn(),
    bdClosedInitiatives: vi.fn(),
    claudeAgentsJson: vi.fn(),
    bdHumanList: vi.fn(),
  };
});

// Minimal SessionState fixture — only the fields stampTransitions reads.
function makeSession(overrides: Partial<SessionState> & { sessionId: string }): SessionState {
  return {
    cwd: "/wt/foo",
    kind: "background",
    startedAt: 1_000,
    ...overrides,
  };
}

describe("stampTransitions", () => {
  it("first sighting uses session.startedAt", () => {
    const prev: TransitionMap = new Map();
    const session = makeSession({ sessionId: "s1", startedAt: 5_000, status: "busy" });
    const lookup = stampTransitions([session], prev, 9_999);
    expect(lookup.get("s1")).toBe(5_000);
    expect(prev.get("s1")?.lastTransitionAt).toBe(5_000);
  });

  it("first sighting falls back to now when startedAt is absent", () => {
    const prev: TransitionMap = new Map();
    const session = makeSession({ sessionId: "s1", startedAt: undefined, status: "busy" });
    const lookup = stampTransitions([session], prev, 9_999);
    expect(lookup.get("s1")).toBe(9_999);
  });

  it("(status, state) change from the prior tick stamps now", () => {
    const prev: TransitionMap = new Map([
      ["s1", { status: "busy", state: "working", lastTransitionAt: 1_000 }],
    ]);
    const session = makeSession({ sessionId: "s1", status: "waiting", state: "blocked" });
    const lookup = stampTransitions([session], prev, 5_000);
    expect(lookup.get("s1")).toBe(5_000);
    expect(prev.get("s1")).toEqual({ status: "waiting", state: "blocked", lastTransitionAt: 5_000 });
  });

  it("unchanged (status, state) keeps the prior stamp — no spurious rise", () => {
    const prev: TransitionMap = new Map([
      ["s1", { status: "busy", state: "working", lastTransitionAt: 1_000 }],
    ]);
    const session = makeSession({ sessionId: "s1", status: "busy", state: "working" });
    const lookup = stampTransitions([session], prev, 5_000);
    expect(lookup.get("s1")).toBe(1_000);
  });

  it("prunes sessionIds that vanished from this tick's snapshot", () => {
    const prev: TransitionMap = new Map([
      ["s1", { status: "busy", state: "working", lastTransitionAt: 1_000 }],
      ["gone", { status: "busy", state: "working", lastTransitionAt: 1_000 }],
    ]);
    const session = makeSession({ sessionId: "s1", status: "busy", state: "working" });
    stampTransitions([session], prev, 5_000);
    expect(prev.has("gone")).toBe(false);
    expect(prev.has("s1")).toBe(true);
  });
});

describe("SnapshotManager overlap guard (agent-teams-assa.1)", () => {
  it("a poll() still in flight causes the overlapping tick to skip, not stack", async () => {
    // Block the first poll() at its very first await (ateamWs), before it ever
    // reaches the CLI calls — this pins down the exact window an overlapping
    // tick would otherwise race into.
    let resolveWs!: (value: string) => void;
    const wsPending = new Promise<string>((resolve) => {
      resolveWs = resolve;
    });

    vi.mocked(cli.ateamWs).mockReturnValue(wsPending);
    vi.mocked(cli.ateamListJson).mockResolvedValue("[]");
    vi.mocked(cli.bdClosedInitiatives).mockResolvedValue("[]");
    vi.mocked(cli.claudeAgentsJson).mockResolvedValue("[]");
    vi.mocked(cli.bdHumanList).mockResolvedValue("[]");

    const manager = new SnapshotManager();
    const pollable = manager as unknown as { poll(): Promise<void> };

    const first = pollable.poll(); // in flight — blocked on the pending ateamWs call
    const second = pollable.poll(); // fires while first is still in flight — must skip

    await second;
    // The overlapping tick must have returned without ever calling through to
    // buildSnapshot's CLI calls (first hasn't even resolved ateamWs yet).
    expect(cli.ateamListJson).not.toHaveBeenCalled();

    resolveWs("/ws");
    await first;
    // Only the first tick's single pass ever called through — no stacked/duplicate call.
    expect(cli.ateamListJson).toHaveBeenCalledTimes(1);
  });
});

describe("buildSnapshot partial-snapshot tolerance (agent-teams-assa.1)", () => {
  it("a rejected slice falls back to last-known-good instead of blanking that slice", async () => {
    vi.mocked(cli.ateamWs).mockResolvedValue("/ws");
    vi.mocked(cli.ateamListJson).mockResolvedValue("[]");
    vi.mocked(cli.bdClosedInitiatives).mockResolvedValue("[]");
    vi.mocked(cli.claudeAgentsJson).mockRejectedValue(new Error("killed"));
    vi.mocked(cli.bdHumanList).mockResolvedValue("[]");

    const goodAgentsRaw = JSON.stringify([
      { sessionId: "s1", cwd: "/wt/foo", kind: "background", startedAt: 1_000 },
    ]);
    const lastGood = { agentsRaw: goodAgentsRaw };

    const event = await buildSnapshot(undefined, 9_999, lastGood);

    // The killed claudeAgentsJson call must not blank the sessions slice — the
    // previous last-known-good session should still surface as unmatched.
    expect(event.unmatchedSessions.map((s) => s.sessionId)).toContain("s1");
    // And the successful fallback is recorded back into lastGood for reuse.
    expect(lastGood.agentsRaw).toBe(goodAgentsRaw);
  });
});
