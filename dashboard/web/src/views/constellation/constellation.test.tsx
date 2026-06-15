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
      initiatives: [makeNode({ id: "nh-1", title: "Blocked Thing", needsHuman: "answer" })],
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
      initiatives: [makeNode({ id: "nh", needsHuman: "answer" })],
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
});
