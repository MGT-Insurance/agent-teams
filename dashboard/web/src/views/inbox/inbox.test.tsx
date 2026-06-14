import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { SnapshotState } from "../../hooks/useSnapshot.js";
import type { InboxItem } from "@agent-teams/shared";

// Snapshot context is mocked so we control the inbox data directly.
const mockState: SnapshotState = {
  initiatives: [],
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

import InboxView from "./index.js";

function renderInbox() {
  return render(
    <MemoryRouter>
      <InboxView />
    </MemoryRouter>
  );
}

function setInbox(items: InboxItem[], extra: Partial<SnapshotState> = {}) {
  mockState.inbox = items;
  mockState.connectionState = extra.connectionState ?? "connected";
  mockState.error = extra.error ?? null;
}

const humanGateItem: InboxItem = {
  initiativeId: "init-1",
  title: "Deploy canary to prod",
  kind: "human-gate",
  question: "Should we enable the feature flag for all users?",
  worktree: "/Users/ericlloyd/.worktrees/init-1",
  prUrl: null,
};

const prMergeItem: InboxItem = {
  initiativeId: "init-2",
  title: "Refactor auth layer",
  kind: "pr-awaiting-merge",
  question: "PR is ready. Merge to main?",
  worktree: "/Users/ericlloyd/.worktrees/init-2",
  prUrl: "https://github.com/org/repo/pull/42",
};

beforeEach(() => {
  mockNavigate.mockReset();
  setInbox([]);
});

afterEach(() => {
  cleanup();
});

describe("InboxView — empty state", () => {
  it("shows a calm 'nothing needs you' message when inbox is empty", () => {
    renderInbox();
    expect(screen.getByText(/nothing needs you/i)).toBeTruthy();
  });

  it("does not render any inbox rows when empty", () => {
    renderInbox();
    expect(screen.queryByRole("button")).toBeNull();
  });
});

describe("InboxView — human-gate item", () => {
  beforeEach(() => setInbox([humanGateItem]));

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
  });

  it("renders a 'human gate' badge", () => {
    renderInbox();
    expect(screen.getByText(/human gate/i)).toBeTruthy();
  });

  it("renders the parked question prominently", () => {
    renderInbox();
    expect(screen.getByText("Should we enable the feature flag for all users?")).toBeTruthy();
  });

  it("does not render a PR link for human-gate items", () => {
    renderInbox();
    expect(screen.queryByText(/view pr/i)).toBeNull();
  });

  it("navigates to /initiative/:id on click", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /deploy canary to prod/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-1");
  });
});

describe("InboxView — pr-awaiting-merge item", () => {
  beforeEach(() => setInbox([prMergeItem]));

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("renders a 'pr awaiting merge' badge", () => {
    renderInbox();
    expect(screen.getByText(/pr awaiting merge/i)).toBeTruthy();
  });

  it("renders the parked question", () => {
    renderInbox();
    expect(screen.getByText("PR is ready. Merge to main?")).toBeTruthy();
  });

  it("renders a link to the PR URL that opens in a new tab", () => {
    renderInbox();
    const link = screen.getByRole("link", { name: /view pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/org/repo/pull/42");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("navigates to /initiative/:id on row click", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /refactor auth layer/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-2");
  });
});

describe("InboxView — both item kinds together", () => {
  beforeEach(() => setInbox([humanGateItem, prMergeItem]));

  it("renders both items", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("shows a count badge for two items", () => {
    renderInbox();
    expect(screen.getByText("2")).toBeTruthy();
  });
});

describe("InboxView — disconnected states", () => {
  it("shows a reconnecting banner when connectionState is reconnecting", () => {
    setInbox([], { connectionState: "reconnecting" });
    renderInbox();
    expect(screen.getByText(/reconnecting/i)).toBeTruthy();
  });

  it("shows an error banner with message when connectionState is error", () => {
    setInbox([], { connectionState: "error", error: "SSE stream closed" });
    renderInbox();
    expect(screen.getByText(/SSE stream closed/i)).toBeTruthy();
  });

  it("shows no banner when connected", () => {
    setInbox([]);
    renderInbox();
    expect(screen.queryByText(/reconnecting/i)).toBeNull();
    expect(screen.queryByText(/connection error/i)).toBeNull();
  });

  it("still renders inbox items when reconnecting", () => {
    setInbox([humanGateItem], { connectionState: "reconnecting" });
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText(/reconnecting/i)).toBeTruthy();
  });
});
