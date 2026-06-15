import { describe, it, expect } from "vitest";
import { snapshotReducer, parseSSEPayload, type SnapshotState } from "./useSnapshot.js";
import type { SnapshotEvent } from "@agent-teams/shared";

const baseState: SnapshotState = {
  initiatives: [],
  unmatchedSessions: [],
  inbox: [],
  ts: null,
  connectionState: "connecting",
  error: null,
};

const sampleSnapshot: SnapshotEvent = {
  initiatives: [],
  unmatchedSessions: [],
  inbox: [],
  ts: 1_700_000_000_000,
};

describe("snapshotReducer", () => {
  it("connecting action resets error and sets connectionState", () => {
    const state = snapshotReducer(
      { ...baseState, connectionState: "error", error: "boom" },
      { type: "connecting" }
    );
    expect(state.connectionState).toBe("connecting");
    expect(state.error).toBeNull();
  });

  it("snapshot action updates data and sets connected", () => {
    const state = snapshotReducer(baseState, {
      type: "snapshot",
      payload: sampleSnapshot,
    });
    expect(state.connectionState).toBe("connected");
    expect(state.ts).toBe(1_700_000_000_000);
    expect(state.error).toBeNull();
  });

  it("snapshot action threads unmatchedSessions into state", () => {
    const withOrphans: SnapshotEvent = {
      ...sampleSnapshot,
      unmatchedSessions: [
        {
          pid: 12345,
          cwd: "/some/orphan/path",
          kind: "background",
          startedAt: 0,
          sessionId: "orphan-uuid",
          status: "idle",
          id: "orphan1",
          name: "dispatch-orchestrator",
          state: "working",
        },
      ],
    };
    const state = snapshotReducer(baseState, { type: "snapshot", payload: withOrphans });
    expect(state.unmatchedSessions).toHaveLength(1);
    expect(state.unmatchedSessions[0]?.sessionId).toBe("orphan-uuid");
  });

  it("snapshot action defaults unmatchedSessions to [] when field absent", () => {
    // Simulates a server that hasn't been updated yet — field missing from payload.
    const legacySnapshot = { initiatives: [], inbox: [], ts: 0 } as unknown as SnapshotEvent;
    const state = snapshotReducer(baseState, { type: "snapshot", payload: legacySnapshot });
    expect(state.unmatchedSessions).toEqual([]);
  });

  it("snapshot action preserves previous data structure", () => {
    const withInbox: SnapshotEvent = {
      ...sampleSnapshot,
      inbox: [
        {
          initiativeId: "abc",
          title: "Do a thing",
          kind: "answer",
          question: "Which approach?",
          worktree: "/tmp/wt",
          prUrl: null,
        },
      ],
    };
    const state = snapshotReducer(baseState, { type: "snapshot", payload: withInbox });
    expect(state.inbox).toHaveLength(1);
    expect(state.inbox[0]?.initiativeId).toBe("abc");
  });

  it("reconnecting action sets connectionState without clearing data", () => {
    const prev = snapshotReducer(baseState, { type: "snapshot", payload: sampleSnapshot });
    const state = snapshotReducer(prev, { type: "reconnecting" });
    expect(state.connectionState).toBe("reconnecting");
    expect(state.ts).toBe(sampleSnapshot.ts); // data preserved
  });

  it("error action sets connectionState and message", () => {
    const state = snapshotReducer(baseState, { type: "error", message: "stream closed" });
    expect(state.connectionState).toBe("error");
    expect(state.error).toBe("stream closed");
  });
});

describe("parseSSEPayload", () => {
  it("parses a valid SnapshotEvent JSON string", () => {
    const raw = JSON.stringify(sampleSnapshot);
    const result = parseSSEPayload(raw);
    expect(result).not.toBeNull();
    expect(result?.ts).toBe(sampleSnapshot.ts);
    expect(Array.isArray(result?.initiatives)).toBe(true);
    expect(Array.isArray(result?.inbox)).toBe(true);
  });

  it("returns null for invalid JSON", () => {
    expect(parseSSEPayload("not json")).toBeNull();
  });

  it("returns null for JSON missing required fields", () => {
    expect(parseSSEPayload(JSON.stringify({ ts: 123 }))).toBeNull();
  });

  it("returns null for an empty object", () => {
    expect(parseSSEPayload("{}")).toBeNull();
  });

  it("returns null for a JSON array", () => {
    expect(parseSSEPayload("[]")).toBeNull();
  });

  it("returns null when initiatives is not an array", () => {
    expect(
      parseSSEPayload(JSON.stringify({ initiatives: "bad", inbox: [], ts: 0 }))
    ).toBeNull();
  });
});
