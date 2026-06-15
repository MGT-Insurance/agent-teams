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

// kind="waiting" — session blocked or explicit human gate
const waitingItem: InboxItem = {
  initiativeId: "init-1",
  title: "Deploy canary to prod",
  kind: "waiting",
  question: "Should we enable the feature flag for all users?",
  worktree: "/Users/ericlloyd/.worktrees/init-1",
  prUrl: null,
};

// kind="review" — explicit gate:review label (AUTHORITATIVE; "review the PR")
const reviewItem: InboxItem = {
  initiativeId: "init-2",
  title: "Refactor auth layer",
  kind: "review",
  question: "Review the PR: https://github.com/org/repo/pull/42",
  worktree: "/Users/ericlloyd/.worktrees/init-2",
  prUrl: "https://github.com/org/repo/pull/42",
};

// kind="generic" — delivered + no session (graceful degrade)
const genericItem: InboxItem = {
  initiativeId: "init-3",
  title: "Specialty quote API",
  kind: "generic",
  question: "Needs your attention",
  worktree: "/Users/ericlloyd/.worktrees/init-3",
  prUrl: "https://github.com/org/repo/pull/99",
};

// kind="waiting" WITH a PR — agent blocked but initiative also has an open PR.
// Delivery is orthogonal to flavor: the PR link must still appear.
const waitingItemWithPr: InboxItem = {
  initiativeId: "init-4",
  title: "Specialty Products quote API",
  kind: "waiting",
  question: "CI GREEN on PR #3551 after rename + py-SDK fix.",
  worktree: "/Users/ericlloyd/.worktrees/init-4",
  prUrl: "https://github.com/MGT-Insurance/midgard/pull/3551",
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

describe("InboxView — Waiting on you section (kind='waiting')", () => {
  beforeEach(() => setInbox([waitingItem]));

  it("renders the 'Waiting on you' section heading", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /waiting on you/i })).toBeTruthy();
  });

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
  });

  it("renders an 'agent waiting' badge on the row", () => {
    renderInbox();
    const badge = screen.getByText(/^agent waiting$/i);
    expect(badge.classList.contains("inbox-row__badge")).toBeTruthy();
  });

  it("renders the parked question prominently", () => {
    renderInbox();
    expect(screen.getByText("Should we enable the feature flag for all users?")).toBeTruthy();
  });

  it("does not render a PR link for waiting items with no prUrl", () => {
    renderInbox();
    expect(screen.queryByText(/view pr/i)).toBeNull();
  });

  it("navigates to /initiative/:id on click", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /deploy canary to prod/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-1");
  });

  it("row has data-kind='waiting' attribute", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /deploy canary to prod/i });
    expect(row.getAttribute("data-kind")).toBe("waiting");
  });
});

