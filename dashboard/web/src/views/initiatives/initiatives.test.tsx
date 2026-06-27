import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { SnapshotState } from "../../hooks/useSnapshot.js";
import type { InitiativeNode, ParsedInitiative, SessionState } from "@agent-teams/shared";

// Snapshot context is mocked so we control the initiatives data directly.
const mockState: SnapshotState = {
  initiatives: [],
  unmatchedSessions: [],
  inbox: [],
  ts: null,
  connectionState: "connected",
  error: null,
};

vi.mock("../../SnapshotContext.js", () => ({
  useSnapshotContext: () => mockState,
}));

// useNavigate is mocked so we can assert navigation without a real router.
const mockNavigate = vi.fn();
vi.mock("react-router-dom", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router-dom")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

import InitiativesView from "./index.js";

function renderView() {
  return render(
    <MemoryRouter>
      <InitiativesView />
    </MemoryRouter>
  );
}

function makeInitiative(over: Partial<ParsedInitiative> = {}): ParsedInitiative {
  return {
    id: "init-1",
    title: "Test initiative",
    description: "",
    notes: "",
    status: "open",
    priority: "1",
    issue_type: "feature",
    owner: "Eric",
    created_at: "2026-06-26",
    updated_at: "2026-06-26",
    problem: "",
    repo: "repo",
    worktree: "/wt/init-1",
    branch: "init-1",
    team: "",
    mode: "",
    goal: "",
    prUrl: null,
    ...over,
  };
}

const workingSession: SessionState = {
  cwd: "/wt/init-1",
  kind: "background",
  startedAt: 0,
  sessionId: "s1",
  status: "busy",
  state: "working",
};

// A parked agent blocked on a human gate — still a live, existing session.
const waitingSession: SessionState = {
  cwd: "/wt/init-1",
  kind: "background",
  startedAt: 0,
  sessionId: "s2",
  status: "waiting",
  state: "blocked",
};

function makeNode(over: Partial<InitiativeNode> = {}, init: Partial<ParsedInitiative> = {}): InitiativeNode {
  return {
    initiative: makeInitiative(init),
    session: null,
    activity: "idle",
    phase: "executing",
    delivery: "none",
    needsHuman: false,
    worktreeExists: false,
    ...over,
  };
}

function setInitiatives(nodes: InitiativeNode[], extra: Partial<SnapshotState> = {}) {
  mockState.initiatives = nodes;
  mockState.connectionState = extra.connectionState ?? "connected";
  mockState.error = extra.error ?? null;
}

beforeEach(() => {
  mockNavigate.mockReset();
  setInitiatives([]);
});

afterEach(() => {
  cleanup();
});

describe("InitiativesView — list rendering", () => {
  it("renders a row per initiative from the snapshot", () => {
    setInitiatives([
      makeNode({}, { id: "init-1", title: "Alpha feature" }),
      makeNode({}, { id: "init-2", title: "Beta feature" }),
    ]);
    renderView();
    expect(screen.getByText("Alpha feature")).toBeTruthy();
    expect(screen.getByText("Beta feature")).toBeTruthy();
    expect(screen.getByText("init-1")).toBeTruthy();
    expect(screen.getByText("init-2")).toBeTruthy();
  });

  it("navigates to /initiative/:id on row click", () => {
    setInitiatives([makeNode({}, { id: "init-7", title: "Click me" })]);
    renderView();
    const row = screen.getByRole("button", { name: /click me/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-7");
  });

  it("shows an empty state when there are no initiatives", () => {
    setInitiatives([]);
    renderView();
    expect(screen.getByText(/no initiatives/i)).toBeTruthy();
  });
});

describe("InitiativesView — search", () => {
  beforeEach(() =>
    setInitiatives([
      makeNode({}, { id: "init-1", title: "Refactor auth" }),
      makeNode({}, { id: "init-2", title: "Dashboard polish" }),
    ])
  );

  it("filters rows by title substring (case-insensitive)", () => {
    renderView();
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "auth" } });
    expect(screen.getByText("Refactor auth")).toBeTruthy();
    expect(screen.queryByText("Dashboard polish")).toBeNull();
  });

  it("filters rows by id substring", () => {
    renderView();
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "init-2" } });
    expect(screen.getByText("Dashboard polish")).toBeTruthy();
    expect(screen.queryByText("Refactor auth")).toBeNull();
  });

  it("shows the no-match empty state when search matches nothing", () => {
    renderView();
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "zzz" } });
    expect(screen.getByText(/no initiatives match/i)).toBeTruthy();
  });
});

