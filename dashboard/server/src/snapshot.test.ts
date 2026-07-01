// Unit tests for stampTransitions — the session-transition bookkeeping helper
// behind session-transition-aware inbox recency (agent-teams-ni2y.8).

import { describe, it, expect } from "vitest";
import { stampTransitions, type TransitionMap } from "./snapshot.js";
import type { SessionState } from "@agent-teams/shared";

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