describe("InboxView — waiting + PR (delivery is orthogonal to flavor)", () => {
  beforeEach(() => setInbox([waitingItemWithPr]));

  it("renders the 'view PR' link for a waiting item that has a prUrl", () => {
    renderInbox();
    const link = screen.getByRole("link", { name: /view pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("renders the PR chip alongside the 'agent waiting' badge", () => {
    renderInbox();
    const { container } = renderInbox();
    // Badge label
    const badges = container.querySelectorAll(".inbox-row__badge");
    const badge = Array.from(badges).find((el) => el.textContent?.match(/agent waiting/i));
    expect(badge).toBeTruthy();
    // PR chip
    const chip = container.querySelector(".inbox-row__pr-chip");
    expect(chip).not.toBeNull();
  });

  it("still renders the 'agent waiting' badge (PR chip does not replace the flavor badge)", () => {
    renderInbox();
    expect(screen.getAllByText(/agent waiting/i).length).toBeGreaterThanOrEqual(1);
  });

  it("row has data-kind='waiting' (flavor badge takes precedence, not PR kind)", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /specialty products quote api/i });
    expect(row.getAttribute("data-kind")).toBe("waiting");
  });

  it("item is placed in the 'Waiting on you' section, not 'Review the PR' or 'Delivered — needs you'", () => {
    renderInbox();
    const waitingSection = screen.getByRole("region", { name: /waiting on you/i });
    expect(waitingSection.querySelector("[data-kind='waiting']")).not.toBeNull();
    expect(screen.queryByRole("region", { name: /review the pr/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /delivered.*needs you/i })).toBeNull();
  });
});

describe("InboxView — Review the PR section (kind='review')", () => {
  beforeEach(() => setInbox([reviewItem]));

  it("renders the 'Review the PR' section heading (authoritative gate:review)", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /review the pr/i })).toBeTruthy();
  });

  it("does NOT render 'Delivered — needs you' section for review items", () => {
    renderInbox();
    expect(screen.queryByRole("region", { name: /delivered.*needs you/i })).toBeNull();
  });

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("renders a 'review the PR' badge for review kind", () => {
    const { container } = renderInbox();
    // Use querySelector to avoid collision with the section heading "Review the PR"
    const badge = container.querySelector(".inbox-row__badge--review");
    expect(badge).not.toBeNull();
    expect(badge?.textContent?.toLowerCase()).toContain("review the pr");
  });

  it("renders the question text", () => {
    const { container } = renderInbox();
    // The question paragraph contains "Review the PR: ..."
    const question = container.querySelector(".inbox-row__question");
    expect(question?.textContent).toMatch(/Review the PR/i);
  });

  it("renders a link to the PR URL that opens in a new tab", () => {
    renderInbox();
    const link = screen.getByRole("link", { name: /view pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/org/repo/pull/42");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("renders a PR chip on a review item with a prUrl", () => {
    const { container } = renderInbox();
    const chip = container.querySelector(".inbox-row__pr-chip");
    expect(chip).not.toBeNull();
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

describe("InboxView — Delivered — needs you section (kind='generic')", () => {
  beforeEach(() => setInbox([genericItem]));

  it("renders the 'Delivered — needs you' section heading for generic items", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /delivered.*needs you/i })).toBeTruthy();
  });

  it("renders the item title for generic kind", () => {
    renderInbox();
    expect(screen.getByText("Specialty quote API")).toBeTruthy();
  });

  it("renders a 'needs you' badge for generic kind", () => {
    renderInbox();
    const badge = screen.getByText(/^needs you$/i);
    expect(badge.classList.contains("inbox-row__badge")).toBeTruthy();
  });

  it("renders a link to the PR URL for generic items (has prUrl)", () => {
    renderInbox();
    const link = screen.getByRole("link", { name: /view pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/org/repo/pull/99");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("row has data-kind='generic' attribute", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /specialty quote api/i });
    expect(row.getAttribute("data-kind")).toBe("generic");
  });
});

describe("InboxView — all three sections together", () => {
  beforeEach(() => setInbox([waitingItem, reviewItem, genericItem]));

  it("renders all items", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
    expect(screen.getByText("Specialty quote API")).toBeTruthy();
  });

  it("shows a count badge for three items", () => {
    renderInbox();
    expect(screen.getByTestId("inbox-count").textContent).toBe("3");
  });

  it("renders all three section headings", () => {
    renderInbox();
    expect(screen.getByRole("region", { name: /review the pr/i })).toBeTruthy();
    expect(screen.getByRole("region", { name: /waiting on you/i })).toBeTruthy();
    expect(screen.getByRole("region", { name: /delivered.*needs you/i })).toBeTruthy();
  });

  it("review in 'Review the PR'; waiting in 'Waiting on you'; generic in 'Delivered — needs you'", () => {
    renderInbox();
    const reviewSection = screen.getByRole("region", { name: /review the pr/i });
    const waitingSection = screen.getByRole("region", { name: /waiting on you/i });
    const deliveredSection = screen.getByRole("region", { name: /delivered.*needs you/i });
    expect(reviewSection.querySelector("[data-kind='review']")).not.toBeNull();
    expect(reviewSection.querySelector("[data-kind='waiting']")).toBeNull();
    expect(reviewSection.querySelector("[data-kind='generic']")).toBeNull();
    expect(waitingSection.querySelector("[data-kind='waiting']")).not.toBeNull();
    expect(waitingSection.querySelector("[data-kind='review']")).toBeNull();
    expect(waitingSection.querySelector("[data-kind='generic']")).toBeNull();
    expect(deliveredSection.querySelector("[data-kind='generic']")).not.toBeNull();
    expect(deliveredSection.querySelector("[data-kind='review']")).toBeNull();
    expect(deliveredSection.querySelector("[data-kind='waiting']")).toBeNull();
  });
});

describe("InboxView — section visibility", () => {
  it("shows only 'Waiting on you' section when inbox has only waiting items", () => {
    setInbox([waitingItem]);
    renderInbox();
    expect(screen.getByRole("region", { name: /waiting on you/i })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /review the pr/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /delivered.*needs you/i })).toBeNull();
  });

  it("shows only 'Review the PR' section when inbox has only review items", () => {
    setInbox([reviewItem]);
    renderInbox();
    expect(screen.getByRole("region", { name: /review the pr/i })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /waiting on you/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /delivered.*needs you/i })).toBeNull();
  });

  it("shows only 'Delivered — needs you' section for generic items", () => {
    setInbox([genericItem]);
    renderInbox();
    expect(screen.getByRole("region", { name: /delivered.*needs you/i })).toBeTruthy();
    expect(screen.queryByRole("region", { name: /review the pr/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /waiting on you/i })).toBeNull();
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
    setInbox([waitingItem], { connectionState: "reconnecting" });
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText(/reconnecting/i)).toBeTruthy();
  });
});
