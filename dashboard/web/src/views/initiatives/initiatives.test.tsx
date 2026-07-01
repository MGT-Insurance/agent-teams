import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from "vitest";
import { render, screen, cleanup, fireEvent, within, waitFor, act } from "@testing-library/react";
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

// api is mocked so LaunchButton tests can control the resolved/rejected value.
// vi.hoisted() ensures the variable is initialized before the mock factory runs
// (vi.mock factories are hoisted to the top of the file by the transform).
const mockLaunchSession = vi.hoisted(() => vi.fn<() => Promise<{ ok: true }>>());
vi.mock("../../lib/api.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/api.js")>();
  return { ...actual, launchSession: mockLaunchSession };
});

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
    epic: null,
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

// Process has exited — `status` absent, lingering in `claude agents --all`. Dead.
const deadSession: SessionState = {
  cwd: "/wt/init-1",
  kind: "background",
  startedAt: 0,
  sessionId: "s3",
  state: "done",
};

// Alive session with a valid short 8-hex id — attach should be offered.
const aliveWithId: SessionState = {
  id: "ab12cd34", // valid 8-hex
  cwd: "/wt/init-1",
  kind: "background",
  startedAt: 0,
  sessionId: "ab12cd34-0000-0000-0000-000000000000",
  status: "busy",
  state: "working",
};

// Detached session (no status) with a valid short 8-hex id — attach should still be offered.
const detachedWithId: SessionState = {
  id: "ff00aa11", // valid 8-hex
  cwd: "/wt/init-1",
  kind: "background",
  startedAt: 0,
  sessionId: "ff00aa11-0000-0000-0000-000000000000",
  // No `status` — session is detached (process exited, lingers in agent list).
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
    sessionCount: over.session ? 1 : 0,
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
  mockLaunchSession.mockReset();
  setInitiatives([]);
  localStorage.clear(); // toggles persist to localStorage — isolate tests
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

describe("InitiativesView — completed toggle", () => {
  beforeEach(() =>
    setInitiatives([
      makeNode({}, { id: "init-open", title: "Open one", status: "open" }),
      makeNode({}, { id: "init-closed", title: "Closed one", status: "closed" }),
      makeNode({}, { id: "init-done", title: "Done one", status: "done" }),
    ])
  );

  it("hides completed (closed/done, no live session) initiatives by default", () => {
    renderView();
    expect(screen.getByText("Open one")).toBeTruthy();
    expect(screen.queryByText("Closed one")).toBeNull();
    expect(screen.queryByText("Done one")).toBeNull();
  });

  it("reveals completed initiatives when 'Show completed' is on", () => {
    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /show completed/i }));
    expect(screen.getByText("Open one")).toBeTruthy();
    expect(screen.getByText("Closed one")).toBeTruthy();
    expect(screen.getByText("Done one")).toBeTruthy();
  });

  it("keeps a closed initiative with ANY lingering session visible (not completed)", () => {
    setInitiatives([
      makeNode({ session: workingSession }, { id: "c-alive", title: "Closed alive", status: "closed" }),
      makeNode({ session: deadSession }, { id: "c-dead", title: "Closed dead", status: "closed" }),
      makeNode({}, { id: "c-none", title: "Closed quiet", status: "closed" }),
    ]);
    renderView();
    // Show completed OFF: the two with a lingering session show; only the
    // truly-gone one (closed + no session) is hidden as "completed".
    expect(screen.getByText("Closed alive")).toBeTruthy();
    expect(screen.getByText("Closed dead")).toBeTruthy();
    expect(screen.queryByText("Closed quiet")).toBeNull();
  });
});

