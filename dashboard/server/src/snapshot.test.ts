// Unit tests for stampTransitions — the session-transition bookkeeping helper
// behind session-transition-aware inbox recency (agent-teams-ni2y.8) — plus
// core-path coverage for the poll overlap-guard and partial-snapshot
// tolerance added in agent-teams-assa.1.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  stampTransitions,
  buildSnapshot,
  SnapshotManager,
  PROJECT_READ_CONCURRENCY,
  type TransitionMap,
} from "./snapshot.js";
import type { SessionState } from "@agent-teams/shared";
import * as cli from "./cli.js";

vi.mock("./cli.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./cli.js")>();
  return {
    ...actual,
    ateamWs: vi.fn(),
    ateamListJson: vi.fn(),
    ateamClosedInitiatives: vi.fn(),
    claudeAgentsJson: vi.fn(),
    bdHumanList: vi.fn(),
    bdProjectBeads: vi.fn(),
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
    vi.mocked(cli.ateamClosedInitiatives).mockResolvedValue("[]");
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
    vi.mocked(cli.ateamClosedInitiatives).mockResolvedValue("[]");
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

function rawInitiative(
  id: string,
  repo: string,
  epic: string,
  associations: Array<{ pr: string; workstream: string }> = [],
) {
  const prs = associations.map((association) => association.pr);
  return {
    id,
    title: `Initiative ${id}`,
    description: "",
    notes: "",
    status: "open",
    priority: "2",
    issue_type: "epic",
    owner: "owner",
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-20T00:00:00Z",
    fields: { repo, epic },
    prs,
    pr_reviews: prs.map((pr) => ({ pr, gate: "" })),
    pr_workstreams: associations,
  };
}

function rawBead(
  id: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    id,
    title: id,
    status: "open",
    priority: "2",
    issue_type: "task",
    ...overrides,
  };
}

function mockSnapshotSlices(initiatives: unknown[]): void {
  vi.mocked(cli.ateamWs).mockResolvedValue("/ws");
  vi.mocked(cli.ateamListJson).mockResolvedValue(JSON.stringify(initiatives));
  vi.mocked(cli.ateamClosedInitiatives).mockResolvedValue("[]");
  vi.mocked(cli.claudeAgentsJson).mockResolvedValue("[]");
  vi.mocked(cli.bdHumanList).mockResolvedValue("[]");
}

describe("buildSnapshot repo-batched workstream projection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("issues one project read per distinct repo and isolates two initiatives sharing a repo", async () => {
    const pr = "https://github.com/org/repo/pull/1";
    mockSnapshotSlices([
      rawInitiative("at-a", "/repo/shared", "epic-a", [{ pr, workstream: "a-nested" }]),
      rawInitiative("at-b", "/repo/shared", "epic-b"),
      rawInitiative("at-c", "/repo/other", "epic-c"),
      rawInitiative("at-empty", "   ", "epic-empty"),
    ]);
    vi.mocked(cli.bdProjectBeads).mockImplementation(async (repo) => {
      if (repo === "/repo/shared") {
        return JSON.stringify([
          rawBead("epic-a", { issue_type: "epic" }),
          rawBead("a-direct", { parent: "epic-a", status: "in_progress" }),
          rawBead("a-nested", { parent: "a-direct", status: "closed" }),
          rawBead("epic-b", { issue_type: "epic" }),
          rawBead("b-direct", { parent: "epic-b" }),
        ]);
      }
      return JSON.stringify([
        rawBead("epic-c", { issue_type: "epic" }),
        rawBead("c-direct", { parent: "epic-c" }),
      ]);
    });

    const event = await buildSnapshot();

    expect(cli.bdProjectBeads).toHaveBeenCalledTimes(2);
    expect(vi.mocked(cli.bdProjectBeads).mock.calls.map(([repo]) => repo)).toEqual([
      "/repo/shared",
      "/repo/other",
    ]);
    const a = event.initiatives.find((node) => node.initiative.id === "at-a")!;
    const b = event.initiatives.find((node) => node.initiative.id === "at-b")!;
    const c = event.initiatives.find((node) => node.initiative.id === "at-c")!;
    const empty = event.initiatives.find((node) => node.initiative.id === "at-empty")!;
    expect(a.workstreams).toEqual([
      expect.objectContaining({
        id: "a-direct",
        memberIds: ["a-direct", "a-nested"],
        progress: { total: 1, closed: 1 },
      }),
    ]);
    expect(b.workstreams?.map((workstream) => workstream.id)).toEqual(["b-direct"]);
    expect(c.workstreams?.map((workstream) => workstream.id)).toEqual(["c-direct"]);
    expect(b.workstreams?.flatMap((workstream) => workstream.memberIds ?? [])).not.toContain(
      "a-nested",
    );
    expect(empty.workstreams).toEqual([
      expect.objectContaining({ id: "initiative:at-empty", kind: "fallback" }),
    ]);
    expect(empty.workstreamDiagnostics).toContainEqual(expect.objectContaining({ kind: "no-repo" }));
  });

  it("retains per-repo last-known-good workstreams while another repo refreshes", async () => {
    mockSnapshotSlices([
      rawInitiative("at-a", "/repo/a", "epic-a"),
      rawInitiative("at-b", "/repo/b", "epic-b"),
    ]);
    const lastGood = {};
    vi.mocked(cli.bdProjectBeads).mockImplementation(async (repo) =>
      JSON.stringify([
        rawBead(repo === "/repo/a" ? "epic-a" : "epic-b", { issue_type: "epic" }),
        rawBead(repo === "/repo/a" ? "a-old" : "b-old", {
          parent: repo === "/repo/a" ? "epic-a" : "epic-b",
        }),
      ]),
    );
    await buildSnapshot(undefined, 1, lastGood);

    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    vi.mocked(cli.bdProjectBeads).mockImplementation(async (repo) => {
      if (repo === "/repo/a") throw new Error("repo a unavailable");
      return JSON.stringify([
        rawBead("epic-b", { issue_type: "epic" }),
        rawBead("b-new", { parent: "epic-b" }),
      ]);
    });
    const event = await buildSnapshot(undefined, 2, lastGood);
    errorSpy.mockRestore();

    const a = event.initiatives.find((node) => node.initiative.id === "at-a")!;
    const b = event.initiatives.find((node) => node.initiative.id === "at-b")!;
    expect(a.workstreams?.map((workstream) => workstream.id)).toEqual(["a-old"]);
    expect(a.workstreamDiagnostics).toContainEqual(expect.objectContaining({ kind: "load-stale" }));
    expect(b.workstreams?.map((workstream) => workstream.id)).toEqual(["b-new"]);
    expect(b.workstreamDiagnostics).toEqual([]);
  });

  it("runs project reads concurrently but never exceeds the fixed pool size", async () => {
    const initiatives = Array.from({ length: PROJECT_READ_CONCURRENCY + 3 }, (_, index) =>
      rawInitiative(`at-${index}`, `/repo/${index}`, `epic-${index}`),
    );
    mockSnapshotSlices(initiatives);
    let active = 0;
    let maxActive = 0;
    vi.mocked(cli.bdProjectBeads).mockImplementation(async (_repo) => {
      active++;
      maxActive = Math.max(maxActive, active);
      await new Promise((resolve) => setTimeout(resolve, 5));
      active--;
      return "[]";
    });

    await buildSnapshot();

    expect(cli.bdProjectBeads).toHaveBeenCalledTimes(initiatives.length);
    expect(maxActive).toBe(PROJECT_READ_CONCURRENCY);
    expect(maxActive).toBeLessThanOrEqual(PROJECT_READ_CONCURRENCY);
  });
});
