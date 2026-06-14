import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { InitiativeNode } from "@agent-teams/shared";

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
  connectionState: string;
  error: string | null;
  ts: number | null;
} = {
  initiatives: [],
  connectionState: "connected",
  error: null,
  ts: null,
};

vi.mock("../../SnapshotContext.js", () => ({
  useSnapshotContext: () => mockSnapshotState,
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
      connectionState: "connected",
      error: null,
      ts: null,
    };
  });

  it("renders empty state when there are no initiatives", () => {
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

  it("renders one SVG node per initiative", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [
        makeNode({ id: "a", title: "Alpha" }),
        makeNode({ id: "b", title: "Beta", activity: "busy" }),
        makeNode({ id: "c", title: "Gamma", activity: "needs-human" }),
      ],
    };
    const { container } = render(<ConstellationView />);
    // Each node has an aria-label
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

  it("node aria-label includes the activity status", () => {
    mockSnapshotState = {
      ...mockSnapshotState,
      initiatives: [makeNode({ id: "nh-1", title: "Blocked Thing", activity: "needs-human" })],
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
});