describe("InitiativesView — on-this-machine filter", () => {
  beforeEach(() =>
    setInitiatives([
      makeNode({ worktreeExists: true }, { id: "init-here", title: "On this host" }),
      makeNode({ worktreeExists: false }, { id: "init-elsewhere", title: "Other host" }),
    ])
  );

  it("shows all initiatives by default", () => {
    renderView();
    expect(screen.getByText("On this host")).toBeTruthy();
    expect(screen.getByText("Other host")).toBeTruthy();
  });

  it("hides off-machine initiatives when 'On this machine' is on", () => {
    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /on this machine/i }));
    expect(screen.getByText("On this host")).toBeTruthy();
    expect(screen.queryByText("Other host")).toBeNull();
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

  it("session chip = green 'running' for an open initiative with a live session", () => {
    setInitiatives([makeNode({ session: workingSession }, { id: "i", title: "Running" })]);
    renderView();
    const chip = within(screen.getByRole("button", { name: /running/i })).getByLabelText("session: running");
    expect(chip.classList.contains("init-chip--good")).toBe(true);
  });

  it("session chip = amber 'running (close it)' for a CLOSED initiative with a live session", () => {
    setInitiatives([makeNode({ session: workingSession }, { id: "i", title: "ClosedRun", status: "closed" })]);
    renderView();
    const chip = within(screen.getByRole("button", { name: /closedrun/i })).getByLabelText("session: running (close it)");
    expect(chip.classList.contains("init-chip--warn")).toBe(true);
  });

  it("session chip = amber 'dead' for an open + on-machine dead session", () => {
    setInitiatives([makeNode({ session: deadSession, worktreeExists: true }, { id: "i", title: "DeadHere" })]);
    renderView();
    const chip = within(screen.getByRole("button", { name: /deadhere/i })).getByLabelText("session: dead");
    expect(chip.classList.contains("init-chip--warn")).toBe(true);
  });

  it("session chip = muted 'dead' for an open dead session NOT on this machine", () => {
    setInitiatives([makeNode({ session: deadSession, worktreeExists: false }, { id: "i", title: "DeadAway" })]);
    renderView();
    const chip = within(screen.getByRole("button", { name: /deadaway/i })).getByLabelText("session: dead");
    expect(chip.classList.contains("init-chip--muted")).toBe(true);
  });

  it("session chip = off when there is no session", () => {
    setInitiatives([makeNode({ session: null }, { id: "i", title: "NoSess" })]);
    renderView();
    const chip = within(screen.getByRole("button", { name: /nosess/i })).getByLabelText("session: none");
    expect(chip.classList.contains("init-chip--off")).toBe(true);
  });
});

describe("InitiativesView — row alerts (anomalies needing action)", () => {
  const alertOf = (title: RegExp) =>
    screen.getByRole("button", { name: title }).getAttribute("data-alert");

  it("no alert for an open initiative with a healthy live session", () => {
    setInitiatives([makeNode({ session: workingSession, worktreeExists: true }, { id: "i", title: "Healthy" })]);
    renderView();
    expect(alertOf(/healthy/i)).toBeNull();
  });

  it("URGENT: open + on-machine + no session (stalled)", () => {
    setInitiatives([makeNode({ session: null, worktreeExists: true }, { id: "i", title: "Stalled" })]);
    renderView();
    expect(alertOf(/stalled/i)).toBe("urgent");
  });

  it("LOW: open + on-machine + dead session", () => {
    setInitiatives([makeNode({ session: deadSession, worktreeExists: true }, { id: "i", title: "OpenDead" })]);
    renderView();
    expect(alertOf(/opendead/i)).toBe("low");
  });

  it("MED: closed + alive session", () => {
    setInitiatives([makeNode({ session: workingSession }, { id: "i", title: "ClosedAlive", status: "closed" })]);
    renderView();
    expect(alertOf(/closedalive/i)).toBe("med");
  });

  it("URGENT: closed + dead session", () => {
    setInitiatives([makeNode({ session: deadSession }, { id: "i", title: "ClosedDead", status: "closed" })]);
    renderView();
    expect(alertOf(/closeddead/i)).toBe("urgent");
  });

  it("no alert for open + no session NOT on this machine (worked elsewhere)", () => {
    setInitiatives([makeNode({ session: null, worktreeExists: false }, { id: "i", title: "Elsewhere" })]);
    renderView();
    expect(alertOf(/elsewhere/i)).toBeNull();
  });

  it("URGENT (wins): multiple sessions on one worktree, even on an otherwise-healthy row", () => {
    setInitiatives([
      makeNode(
        { session: workingSession, worktreeExists: true, sessionCount: 2 },
        { id: "i", title: "MultiSess" }
      ),
    ]);
    renderView();
    expect(alertOf(/multisess/i)).toBe("urgent");
    const pop = within(screen.getByRole("button", { name: /multisess/i })).getByRole("tooltip");
    expect(pop.textContent).toMatch(/2 sessions/i);
  });

  it("renders a why+action info popover on alerted rows only", () => {
    setInitiatives([
      makeNode({ session: workingSession, worktreeExists: true }, { id: "ok", title: "Healthy" }),
      makeNode({ session: deadSession }, { id: "bad", title: "ClosedDead", status: "closed" }),
    ]);
    renderView();
    // Healthy row has no info popover.
    expect(within(screen.getByRole("button", { name: /healthy/i })).queryByRole("tooltip")).toBeNull();
    // Alerted row explains why + what to do.
    const pop = within(screen.getByRole("button", { name: /closeddead/i })).getByRole("tooltip");
    expect(pop.textContent).toMatch(/why/i);
    expect(pop.textContent).toMatch(/reap it/i);
  });
});

describe("InitiativesView — phase token", () => {
  it("keys the phase class off the phase so categories style distinctly", () => {
    setInitiatives([
      makeNode({ phase: "delivered" }, { id: "init-1", title: "Shipped one" }),
      makeNode({ phase: "active" }, { id: "init-2", title: "Working one" }),
    ]);
    renderView();
    const delivered = screen.getByText("delivered");
    const active = screen.getByText("active");
    expect(delivered.classList.contains("init-row__phase--delivered")).toBe(true);
    expect(active.classList.contains("init-row__phase--active")).toBe(true);
  });
});

