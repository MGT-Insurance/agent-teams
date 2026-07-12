import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import type { InitiativeNode, MailMessage, ParsedInitiative } from "@agent-teams/shared";
import type { SnapshotState } from "../../hooks/useSnapshot.js";

// Snapshot context is mocked so we control the initiatives (recipient picker) directly.
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

// api is mocked so tests control fetchMail/sendMail directly (agent-teams-hw71.4).
const mockFetchMail = vi.hoisted(() => vi.fn());
const mockSendMail = vi.hoisted(() => vi.fn());
vi.mock("../../lib/api.js", () => ({
  fetchMail: mockFetchMail,
  sendMail: mockSendMail,
}));

import MailView from "./index.js";

function renderMail() {
  return render(<MailView />);
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

function makeNode(over: Partial<ParsedInitiative> = {}): InitiativeNode {
  return {
    initiative: makeInitiative(over),
    session: null,
    activity: "idle",
    phase: "active",
    delivery: "none",
    needsHuman: false,
    worktreeExists: true,
    sessionCount: 0,
    alert: null,
  };
}

const unreadMessage: MailMessage = {
  id: "msg-1",
  to: "init-1",
  from: "planner",
  subject: "message from planner",
  body: "Please review the plan.",
  status: "pending",
  createdAt: "2026-07-10T10:00:00Z",
  readAt: null,
  readBy: null,
  thread: null,
};

const readMessage: MailMessage = {
  id: "msg-2",
  to: "init-2",
  from: "implementer",
  subject: "message from implementer",
  body: "Done with the task.",
  status: "read",
  createdAt: "2026-07-09T09:00:00Z",
  readAt: "2026-07-09T09:05:00Z",
  readBy: "Eric",
  thread: null,
};

beforeEach(() => {
  mockFetchMail.mockReset();
  mockSendMail.mockReset();
  mockFetchMail.mockResolvedValue({ messages: [unreadMessage, readMessage] });
  mockState.initiatives = [
    makeNode({ id: "init-1", title: "Open Initiative", status: "open" }),
    makeNode({ id: "init-2", title: "Closed Initiative", status: "closed" }),
    makeNode({ id: "init-3", title: "In Progress Initiative", status: "in_progress" }),
  ];
});

afterEach(() => {
  cleanup();
});

describe("MailView — message list", () => {
  it("renders an unread and a read message with correct who/when cells, newest-first", async () => {
    const { container } = renderMail();
    await waitFor(() => expect(container.querySelectorAll(".mail-row")).toHaveLength(2));

    const rows = container.querySelectorAll(".mail-row");
    // unreadMessage (2026-07-10) is newer than readMessage (2026-07-09) -> sorts first.
    const firstCells = rows[0]!.querySelectorAll("td");
    expect(firstCells[0]!.textContent).toBe("init-1"); // To
    expect(firstCells[1]!.textContent).toBe("planner"); // From
    expect(firstCells[3]!.textContent).toBe("pending"); // Status
    expect(firstCells[5]!.textContent).toBe("—"); // Read At — unread
    expect(firstCells[6]!.textContent).toBe("—"); // Read By — unread
    expect(rows[0]!.classList.contains("mail-row--unread")).toBe(true);

    const secondCells = rows[1]!.querySelectorAll("td");
    expect(secondCells[0]!.textContent).toBe("init-2");
    expect(secondCells[1]!.textContent).toBe("implementer");
    expect(secondCells[3]!.textContent).toBe("read");
    expect(secondCells[5]!.textContent).not.toBe("—"); // Read At — present
    expect(secondCells[6]!.textContent).toBe("Eric"); // Read By
    expect(rows[1]!.classList.contains("mail-row--read")).toBe(true);
  });

  it("expands a row to reveal the full message body on click", async () => {
    const { container } = renderMail();
    await waitFor(() => expect(container.querySelectorAll(".mail-row")).toHaveLength(2));

    const rows = container.querySelectorAll(".mail-row");
    fireEvent.click(rows[0]!);
    expect(screen.getByText("Please review the plan.")).toBeTruthy();
  });
});

describe("MailView — recipient picker", () => {
  it("lists only non-closed initiatives, showing the name", async () => {
    renderMail();
    await waitFor(() => expect(mockFetchMail).toHaveBeenCalled());

    const select = screen.getByLabelText(/recipient initiative/i) as HTMLSelectElement;
    const options = Array.from(select.options).map((o) => o.textContent);
    expect(options).toContain("Open Initiative (init-1)");
    expect(options).toContain("In Progress Initiative (init-3)");
    expect(options).not.toContain("Closed Initiative (init-2)");
  });
});

describe("MailView — send form submission", () => {
  it("calls sendMail with the picked initiative id and body", async () => {
    mockSendMail.mockResolvedValue({ ok: true, messageId: "msg-99", recipient: "init-1" });
    renderMail();
    await waitFor(() => expect(mockFetchMail).toHaveBeenCalled());

    const select = screen.getByLabelText(/recipient initiative/i) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "init-1" } });
    const textarea = screen.getByLabelText(/message body/i);
    fireEvent.change(textarea, { target: { value: "Hello there" } });
    fireEvent.click(screen.getByRole("button", { name: /^send$/i }));

    await waitFor(() =>
      expect(mockSendMail).toHaveBeenCalledWith({ to: "init-1", body: "Hello there" }),
    );
  });

  it("shows the returned messageId on success and re-fetches mail", async () => {
    mockSendMail.mockResolvedValue({ ok: true, messageId: "msg-99", recipient: "init-1" });
    renderMail();
    await waitFor(() => expect(mockFetchMail).toHaveBeenCalledTimes(1));

    const select = screen.getByLabelText(/recipient initiative/i) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "init-1" } });
    fireEvent.change(screen.getByLabelText(/message body/i), { target: { value: "Hi" } });
    fireEvent.click(screen.getByRole("button", { name: /^send$/i }));

    await screen.findByText(/msg-99/);
    await waitFor(() => expect(mockFetchMail).toHaveBeenCalledTimes(2));
  });

  it("surfaces the error message on failure", async () => {
    mockSendMail.mockRejectedValue(new Error("invalid recipient initiative id"));
    renderMail();
    await waitFor(() => expect(mockFetchMail).toHaveBeenCalled());

    const select = screen.getByLabelText(/recipient initiative/i) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "init-1" } });
    fireEvent.change(screen.getByLabelText(/message body/i), { target: { value: "Hi" } });
    fireEvent.click(screen.getByRole("button", { name: /^send$/i }));

    await screen.findByText(/invalid recipient initiative id/i);
  });
});
