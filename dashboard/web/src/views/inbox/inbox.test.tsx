import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { SnapshotState } from "../../hooks/useSnapshot.js";
import type { InboxItem } from "@agent-teams/shared";

// Snapshot context is mocked so we control the inbox data directly.
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

// kind="answer" — initiative parked on a gate/question
const answerItem: InboxItem = {
  initiativeId: "init-1",
  title: "Deploy canary to prod",
  kind: "answer",
  question: "Should we enable the feature flag for all users?",
  worktree: "/Users/ericlloyd/.worktrees/init-1",
  prUrl: null,
};

// kind="review" — PR open and idle, awaiting Eric's review/merge
const reviewItem: InboxItem = {
  initiativeId: "init-2",
  title: "Refactor auth layer",
  kind: "review",
  question: "PR awaiting review: https://github.com/org/repo/pull/42",
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

  it("does not show a count badge when inbox is empty", () => {
    renderInbox();
    expect(screen.queryByTestId("inbox-count")).toBeNull();
  });
});

describe("InboxView — Answer section (kind='answer')", () => {
  beforeEach(() => setInbox([answerItem]));

  it("renders the 'Answer' section heading", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /answer/i })).toBeTruthy();
  });

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
  });

  it("renders an 'answer' badge on the row", () => {
    renderInbox();
    // The badge is a span inside the row; getAllByText handles both section heading + badge.
    const matches = screen.getAllByText(/^answer$/i);
    // At least one match should be the badge (has the badge class).
    const badge = matches.find(
      (el) => el.classList.contains("inbox-row__badge"),
    );
    expect(badge).toBeTruthy();
  });

  it("renders the parked question prominently", () => {
    renderInbox();
    expect(screen.getByText("Should we enable the feature flag for all users?")).toBeTruthy();
  });

  it("does not render a PR link for answer items", () => {
    renderInbox();
    expect(screen.queryByText(/view pr/i)).toBeNull();
  });

  it("navigates to /initiative/:id on click", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /deploy canary to prod/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-1");
  });

  it("row has data-kind='answer' attribute", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /deploy canary to prod/i });
    expect(row.getAttribute("data-kind")).toBe("answer");
  });
});

describe("InboxView — Review / merge section (kind='review')", () => {
  beforeEach(() => setInbox([reviewItem]));

  it("renders the 'Review / merge' section heading", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /review \/ merge/i })).toBeTruthy();
  });

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("renders a 'review' badge", () => {
    renderInbox();
    expect(screen.getByText(/^review$/i)).toBeTruthy();
  });

  it("renders the question text", () => {
    renderInbox();
    expect(screen.getByText(/PR awaiting review/i)).toBeTruthy();
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

  it("row has data-kind='review' attribute", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /refactor auth layer/i });
    expect(row.getAttribute("data-kind")).toBe("review");
  });
});

describe("InboxView — both sections together", () => {
  beforeEach(() => setInbox([answerItem, reviewItem]));

  it("renders both items", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("shows a count badge for two items", () => {
    renderInbox();
    expect(screen.getByTestId("inbox-count").textContent).toBe("2");
  });

  it("renders both section headings", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /answer/i })).toBeTruthy();
    expect(screen.getByRole("region", { name: /review \/ merge/i })).toBeTruthy();
  });

  it("answer item is in Answer section, review item is in Review/merge section", () => {
    renderInbox();
    const answerSection = screen.getByRole("region", { name: /^answer$/i });
    const reviewSection = screen.getByRole("region", { name: /review \/ merge/i });
    expect(answerSection.querySelector("[data-kind='answer']")).not.toBeNull();
    expect(answerSection.querySelector("[data-kind='review']")).toBeNull();
    expect(reviewSection.querySelector("[data-kind='review']")).not.toBeNull();
    expect(reviewSection.querySelector("[data-kind='answer']")).toBeNull();
  });
});

describe("InboxView — section visibility", () => {
  it("shows only Answer section when inbox has only answer items", () => {
    setInbox([answerItem]);
    renderInbox();
    expect(screen.getByRole("region", { name: /^answer$/i })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /review \/ merge/i })).toBeNull();
  });

  it("shows only Review/merge section when inbox has only review items", () => {
    setInbox([reviewItem]);
    renderInbox();
    expect(screen.getByRole("region", { name: /review \/ merge/i })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /^answer$/i })).toBeNull();
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
    setInbox([answerItem], { connectionState: "reconnecting" });
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText(/reconnecting/i)).toBeTruthy();
  });
});