describe("InitiativesView — toggle persistence", () => {
  it("persists 'Show completed' across remounts via localStorage", () => {
    setInitiatives([
      makeNode({}, { id: "init-open", title: "Open one", status: "open" }),
      makeNode({}, { id: "init-closed", title: "Closed one", status: "closed" }),
    ]);
    const { unmount } = renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /show completed/i }));
    expect(localStorage.getItem("initiatives.showCompleted")).toBe("true");
    unmount();

    renderView();
    expect(
      (screen.getByRole("checkbox", { name: /show completed/i }) as HTMLInputElement).checked
    ).toBe(true);
    expect(screen.getByText("Closed one")).toBeTruthy();
  });

  it("persists 'On this machine' across remounts via localStorage", () => {
    setInitiatives([
      makeNode({ worktreeExists: true }, { id: "init-here", title: "On this host" }),
      makeNode({ worktreeExists: false }, { id: "init-elsewhere", title: "Other host" }),
    ]);
    const { unmount } = renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /on this machine/i }));
    expect(localStorage.getItem("initiatives.onlyOnMachine")).toBe("true");
    unmount();

    renderView();
    expect(
      (screen.getByRole("checkbox", { name: /on this machine/i }) as HTMLInputElement).checked
    ).toBe(true);
    expect(screen.queryByText("Other host")).toBeNull();
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

describe("LaunchButton — core paths", () => {
  // The LaunchButton renders for open + on-machine + no-session rows.
  function makeStallNode(id: string, title: string): InitiativeNode {
    return makeNode({ worktreeExists: true, session: null }, { id, title });
  }

  it("reaches error state and surfaces reason when launchSession rejects", async () => {
    mockLaunchSession.mockRejectedValueOnce(
      new Error("ateam resume exited with code 1\nextra detail\nLog: /home/.agent-teams/logs/launch-x.log")
    );
    setInitiatives([makeStallNode("at-fail", "Fail Launch")]);
    renderView();

    // Idle: launch button visible.
    const launchBtn = screen.getByRole("button", { name: "launch" });
    fireEvent.click(launchBtn);

    // After reject, button should flip to ✗ and show the first error line.
    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "✗" })).toBeTruthy();
    });

    const errBtn = screen.getByRole("button", { name: "✗" });
    expect(errBtn.getAttribute("title")).toMatch(/exited with code 1/);
    // First-line error text renders inline next to the button.
    expect(screen.getByText(/ateam resume exited with code 1/)).toBeTruthy();
  });

  it("reaches ok state when launchSession resolves", async () => {
    mockLaunchSession.mockResolvedValueOnce({ ok: true });
    setInitiatives([makeStallNode("at-ok", "OK Launch")]);
    renderView();

    const launchBtn = screen.getByRole("button", { name: "launch" });
    fireEvent.click(launchBtn);

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "✓" })).toBeTruthy();
    });
    // No error text should appear on success.
    expect(screen.queryByText(/exited with code/i)).toBeNull();
  });
});

