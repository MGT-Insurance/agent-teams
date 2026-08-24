import { describe, it, expect, vi, beforeEach, afterEach, type Mock } from "vitest";
import { render, screen, cleanup, fireEvent, within, waitFor, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { SnapshotState } from "../../hooks/useSnapshot.js";
import type { Alert, InitiativeNode, ParsedInitiative, SessionState } from "@agent-teams/shared";

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

function rerenderView(view: ReturnType<typeof renderView>) {
  view.rerender(
    <MemoryRouter>
      <InitiativesView />
    </MemoryRouter>
  );
}

function initiativeIdentity(title: string) {
  return screen.getByRole("button", { name: `Open initiative: ${title}` });
}

function queryInitiativeIdentity(title: string) {
  return screen.queryByRole("button", { name: `Open initiative: ${title}` });
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
    prs: [],
    prReviews: [],
    epic: null,
    // ParsedInitiative carries the CLI-parsed routing fields (agent-teams-ully.12).
    // The view reads the flattened members above, so an empty object is honest here.
    fields: {},
    ...over,
  };
}

// The browser must render a future producer gate honestly even though today's
// shared union enumerates only the gates currently emitted by the CLI.
function forwardCompatiblePRReview(pr: string, gate: string): ParsedInitiative["prReviews"][number] {
  return { pr, gate } as unknown as ParsedInitiative["prReviews"][number];
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

// Alert fixtures — the view now renders node.alert directly (server-derived,
// see server/src/parse.ts deriveAlert). Text carries over VERBATIM from the
// former client-side rowAlert/alertInfo case trees (CONTRACT B).
const stalledAlert: Alert = {
  level: "urgent",
  reason: "Open with a worktree on this machine, but nothing is running — stalled.",
  action: "Resume the session, or close the initiative if it's abandoned.",
};

const openDeadAlert: Alert = {
  level: "low",
  reason: "The session has exited — it won't receive messages.",
  action: "Resume it, or close out the initiative.",
};

const closedAliveAlert: Alert = {
  level: "med",
  reason: "Closed, but a session is still running on it.",
  action: "Close the session — the work is done.",
};

const closedDeadAlert: Alert = {
  level: "urgent",
  reason: "Closed, but a finished session is still lingering in the agent list.",
  action: "Reap it (claude stop) so it clears out.",
};

const multiSessionAlert: Alert = {
  level: "urgent",
  reason: "2 sessions are attached to this worktree — a conflict.",
  action: "Stop the extras (claude stop) — only one session should run per worktree.",
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
    alert: null,
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
    expect(initiativeIdentity("Alpha feature")).toBeTruthy();
    expect(initiativeIdentity("Beta feature")).toBeTruthy();
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

describe("InitiativesView — kanban DOM integration", () => {
  it("renders the seven states in order, one lane per filtered initiative, and only direct or sole fallback cards", () => {
    setInitiatives([
      makeNode(
        {
          workstreams: [
            {
              id: "ws-api",
              title: "API delivery",
              status: "open",
              issueType: "feature",
              priority: "P1",
              labels: [],
              progress: { total: 2, closed: 1 },
              memberIds: ["ws-api", "api-child"],
              sourceOrder: 0,
            },
            {
              id: "ws-web",
              title: "Web delivery",
              status: "in_progress",
              issueType: "task",
              priority: "P2",
              labels: [],
              progress: { total: 0, closed: 0 },
              memberIds: ["ws-web"],
              sourceOrder: 1,
            },
          ],
        },
        { id: "init-direct", title: "Direct initiative" },
      ),
      makeNode({}, { id: "init-fallback", title: "Fallback initiative" }),
    ]);

    const { container } = renderView();
    const labels = Array.from(
      container.querySelectorAll(".initiative-board__header .initiative-board__column-header > span:first-child"),
      (element) => element.textContent,
    );
    expect(labels).toEqual([
      "Planned",
      "Active",
      "Verifying",
      "Your Review",
      "External Review",
      "Blocked",
      "Done",
    ]);

    const laneList = screen.getByRole("list", { name: "Initiative swimlanes" });
    expect(laneList.children).toHaveLength(2);
    expect(initiativeIdentity("Direct initiative")).toBeTruthy();
    expect(initiativeIdentity("Fallback initiative")).toBeTruthy();
    expect(screen.getAllByRole("article")).toHaveLength(3);
    expect(screen.getByRole("article", { name: "API delivery, Planned" })).toBeTruthy();
    expect(screen.getByRole("article", { name: "Web delivery, Active" })).toBeTruthy();
    expect(screen.getByRole("article", { name: "Fallback initiative, Planned" })).toBeTruthy();
    expect(screen.queryByText("Fallback initiative:init-direct")).toBeNull();
    expect(screen.getByText("Fallback initiative:init-fallback")).toBeTruthy();

    // The fallback title intentionally appears as the lane heading and card heading,
    // but the stable fallback card itself appears exactly once.
    expect(screen.getAllByText("Fallback initiative")).toHaveLength(2);
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "fallback" } });
    expect(laneList.children).toHaveLength(1);
    expect(queryInitiativeIdentity("Direct initiative")).toBeNull();
    expect(screen.getAllByRole("article")).toHaveLength(1);
  });

  it("keeps one stable workstream card while PR attachment moves it through every state", () => {
    const href = "https://github.com/example/dashboard/pull/101";
    const stableNode = (status: string, gate?: string) =>
      makeNode(
        {
          workstreams: [
            {
              id: "ws-stable",
              title: "Persistent stream",
              status,
              issueType: "task",
              priority: "P1",
              labels: [],
              progress: { total: 3, closed: 1 },
              memberIds: ["ws-stable"],
            },
          ],
        },
        {
          id: "init-stable",
          title: "Stable initiative",
          prs: gate === undefined ? [] : [href],
          prReviews: gate === undefined ? [] : [forwardCompatiblePRReview(href, gate)],
          prWorkstreams: gate === undefined ? [] : [{ pr: href, workstream: "ws-stable" }],
        },
      );

    const assertOnlyStableCard = (columnLabel: string, expectedLinks: number) => {
      const cards = screen.getAllByRole("article");
      expect(cards).toHaveLength(1);
      expect(cards[0]?.getAttribute("aria-label")).toBe(`Persistent stream, ${columnLabel}`);
      expect(cards[0]?.getAttribute("data-column")).toBe(columnLabel.toLowerCase().replace(" ", "-"));
      expect(within(cards[0] as HTMLElement).getByText("Bead ws-stable")).toBeTruthy();
      expect(within(cards[0] as HTMLElement).queryAllByRole("link")).toHaveLength(expectedLinks);
    };

    setInitiatives([stableNode("open")]);
    const view = renderView();
    assertOnlyStableCard("Planned", 0);

    const scroller = screen.getByRole("region", { name: /seven-column initiative swimlane board/i });
    scroller.focus();
    scroller.scrollLeft = 271;

    setInitiatives([stableNode("in_progress")]);
    rerenderView(view);
    assertOnlyStableCard("Active", 0);
    expect(document.activeElement).toBe(scroller);
    expect(scroller.scrollLeft).toBe(271);

    for (const scenario of [
      { status: "in_progress", gate: "future-gate", label: "Verifying" },
      { status: "in_progress", gate: "review", label: "Your Review" },
      { status: "in_progress", gate: "external", label: "External Review" },
      { status: "in_progress", gate: "question", label: "Blocked" },
      { status: "closed", gate: "question", label: "Done" },
    ]) {
      setInitiatives([stableNode(scenario.status, scenario.gate)]);
      rerenderView(view);
      assertOnlyStableCard(scenario.label, 1);
      expect(screen.getByRole("link", { name: new RegExp(`gate ${scenario.gate}`, "i") })).toBeTruthy();
    }
  });

  it("applies multi-PR precedence, exposes unknown gates, and renders safe accessible destinations", () => {
    const prs = [
      "https://github.com/example/dashboard/pull/201",
      "https://github.com/example/dashboard/pull/202",
      "https://github.com/example/dashboard/pull/203",
      "https://github.com/example/dashboard/pull/204",
    ];
    setInitiatives([
      makeNode(
        {
          workstreams: [{
            id: "ws-many-prs",
            title: "Many PR stream",
            status: "in_progress",
            labels: [],
            progress: { total: 0, closed: 0 },
            memberIds: ["ws-many-prs"],
          }],
        },
        {
          id: "init-many-prs",
          title: "Many PR initiative",
          prs,
          prReviews: [
            forwardCompatiblePRReview(prs[0] as string, "external"),
            forwardCompatiblePRReview(prs[1] as string, "vendor-hold"),
            forwardCompatiblePRReview(prs[2] as string, "review"),
            forwardCompatiblePRReview(prs[3] as string, "question"),
          ],
          prWorkstreams: prs.map((pr) => ({ pr, workstream: "ws-many-prs" })),
        },
      ),
    ]);

    renderView();
    const card = screen.getByRole("article", { name: "Many PR stream, Blocked" });
    expect(screen.getAllByRole("article")).toHaveLength(1);
    expect(within(card).getByText("Pull requests (4)")).toBeTruthy();
    expect(within(card).getByText("Gate: vendor-hold")).toBeTruthy();
    const links = within(card).getAllByRole("link", { name: /open pr example\/dashboard/i });
    expect(links).toHaveLength(4);
    for (const [index, link] of links.entries()) {
      expect(link.getAttribute("href")).toBe(prs[index]);
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toBe("noopener noreferrer");
    }
  });

  it("renders unassigned and stale diagnostics without losing descendant progress or total accounting", () => {
    const assigned = "https://github.com/example/dashboard/pull/301";
    const omitted = "https://github.com/example/dashboard/pull/302";
    const stale = "https://github.com/example/dashboard/pull/303";
    const missing = "https://github.com/example/dashboard/pull/304";
    setInitiatives([
      makeNode(
        {
          workstreams: [{
            id: "ws-diagnostic",
            title: "Diagnostic stream",
            status: "open",
            labels: [],
            progress: { total: 5, closed: 2 },
            memberIds: ["ws-diagnostic", "nested-diagnostic"],
          }],
          workstreamDiagnostics: [
            { kind: "orphan", message: "Parent not found", beadId: "orphan-1" },
            { kind: "load-error", message: "Using last-known-good workstreams" },
          ],
        },
        {
          id: "init-diagnostic",
          title: "Diagnostic initiative",
          prs: [assigned, omitted, stale],
          prReviews: [
            forwardCompatiblePRReview(assigned, "review"),
            forwardCompatiblePRReview(omitted, ""),
            forwardCompatiblePRReview(stale, "vendor-hold"),
          ],
          prWorkstreams: [
            { pr: assigned, workstream: "nested-diagnostic" },
            { pr: stale, workstream: "deleted-stream" },
            { pr: missing, workstream: "ws-diagnostic" },
          ],
        },
      ),
    ]);

    const { container } = renderView();
    const card = screen.getByRole("article", { name: "Diagnostic stream, Your Review" });
    expect(screen.getAllByRole("article")).toHaveLength(1);
    expect(within(card).getByText("2 / 5 closed")).toBeTruthy();
    expect(within(card).getAllByRole("link")).toHaveLength(1);

    const diagnostics = screen.getByRole("complementary", { name: "Initiative diagnostics" });
    expect(within(diagnostics).getByRole("heading", { name: "Unassigned PRs (2)" })).toBeTruthy();
    expect(within(diagnostics).getByRole("heading", { name: "Stale PR mappings (2)" })).toBeTruthy();
    expect(within(diagnostics).getByText(/missing workstream:/i)).toBeTruthy();
    expect(within(diagnostics).getByText(/missing pr:/i)).toBeTruthy();
    expect(within(diagnostics).getByText(/Parent not found/)).toBeTruthy();
    expect(within(diagnostics).getByText(/Using last-known-good workstreams/)).toBeTruthy();
    expect(within(diagnostics).getAllByRole("link")).toHaveLength(4);

    expect(container.querySelector(".initiative-board__summary")?.textContent).toBe(
      "1 workstream card across seven states",
    );
    const counts = Array.from(container.querySelectorAll<HTMLElement>(".initiative-board__cell"))
      .map((cell) => Number(cell.getAttribute("aria-label")?.match(/: (\d+) workstream/)?.[1] ?? 0));
    expect(counts).toEqual([0, 0, 0, 1, 0, 0, 0]);
    expect(counts.reduce((total, count) => total + count, 0)).toBe(1);
  });

  it("keeps reap actions and alerts visible through filters and supports keyboard drill-in navigation", () => {
    const reapSession = { ...deadSession, id: "deadbeef" };
    setInitiatives([
      makeNode(
        {
          session: reapSession,
          needsHuman: "reap",
          worktreeExists: false,
          alert: closedDeadAlert,
        },
        { id: "init-reap", title: "Reap this session", status: "closed" },
      ),
      makeNode({}, { id: "init-other", title: "Other initiative" }),
    ]);

    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /on this machine/i }));
    const identity = initiativeIdentity("Reap this session");
    expect(queryInitiativeIdentity("Other initiative")).toBeNull();
    expect(within(identity).getByRole("button", { name: "Stop session" })).toBeTruthy();
    expect(within(identity).getByRole("tooltip").textContent).toMatch(/reap it/i);
    fireEvent.keyDown(identity, { key: "Enter" });
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-reap");

    const scroller = screen.getByRole("region", { name: /seven-column initiative swimlane board/i });
    expect(scroller.getAttribute("tabindex")).toBe("0");
    const card = screen.getByRole("article", { name: "Reap this session, Done" });
    expect(card.getAttribute("role")).toBeNull();
    expect(within(card).queryByRole("button")).toBeNull();
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
    expect(initiativeIdentity("Refactor auth")).toBeTruthy();
    expect(queryInitiativeIdentity("Dashboard polish")).toBeNull();
  });

  it("filters rows by id substring", () => {
    renderView();
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "init-2" } });
    expect(initiativeIdentity("Dashboard polish")).toBeTruthy();
    expect(queryInitiativeIdentity("Refactor auth")).toBeNull();
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
    expect(initiativeIdentity("Open one")).toBeTruthy();
    expect(queryInitiativeIdentity("Closed one")).toBeNull();
    expect(queryInitiativeIdentity("Done one")).toBeNull();
  });

  it("reveals completed initiatives when 'Show completed' is on", () => {
    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /show completed/i }));
    expect(initiativeIdentity("Open one")).toBeTruthy();
    expect(initiativeIdentity("Closed one")).toBeTruthy();
    expect(initiativeIdentity("Done one")).toBeTruthy();
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
    expect(initiativeIdentity("Closed alive")).toBeTruthy();
    expect(initiativeIdentity("Closed dead")).toBeTruthy();
    expect(queryInitiativeIdentity("Closed quiet")).toBeNull();
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
    expect(initiativeIdentity("On this host")).toBeTruthy();
    expect(initiativeIdentity("Other host")).toBeTruthy();
  });

  it("hides off-machine initiatives when 'On this machine' is on", () => {
    renderView();
    fireEvent.click(screen.getByRole("checkbox", { name: /on this machine/i }));
    expect(initiativeIdentity("On this host")).toBeTruthy();
    expect(queryInitiativeIdentity("Other host")).toBeNull();
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

  it("renders an open-PR link when delivery is pr-open and prs is non-empty", () => {
    setInitiatives([
      makeNode(
        { delivery: "pr-open" },
        { id: "init-1", title: "Has PR", prs: ["https://github.com/org/repo/pull/5"] }
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
        { id: "init-1", title: "Has PR", prs: ["https://github.com/org/repo/pull/5"] }
      ),
    ]);
    renderView();
    fireEvent.click(screen.getByRole("link", { name: /open pr/i }));
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  // agent-teams-ssib.9: an initiative can have more than one PR open at once —
  // the row must render a chip for every one of them, not just the first.
  it("renders a chip for every PR when the initiative has more than one open", () => {
    setInitiatives([
      makeNode(
        { delivery: "pr-open" },
        {
          id: "init-1",
          title: "Has two PRs",
          prs: [
            "https://github.com/erlloyd/pr-shepherd/pull/3",
            "https://github.com/MGT-Insurance/midgard/pull/4632",
          ],
        }
      ),
    ]);
    renderView();
    const links = screen.getAllByRole("link", { name: /open pr/i });
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "https://github.com/erlloyd/pr-shepherd/pull/3",
      "https://github.com/MGT-Insurance/midgard/pull/4632",
    ]);
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
    setInitiatives([
      makeNode({ session: null, worktreeExists: true, alert: stalledAlert }, { id: "i", title: "Stalled" }),
    ]);
    renderView();
    expect(alertOf(/stalled/i)).toBe("urgent");
  });

  it("LOW: open + on-machine + dead session", () => {
    setInitiatives([
      makeNode(
        { session: deadSession, worktreeExists: true, alert: openDeadAlert },
        { id: "i", title: "OpenDead" }
      ),
    ]);
    renderView();
    expect(alertOf(/opendead/i)).toBe("low");
  });

  it("MED: closed + alive session", () => {
    setInitiatives([
      makeNode(
        { session: workingSession, alert: closedAliveAlert },
        { id: "i", title: "ClosedAlive", status: "closed" }
      ),
    ]);
    renderView();
    expect(alertOf(/closedalive/i)).toBe("med");
  });

  it("URGENT: closed + dead session", () => {
    setInitiatives([
      makeNode(
        { session: deadSession, alert: closedDeadAlert },
        { id: "i", title: "ClosedDead", status: "closed" }
      ),
    ]);
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
        { session: workingSession, worktreeExists: true, sessionCount: 2, alert: multiSessionAlert },
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
      makeNode(
        { session: deadSession, alert: closedDeadAlert },
        { id: "bad", title: "ClosedDead", status: "closed" }
      ),
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
    expect(initiativeIdentity("Closed one")).toBeTruthy();
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
    expect(queryInitiativeIdentity("Other host")).toBeNull();
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
