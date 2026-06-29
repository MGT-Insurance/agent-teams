import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import type { DrillInDetail, SessionState, WorkBead } from "@agent-teams/shared";

// --- Mocks ---

// xterm.js uses canvas APIs not present in jsdom; mock the Terminal class.
vi.mock("@xterm/xterm", () => {
  const Terminal = vi.fn(() => ({
    open: vi.fn(),
    write: vi.fn(),
    writeln: vi.fn(),
    dispose: vi.fn(),
  }));
  return { Terminal };
});

// Mock the CSS import — jsdom doesn't process it.
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("./drillin.css", () => ({}));

const mockNavigate = vi.fn();
const mockParams: { id: string } = { id: "init-abc" };

vi.mock("react-router-dom", () => ({
  useParams: () => mockParams,
  useNavigate: () => mockNavigate,
}));

const mockFetchInitiative = vi.fn();
const mockAttachToInitiative = vi.fn();
const mockLogsUrl = vi.fn((_id: string, _sessionId: string) => "/api/initiatives/init-abc/logs?session=sess-1");

vi.mock("../../lib/api.js", () => ({
  fetchInitiative: (id: string) => mockFetchInitiative(id),
  attachToInitiative: (id: string, sessionId: string) => mockAttachToInitiative(id, sessionId),
  logsUrl: (id: string, sessionId: string) => mockLogsUrl(id, sessionId),
}));

// Mock fetch for log streaming (LogPane calls fetch directly).
function makeMockFetch() {
  return Promise.resolve({
    ok: true,
    body: {
      getReader: () => ({
        read: vi
          .fn()
          .mockResolvedValueOnce({ done: false, value: new Uint8Array([27, 91, 72]) })
          .mockResolvedValueOnce({ done: true, value: undefined }),
      }),
    },
  } as unknown as Response);
}

vi.stubGlobal("fetch", vi.fn(() => makeMockFetch()));

// --- Fixtures ---

const bgSession: SessionState = {
  pid: 101,
  cwd: "/worktrees/init-abc",
  kind: "background",
  startedAt: 1_700_000_000_000,
  sessionId: "sess-1",
  status: "busy",
  id: "s1",
  name: "planner",
  state: "working",
};

const interactiveSession: SessionState = {
  pid: 202,
  cwd: "/worktrees/init-abc",
  kind: "interactive",
  startedAt: 1_700_000_001_000,
  sessionId: "sess-2",
  status: "idle",
};

const bead: WorkBead = {
  id: "bead-1",
  title: "Implement the thing",
  status: "open",
  priority: "P1",
  issue_type: "task",
};

const sampleDetail: DrillInDetail = {
  // RawInitiative fields
  id: "init-abc",
  title: "My Test Initiative",
  description: "repo: testrepo\nworktree: /wt/abc\nbranch: feat/thing\nteam: default\nmode: auto\ngoal: Do the thing",
  notes: "Latest note text",
  status: "open",
  priority: "P1",
  issue_type: "initiative",
  owner: "Eric",
  created_at: "2026-01-01",
  updated_at: "2026-01-02",
  // ParsedInitiative fields
  problem: "Problem statement",
  repo: "testrepo",
  worktree: "/wt/abc",
  branch: "feat/thing",
  team: "default",
  mode: "auto",
  goal: "Do the thing",
  prUrl: "https://github.com/org/repo/pull/42",
  epic: null,
  // DrillInDetail extras
  notesHistory: ["First note", "Second note", "Third note"],
  sessions: [bgSession, interactiveSession],
  workBeads: [bead],
};

// Import after mocks are registered.
const { default: DrillInView } = await import("./index.js");

// --- Tests ---

