import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";

// ---------------------------------------------------------------------------
// Mocks — must be hoisted before the component import.
// ---------------------------------------------------------------------------

const mockNavigate = vi.fn();

vi.mock("react-router-dom", () => ({
  useNavigate: () => mockNavigate,
}));

// Capture the context value injected by tests.
let mockSnapshotState: {
  initiatives: InitiativeNode[];
  unmatchedSessions: SessionState[];
  connectionState: string;
  error: string | null;
  ts: number | null;
} = {
  initiatives: [],
  unmatchedSessions: [],
  connectionState: "connected",
  error: null,
  ts: null,
};

vi.mock("../../SnapshotContext.js", () => ({
  useSnapshotContext: () => mockSnapshotState,
}));

// ResizeObserver not available in jsdom — provide a no-op stub.
global.ResizeObserver = vi.fn().mockImplementation(() => ({
  observe: vi.fn(),
  unobserve: vi.fn(),
  disconnect: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Import component AFTER mocks are registered.
// ---------------------------------------------------------------------------
import ConstellationView from "./index.js";
import { globalEvenAngles } from "./index.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeNode(
  overrides: Partial<InitiativeNode> & { id?: string; title?: string } = {},
): InitiativeNode {
  const id = overrides.id ?? "test-id-1";
  const title = overrides.title ?? "My Initiative";
  return {
    initiative: {
      id,
      title,
      description: "",
      notes: "",
      status: "open",
      priority: "P1",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-01-01",
      updated_at: "2026-01-01",
      problem: "",
      repo: "",
      worktree: "/wt",
      branch: "main",
      team: "",
      mode: "",
      goal: "",
      prUrl: null,
    },
    session: null,
    activity: overrides.activity ?? "idle",
    phase: overrides.phase ?? "parked",
    delivery: overrides.delivery ?? "none",
    needsHuman: overrides.needsHuman ?? false,
    ...overrides,
  };
}

function makeOrphan(overrides: Partial<SessionState> & { sessionId?: string } = {}): SessionState {
  return {
    pid: 99999,
    cwd: "/some/unregistered/path",
    kind: "background",
    startedAt: Date.now(),
    sessionId: overrides.sessionId ?? "orphan-session-id-1",
    status: "idle",
    id: overrides.id ?? "orphan1",
    name: overrides.name ?? "orphan-session-name",
    state: "working",
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ConstellationView", () => {
  beforeEach(() => {
    mockNavigate.mockClear();
    mockSnapshotState = {
      initiatives: [],
      unmatchedSessions: [],
      connectionState: "connected",
      error: null,
      ts: null,
    };
  });

  it("renders empty state when there are no initiatives or orphans", () => {
    const { container } = render(<ConstellationView />);
    expect(container.textContent).toContain("No initiatives active");
  });

  it("renders connecting state while connecting with no data", () => {
    mockSnapshotState = { ...mockSnapshotState, connectionState: "connecting" };
    const { container } = render(<ConstellationView />);
    expect(container.textContent).toContain("Connecting");
  });

  it("renders error state on connection error", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      connectionState: "error",
      error: "stream closed",
    };
    const { container } = render(<ConstellationView />);
    expect(container.textContent).toContain("stream closed");
  });

  it("renders one clickable SVG node per initiative", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({ id: "a", title: "Alpha" }),
        makeNode({ id: "b", title: "Beta", activity: "busy" }),
        makeNode({ id: "c", title: "Gamma", activity: "needs-human" }),
      ],
    };
    const { container } = render(<ConstellationView />);
    const nodes = container.querySelectorAll("[role='button']");
    expect(nodes).toHaveLength(3);
  });

  it("navigates to /initiative/:id on node click", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "xyz-123", title: "Test Nav", activity: "busy" })],
    };
    const { container } = render(<ConstellationView />);
    const node = container.querySelector("[role='button']") as Element;
    expect(node).not.toBeNull();
    fireEvent.click(node);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/xyz-123");
  });

  it("navigates on Enter key press", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "kbd-id", title: "Keyboard Nav", activity: "idle" })],
    };
    render(<ConstellationView />);
    const node = screen.getByRole("button", { name: /Keyboard Nav/ });
    node.focus();
    fireEvent.keyDown(node, { key: "Enter" });
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/kbd-id");
  });

  it("node aria-label includes the activity status (needs you)", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "nh-1", title: "Blocked Thing", needsHuman: "waiting" })],
    };
    render(<ConstellationView />);
    const node = screen.getByRole("button", { name: /Blocked Thing.*needs you/i });
    expect(node).not.toBeNull();
  });

  it("renders with various activity states without throwing", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({ id: "1", activity: "busy" }),
        makeNode({ id: "2", activity: "idle" }),
        makeNode({ id: "3", activity: "needs-human" }),
        makeNode({ id: "4", activity: "delivered" }),
        makeNode({ id: "5", activity: "done" }),
      ],
    };
    expect(() => render(<ConstellationView />)).not.toThrow();
  });

  it("shows reconnecting banner but still renders nodes", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      connectionState: "reconnecting",
      initiatives: [makeNode({ id: "reco", title: "Reco Node" })],
    };
    const { container } = render(<ConstellationView />);
    expect(container.textContent).toContain("reconnecting");
    const nodes = container.querySelectorAll("[role='button']");
    expect(nodes).toHaveLength(1);
  });

  // ---------------------------------------------------------------------------
  // Orphan session nodes
  // ---------------------------------------------------------------------------

  it("renders orphan session nodes with data-orphan-session-id attribute", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "init-1", title: "Tracked" })],
      unmatchedSessions: [makeOrphan({ sessionId: "orphan-abc", name: "dispatch-orchestrator" })],
    };
    const { container } = render(<ConstellationView />);
    const orphanNode = container.querySelector("[data-orphan-session-id='orphan-abc']");
    expect(orphanNode).not.toBeNull();
  });

  it("orphan nodes are not clickable (no role=button)", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [],
      unmatchedSessions: [makeOrphan({ sessionId: "orphan-xyz" })],
    };
    const { container } = render(<ConstellationView />);
    const buttons = container.querySelectorAll("[role='button']");
    expect(buttons).toHaveLength(0);
    const orphan = container.querySelector("[data-orphan-session-id='orphan-xyz']");
    expect(orphan).not.toBeNull();
  });

  it("orphan node is labeled with session name", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [],
      unmatchedSessions: [makeOrphan({ sessionId: "orphan-named", name: "my-orphan-bg" })],
    };
    const { container } = render(<ConstellationView />);
    const orphan = container.querySelector("[data-orphan-session-id='orphan-named']");
    expect(orphan?.getAttribute("aria-label")).toContain("my-orphan-bg");
  });

  it("empty state NOT shown when only orphans exist (no initiatives)", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [],
      unmatchedSessions: [makeOrphan()],
    };
    const { container } = render(<ConstellationView />);
    expect(container.textContent).not.toContain("No initiatives active");
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
  });

  it("data-node-count and data-orphan-count are set on the SVG", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "i1" }), makeNode({ id: "i2" })],
      unmatchedSessions: [makeOrphan({ sessionId: "o1" }), makeOrphan({ sessionId: "o2" })],
    };
    const { container } = render(<ConstellationView />);
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("data-node-count")).toBe("2");
    expect(svg?.getAttribute("data-orphan-count")).toBe("2");
  });

  // ---------------------------------------------------------------------------
  // Badges
  // ---------------------------------------------------------------------------

  it("needs-human node has the needs-human badge", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      // needsHuman drives the badge now; activity field kept for compat.
      initiatives: [makeNode({ id: "nh", needsHuman: "waiting" })],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
  });

  it("node with delivery=pr-open and needsHuman=false has the PR badge", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({
          id: "pr-node",
          delivery: "pr-open",
          needsHuman: false,
          initiative: {
            id: "pr-node",
            title: "PR Node",
            description: "",
            notes: "",
            status: "open",
            priority: "P1",
            issue_type: "task",
            owner: "eric",
            created_at: "2026-01-01",
            updated_at: "2026-01-01",
            problem: "",
            repo: "",
            worktree: "/wt",
            branch: "main",
            team: "",
            mode: "",
            goal: "",
            prUrl: "https://github.com/org/repo/pull/42",
          },
        }),
      ],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='pr']");
    expect(badge).not.toBeNull();
  });

  it("idle node with no prUrl has no PR badge", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "idle-no-pr", activity: "idle" })],
    };
    const { container } = render(<ConstellationView />);
    expect(container.querySelector("[data-badge='pr']")).toBeNull();
  });

  it("needs-human node does NOT show PR badge even with delivery=pr-open", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({
          id: "nh-pr",
          // review flavor: pr-open + idle — needsHuman badge takes priority
          delivery: "pr-open",
          needsHuman: "review",
          initiative: {
            id: "nh-pr",
            title: "NH with PR",
            description: "",
            notes: "",
            status: "open",
            priority: "P1",
            issue_type: "task",
            owner: "eric",
            created_at: "2026-01-01",
            updated_at: "2026-01-01",
            problem: "",
            repo: "",
            worktree: "/wt",
            branch: "main",
            team: "",
            mode: "",
            goal: "",
            prUrl: "https://github.com/org/repo/pull/99",
          },
        }),
      ],
    };
    const { container } = render(<ConstellationView />);
    // needs-human takes priority: needs-human badge shown, PR badge not shown
    expect(container.querySelector("[data-badge='needs-human']")).not.toBeNull();
    expect(container.querySelector("[data-badge='pr']")).toBeNull();
  });

  // ---------------------------------------------------------------------------
  // Legend
  // ---------------------------------------------------------------------------

  it("renders the legend element", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "leg-1" })],
    };
    const { container } = render(<ConstellationView />);
    const legend = container.querySelector("[data-testid='constellation-legend']");
    expect(legend).not.toBeNull();
  });

  it("legend has aria-label for accessibility", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "leg-2" })],
    };
    const { container } = render(<ConstellationView />);
    const legend = container.querySelector("[data-testid='constellation-legend']");
    expect(legend?.getAttribute("aria-label")).toBeTruthy();
  });

  // ---------------------------------------------------------------------------
  // Attention state model (agent-teams-blo)
  // ---------------------------------------------------------------------------

  it("needsHuman='waiting' node has the needs-human badge (most urgent)", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "wt-1", needsHuman: "waiting" })],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
  });

  it("needsHuman='generic' node has the needs-human badge", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "gen-1", needsHuman: "generic" })],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
  });

  it("needsHuman='review' node has the needs-human badge", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "rv-1", needsHuman: "review" })],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
  });

  it("needsHuman='waiting' maps to data-activity='needs-human'", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "wt-2", needsHuman: "waiting" })],
    };
    const { container } = render(<ConstellationView />);
    const node = container.querySelector("[data-initiative-id='wt-2']");
    expect(node?.getAttribute("data-activity")).toBe("needs-human");
  });

  it("needsHuman=false working node maps to data-activity='busy'", () => {
    const workingSession: import("@agent-teams/shared").SessionState = {
      sessionId: "sess-wk",
      kind: "background",
      cwd: "/wt",
      startedAt: 0,
      status: "busy",
      state: "working",
    };
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "wk-1", needsHuman: false, session: workingSession })],
    };
    const { container } = render(<ConstellationView />);
    const node = container.querySelector("[data-initiative-id='wk-1']");
    expect(node?.getAttribute("data-activity")).toBe("busy");
  });

  // ---------------------------------------------------------------------------
  // Explicit gate:review (agent-teams-0rl)
  // ---------------------------------------------------------------------------

  it("needsHuman='review' (gate:review) node has needs-human badge + inner orbit styling", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({
          id: "gate-review-1",
          needsHuman: "review",
          delivery: "pr-open",
          initiative: {
            id: "gate-review-1",
            title: "Gate Review Initiative",
            description: "",
            notes: "",
            status: "open",
            priority: "P1",
            issue_type: "task",
            owner: "eric",
            created_at: "2026-01-01",
            updated_at: "2026-01-01",
            problem: "",
            repo: "",
            worktree: "/wt",
            branch: "main",
            team: "",
            mode: "",
            goal: "",
            prUrl: "https://github.com/org/repo/pull/10",
          },
        }),
      ],
    };
    const { container } = render(<ConstellationView />);
    // needs-human badge (orange "!")
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
    // data-activity='needs-human'
    const node = container.querySelector("[data-initiative-id='gate-review-1']");
    expect(node?.getAttribute("data-activity")).toBe("needs-human");
    // delivery ring (green ring around node) — delivery=pr-open -> hasPr=true
    expect(node?.querySelector(".node-delivery-ring")).not.toBeNull();
    // NOT PR badge (needs-human takes priority)
    expect(container.querySelector("[data-badge='pr']")).toBeNull();
  });

  it("gate:review node wins over working session (still shows needs-human)", () => {
    const workingSession: import("@agent-teams/shared").SessionState = {
      sessionId: "gate-sess",
      kind: "background",
      cwd: "/wt",
      startedAt: 0,
      status: "busy",
      state: "working",
    };
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({
          id: "gate-wins",
          needsHuman: "review",
          delivery: "pr-open",
          session: workingSession,
        }),
      ],
    };
    const { container } = render(<ConstellationView />);
    const badge = container.querySelector("[data-badge='needs-human']");
    expect(badge).not.toBeNull();
    const node = container.querySelector("[data-initiative-id='gate-wins']");
    expect(node?.getAttribute("data-activity")).toBe("needs-human");
  });
});