describe("LaunchButton — edge cases", () => {
  function makeStallNode(id: string, title: string): InitiativeNode {
    return makeNode({ worktreeExists: true, session: null }, { id, title });
  }

  afterEach(() => {
    vi.useRealTimers();
  });

  it("clicking while pending is a no-op: launchSession is called exactly once", async () => {
    // Block launchSession from ever resolving so the button stays in pending state.
    (mockLaunchSession as Mock).mockImplementation(() => new Promise(() => {}));
    setInitiatives([makeStallNode("at-dbl", "Double Click")]);
    renderView();

    const launchBtn = screen.getByRole("button", { name: "launch" });
    fireEvent.click(launchBtn);

    // Second click while pending.
    await waitFor(() => screen.getByRole("button", { name: "…" }));
    fireEvent.click(screen.getByRole("button", { name: "…" }));

    expect(mockLaunchSession).toHaveBeenCalledTimes(1);
  });

  it("error title contains the full multi-line error message", async () => {
    const fullMessage =
      "ateam resume exited with code 1\ninitiative at-ggz is closed\nLog: /logs/launch.log";
    mockLaunchSession.mockRejectedValueOnce(new Error(fullMessage));
    setInitiatives([makeStallNode("at-title", "Title Check")]);
    renderView();

    fireEvent.click(screen.getByRole("button", { name: "launch" }));

    await waitFor(() => screen.getByRole("button", { name: "✗" }));
    const errBtn = screen.getByRole("button", { name: "✗" });
    // Title must carry the full multi-line message so it's inspectable on hover.
    expect(errBtn.getAttribute("title")).toBe(fullMessage);
  });

  it("only the first error line renders inline; detail and Log lines do not", async () => {
    const fullMessage =
      "ateam resume exited with code 1\nextra detail\nLog: /logs/launch.log";
    mockLaunchSession.mockRejectedValueOnce(new Error(fullMessage));
    setInitiatives([makeStallNode("at-inline", "Inline Check")]);
    renderView();

    fireEvent.click(screen.getByRole("button", { name: "launch" }));

    await waitFor(() => screen.getByRole("button", { name: "✗" }));
    // First line is visible inline.
    expect(screen.getByText("ateam resume exited with code 1")).toBeTruthy();
    // Detail and Log lines must NOT be rendered as text nodes.
    expect(screen.queryByText("extra detail")).toBeNull();
    expect(screen.queryByText(/Log: \/logs/)).toBeNull();
  });

  it("ok state auto-resets to idle after 3s", async () => {
    vi.useFakeTimers();
    mockLaunchSession.mockResolvedValueOnce({ ok: true });
    setInitiatives([makeStallNode("at-reset-ok", "Reset OK")]);
    renderView();

    // Click and flush the launchSession microtask so we reach ok state.
    // advanceTimersByTimeAsync advances clock AND flushes pending microtasks/promises
    // between ticks; wrapping in act ensures React commits the resulting state updates.
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "launch" }));
      // No timers in first 50ms — this just drains the microtask queue.
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(screen.getByRole("button", { name: "✓" })).toBeTruthy();

    // Advance past the 3s ok→idle reset timer.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3100);
    });
    expect(screen.getByRole("button", { name: "launch" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "✓" })).toBeNull();
  });

  it("err state auto-resets to idle after 5s and clears the error message", async () => {
    vi.useFakeTimers();
    mockLaunchSession.mockRejectedValueOnce(
      new Error("ateam resume exited with code 1"),
    );
    setInitiatives([makeStallNode("at-reset-err", "Reset Err")]);
    renderView();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "launch" }));
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(screen.getByRole("button", { name: "✗" })).toBeTruthy();
    expect(screen.getByText("ateam resume exited with code 1")).toBeTruthy();

    // Advance past the 5s err→idle reset timer.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5100);
    });
    expect(screen.getByRole("button", { name: "launch" })).toBeTruthy();
    expect(screen.queryByText(/exited with code/i)).toBeNull();
  });
});

describe("InitiativesView — launch vs attach button (agent-teams-u9f2)", () => {
  // Helper: query for the attach or launch button inside a specific row.
  function getRowButton(rowTitle: RegExp, btnLabel: RegExp) {
    return within(screen.getByRole("button", { name: rowTitle })).queryByRole("button", { name: btnLabel });
  }

  it("offers Attach when the session is alive and has a valid 8-hex id", () => {
    setInitiatives([makeNode({ session: aliveWithId, worktreeExists: true }, { id: "a1", title: "AliveWithId" })]);
    renderView();
    expect(getRowButton(/alivewithid/i, /attach/i)).toBeTruthy();
    expect(getRowButton(/alivewithid/i, /launch/i)).toBeNull();
  });

  it("offers Attach when the session is detached (no status) but has a valid 8-hex id", () => {
    setInitiatives([makeNode({ session: detachedWithId, worktreeExists: true }, { id: "d1", title: "DetachedWithId" })]);
    renderView();
    expect(getRowButton(/detachedwithid/i, /attach/i)).toBeTruthy();
    expect(getRowButton(/detachedwithid/i, /launch/i)).toBeNull();
  });

  it("offers Launch (not Attach) when no session entry exists but worktree is on machine", () => {
    setInitiatives([makeNode({ session: null, worktreeExists: true }, { id: "n1", title: "NoSession" })]);
    renderView();
    expect(getRowButton(/nosession/i, /launch/i)).toBeTruthy();
    expect(getRowButton(/nosession/i, /attach/i)).toBeNull();
  });

  it("offers Launch when the session has no valid 8-hex id and worktree is on machine", () => {
    // deadSession has no `id` field at all -> no attach id -> fall through to Launch.
    setInitiatives([makeNode({ session: deadSession, worktreeExists: true }, { id: "nd", title: "NoId" })]);
    renderView();
    expect(getRowButton(/noid/i, /launch/i)).toBeTruthy();
    expect(getRowButton(/noid/i, /attach/i)).toBeNull();
  });

  it("offers neither button when no valid session id and worktree is not on machine", () => {
    setInitiatives([makeNode({ session: null, worktreeExists: false }, { id: "x1", title: "OffMachine" })]);
    renderView();
    expect(getRowButton(/offmachine/i, /attach/i)).toBeNull();
    expect(getRowButton(/offmachine/i, /launch/i)).toBeNull();
  });
});