describe("InitiativesView — closed toggle", () => {
  beforeEach(() =>
    setInitiatives([
      makeNode({}, { id: "init-open", title: "Open one", status: "open" }),
      makeNode({}, { id: "init-closed", title: "Closed one", status: "closed" }),
      makeNode({}, { id: "init-done", title: "Done one", status: "done" }),
    ])
  );

  it("hides closed and done initiatives by default", () => {
    renderView();
    expect(screen.getByText("Open one")).toBeTruthy();
    expect(screen.queryByText("Closed one")).toBeNull();
    expect(screen.queryByText("Done one")).toBeNull();
  });

  it("reveals closed and done initiatives when 'Show closed' is on", () => {
    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /show closed/i }));
    expect(screen.getByText("Open one")).toBeTruthy();
    expect(screen.getByText("Closed one")).toBeTruthy();
    expect(screen.getByText("Done one")).toBeTruthy();
  });
});

describe("InitiativesView — signal chips", () => {
  it("lights 'on machine' when worktreeExists is true", () => {
    setInitiatives([makeNode({ worktreeExists: true }, { id: "init-1", title: "On machine" })]);
    renderView();
    const row = screen.getByRole("button", { name: /on machine/i });
    const chip = within(row).getByLabelText("on machine: yes");
    expect(chip.classList.contains("init-chip--on")).toBe(true);
  });

  it("dims 'on machine' when worktreeExists is false", () => {
    setInitiatives([makeNode({ worktreeExists: false }, { id: "init-1", title: "Off machine" })]);
    renderView();
    const row = screen.getByRole("button", { name: /off machine/i });
    const chip = within(row).getByLabelText("on machine: no");
    expect(chip.classList.contains("init-chip--off")).toBe(true);
  });

  it("renders an open-PR link when delivery is pr-open and prUrl is present", () => {
    setInitiatives([
      makeNode(
        { delivery: "pr-open" },
        { id: "init-1", title: "Has PR", prUrl: "https://github.com/org/repo/pull/5" }
      ),
    ]);
    renderView();
    const link = screen.getByRole("link", { name: /open pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/org/repo/pull/5");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("does not navigate the row when the PR link is clicked", () => {
    setInitiatives([
      makeNode(
        { delivery: "pr-open" },
        { id: "init-1", title: "Has PR", prUrl: "https://github.com/org/repo/pull/5" }
      ),
    ]);
    renderView();
    fireEvent.click(screen.getByRole("link", { name: /open pr/i }));
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it("lights 'session' when a working session is present", () => {
    setInitiatives([makeNode({ session: workingSession }, { id: "init-1", title: "Running" })]);
    renderView();
    const row = screen.getByRole("button", { name: /running/i });
    const chip = within(row).getByLabelText("session: yes");
    expect(chip.classList.contains("init-chip--on")).toBe(true);
  });

  it("lights 'session' when a waiting (parked) session is present", () => {
    setInitiatives([makeNode({ session: waitingSession }, { id: "init-1", title: "Parked" })]);
    renderView();
    const row = screen.getByRole("button", { name: /parked/i });
    const chip = within(row).getByLabelText("session: yes");
    expect(chip.classList.contains("init-chip--on")).toBe(true);
  });

  it("dims 'session' when there is no session", () => {
    setInitiatives([makeNode({ session: null }, { id: "init-1", title: "No session" })]);
    renderView();
    const row = screen.getByRole("button", { name: /no session/i });
    const chip = within(row).getByLabelText("session: no");
    expect(chip.classList.contains("init-chip--off")).toBe(true);
  });
});

describe("InitiativesView — disconnected states", () => {
  it("shows a reconnecting banner when reconnecting", () => {
    setInitiatives([], { connectionState: "reconnecting" });
    renderView();
    expect(screen.getByText(/reconnecting/i)).toBeTruthy();
  });

  it("shows an error banner with message when connectionState is error", () => {
    setInitiatives([], { connectionState: "error", error: "SSE stream closed" });
    renderView();
    expect(screen.getByText(/SSE stream closed/i)).toBeTruthy();
  });
});