describe("DrillInView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("fetch", vi.fn(() => makeMockFetch()));
  });

  afterEach(() => {
    cleanup();
  });

  it("shows loading state while fetching", () => {
    mockFetchInitiative.mockReturnValue(new Promise(() => undefined));
    render(<DrillInView />);
    expect(screen.getByText(/loading initiative/i)).toBeTruthy();
  });

  it("renders initiative detail from sample DrillInDetail", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("My Test Initiative")).toBeTruthy();
    });

    // Header meta fields
    expect(screen.getByText("feat/thing")).toBeTruthy();
    expect(screen.getByText("testrepo")).toBeTruthy();
    expect(screen.getByText("Do the thing")).toBeTruthy();

    // PR link
    const prLink = screen.getByText("https://github.com/org/repo/pull/42");
    expect(prLink.tagName.toLowerCase()).toBe("a");
    expect((prLink as HTMLAnchorElement).href).toBe("https://github.com/org/repo/pull/42");

    // Notes history rendered
    expect(screen.getByText("Third note")).toBeTruthy();
    expect(screen.getByText("First note")).toBeTruthy();

    // Sessions table shows session name — "planner" appears in both table and toolbar attach
    const plannerEls = screen.getAllByText("planner");
    expect(plannerEls.length).toBeGreaterThanOrEqual(1);

    // Work beads table
    expect(screen.getByText("Implement the thing")).toBeTruthy();
    expect(screen.getByText("bead-1")).toBeTruthy();
  });

  it("shows fetch error when fetchInitiative rejects", async () => {
    mockFetchInitiative.mockRejectedValue(new Error("network error"));
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText(/failed to load initiative/i)).toBeTruthy();
      expect(screen.getByText(/network error/i)).toBeTruthy();
    });
  });

  it("shows 'no live sessions' when sessions array is empty", async () => {
    mockFetchInitiative.mockResolvedValue({ ...sampleDetail, sessions: [] });
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("My Test Initiative")).toBeTruthy();
    });

    expect(screen.getByText(/no live sessions/i)).toBeTruthy();
    expect(screen.getByText(/no background sessions — logs unavailable/i)).toBeTruthy();
  });

  it("attach button calls attachToInitiative with correct args", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    mockAttachToInitiative.mockResolvedValue({ ok: true });

    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("My Test Initiative")).toBeTruthy();
    });

    // There is exactly one attach button (one bg session in sampleDetail).
    const attachBtn = screen.getByRole("button", { name: /^attach$/i });
    fireEvent.click(attachBtn);

    // Must pass the short id (s.id = "s1"), not the full sessionId ("sess-1").
    await waitFor(() => {
      expect(mockAttachToInitiative).toHaveBeenCalledWith("init-abc", "s1");
    });

    // Confirmation toast
    await waitFor(() => {
      expect(screen.getByText(/launched terminal for planner/i)).toBeTruthy();
    });
  });

  it("attach button shows error toast when attach fails", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    mockAttachToInitiative.mockRejectedValue(new Error("osascript failed"));

    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("My Test Initiative")).toBeTruthy();
    });

    const attachBtn = screen.getByRole("button", { name: /^attach$/i });
    fireEvent.click(attachBtn);

    await waitFor(() => {
      expect(screen.getByText(/attach failed: osascript failed/i)).toBeTruthy();
    });
  });

  it("back button calls navigate(-1)", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("My Test Initiative")).toBeTruthy();
    });

    const backBtn = screen.getByRole("button", { name: /back/i });
    fireEvent.click(backBtn);
    expect(mockNavigate).toHaveBeenCalledWith(-1);
  });

  it("renders work beads with status, priority, and type", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("Implement the thing")).toBeTruthy();
    });

    expect(screen.getByText("P1")).toBeTruthy();
    expect(screen.getByText("task")).toBeTruthy();
  });

  it("renders all notes with descending numeric labels", async () => {
    mockFetchInitiative.mockResolvedValue(sampleDetail);
    render(<DrillInView />);

    await waitFor(() => {
      expect(screen.getByText("Third note")).toBeTruthy();
    });

    const items = screen.getAllByRole("listitem");
    const noteItems = items.filter((el) => el.className.includes("notes-item"));
    // notesHistory has 3 entries; most recent at top — "Third note" is index 0, label = 3
    expect(noteItems.length).toBe(3);
    expect(noteItems[0]?.textContent).toContain("3");
    expect(noteItems[0]?.textContent).toContain("Third note");
    // Oldest ("First note") is last, label = 1
    expect(noteItems[2]?.textContent).toContain("1");
    expect(noteItems[2]?.textContent).toContain("First note");
  });
});