// ---------------------------------------------------------------------------
// globalEvenAngles — unit tests for the globally-even angular distribution
// (agent-teams-55v: collision-free layout)
// ---------------------------------------------------------------------------

describe("globalEvenAngles", () => {
  it("returns empty map for zero nodes", () => {
    const result = globalEvenAngles([]);
    expect(result.size).toBe(0);
  });

  it("assigns one angle for a single node", () => {
    const result = globalEvenAngles([{ id: "a", urgencyOrder: 0 }]);
    expect(result.size).toBe(1);
    expect(result.has("a")).toBe(true);
  });

  it("all angles are distinct — no two nodes share an angle", () => {
    const ids = ["alpha", "beta", "gamma", "delta", "epsilon"].map((id, i) => ({
      id,
      urgencyOrder: i % 3,
    }));
    const result = globalEvenAngles(ids);
    const angles = Array.from(result.values());
    const uniqueAngles = new Set(angles.map((a) => a.toFixed(10)));
    expect(uniqueAngles.size).toBe(ids.length);
  });

  it("minimum angular separation equals 2pi/N for N nodes", () => {
    const n = 6;
    const ids = Array.from({ length: n }, (_, i) => ({ id: `node-${i}`, urgencyOrder: 0 }));
    const result = globalEvenAngles(ids);
    const angles = Array.from(result.values()).sort((a, b) => a - b);
    const expectedSlice = (2 * Math.PI) / n;
    for (let i = 0; i < angles.length - 1; i++) {
      const sep = (angles[i + 1] ?? 0) - (angles[i] ?? 0);
      expect(sep).toBeCloseTo(expectedSlice, 5);
    }
  });

  it("layout is stable — same inputs produce same angles on repeated calls", () => {
    const ids = [
      { id: "waiting-node", urgencyOrder: 0 },
      { id: "needs-human-node", urgencyOrder: 1 },
      { id: "working-node", urgencyOrder: 2 },
      { id: "idle-node", urgencyOrder: 3 },
    ];
    const result1 = globalEvenAngles(ids);
    const result2 = globalEvenAngles(ids);
    for (const { id } of ids) {
      expect(result1.get(id)).toBe(result2.get(id));
    }
  });

  it("urgency order controls sort: most urgent node gets first angle slot", () => {
    const ids = [
      { id: "idle", urgencyOrder: 3 },
      { id: "urgent", urgencyOrder: 0 },
      { id: "working", urgencyOrder: 2 },
    ];
    const result = globalEvenAngles(ids);
    const slice = (2 * Math.PI) / 3;
    const phase = -Math.PI / 2;
    expect(result.get("urgent")).toBeCloseTo(phase, 5);
    expect(result.get("working")).toBeCloseTo(phase + slice, 5);
    expect(result.get("idle")).toBeCloseTo(phase + 2 * slice, 5);
  });

  it("two nodes at same urgencyOrder are separated by pi (half circle)", () => {
    const ids = [
      { id: "node-a", urgencyOrder: 0 },
      { id: "node-b", urgencyOrder: 0 },
    ];
    const result = globalEvenAngles(ids);
    const a = result.get("node-a") ?? 0;
    const b = result.get("node-b") ?? 0;
    const sep = Math.abs(b - a);
    expect(sep).toBeCloseTo(Math.PI, 5);
  });

  it("handles 4 mixed-tier nodes with distinct evenly-spaced angles", () => {
    const ids = [
      { id: "w1", urgencyOrder: 0 },
      { id: "n1", urgencyOrder: 1 },
      { id: "i1", urgencyOrder: 3 },
      { id: "d1", urgencyOrder: 4 },
    ];
    const result = globalEvenAngles(ids);
    expect(result.size).toBe(4);
    const angles = Array.from(result.values());
    const uniqueAngles = new Set(angles.map((a) => a.toFixed(10)));
    expect(uniqueAngles.size).toBe(4);
    const sorted = angles.slice().sort((a, b) => a - b);
    for (let i = 0; i < sorted.length - 1; i++) {
      const sep = (sorted[i + 1] ?? 0) - (sorted[i] ?? 0);
      expect(sep).toBeCloseTo(Math.PI / 2, 5);
    }
  });
});
