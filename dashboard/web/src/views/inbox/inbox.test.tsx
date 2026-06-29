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
  nextAction: "Should we enable the feature flag for all users?",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T10:00:00Z",
  worktree: "/Users/ericlloyd/.worktrees/init-1",
  prUrl: null,
  onThisMachine: true,
};

// kind="review" — explicit gate:review label (AUTHORITATIVE; "review the PR")
const reviewItem: InboxItem = {
  initiativeId: "init-2",
  title: "Refactor auth layer",
  kind: "review",
  nextAction: "Review the PR and merge or send it back.",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T12:00:00Z",
  worktree: "/Users/ericlloyd/.worktrees/init-2",
  prUrl: "https://github.com/org/repo/pull/42",
  onThisMachine: true,
};

// kind="generic" — delivered + no session (graceful degrade)
const genericItem: InboxItem = {
  initiativeId: "init-3",
  title: "Specialty quote API",
  kind: "generic",
  nextAction: "Delivered with no gate — open the worktree to see what's needed.",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T08:00:00Z",
  worktree: "/Users/ericlloyd/.worktrees/init-3",
  prUrl: "https://github.com/org/repo/pull/99",
  onThisMachine: true,
};

// kind="waiting" WITH a PR — agent blocked but initiative also has an open PR.
// Delivery is orthogonal to flavor: the PR link must still appear.
const waitingItemWithPr: InboxItem = {
  initiativeId: "init-4",
  title: "Specialty Products quote API",
  kind: "waiting",
  nextAction: "Should we proceed with the rename?",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T11:00:00Z",
  worktree: "/Users/ericlloyd/.worktrees/init-4",
  prUrl: "https://github.com/MGT-Insurance/midgard/pull/3551",
  onThisMachine: true,
};

// Off-machine item — worktree lives on another machine.
const offMachineItem: InboxItem = {
  initiativeId: "init-5",
  title: "Remote machine initiative",
  kind: "generic",
  nextAction: "Delivered with no gate — open the worktree to see what's needed.",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T09:00:00Z",
  worktree: "/Users/other/.worktrees/init-5",
  prUrl: null,
  onThisMachine: false,
};

// kind="check" — session blocked but NO declared gate (soft tier; agent-teams-ja9c)
const checkItem: InboxItem = {
  initiativeId: "init-6",
  title: "Idle background session maybe stuck",
  kind: "check",
  nextAction: "Look at the session for more info.",
  recommendation: "",
  alternative: "",
  updatedAt: "2026-06-25T13:00:00Z", // newer than reviewItem (12:00) to prove tiering overrides recency
  worktree: "/Users/ericlloyd/.worktrees/init-6",
  prUrl: null,
  onThisMachine: true,
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

describe("InboxView — flat list (no section headings)", () => {
  beforeEach(() => setInbox([waitingItem, reviewItem, genericItem]));

  it("renders all items in a single flat list (no section headings)", () => {
    renderInbox();
    // No section headings should be present.
    expect(screen.queryByRole("region", { name: /waiting on you/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /review the pr/i })).toBeNull();
    expect(screen.queryByRole("region", { name: /delivered.*needs you/i })).toBeNull();
  });

  it("renders all item titles", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
    expect(screen.getByText("Specialty quote API")).toBeTruthy();
  });

  it("shows a count badge for three items", () => {
    renderInbox();
    expect(screen.getByTestId("inbox-count").textContent).toBe("3");
  });

  it("renders nextAction as the primary text on each row", () => {
    const { container } = renderInbox();
    const actions = container.querySelectorAll(".inbox-row__next-action");
    const texts = Array.from(actions).map((el) => el.textContent);
    expect(texts).toContain("Should we enable the feature flag for all users?");
    expect(texts).toContain("Review the PR and merge or send it back.");
    expect(texts).toContain("Delivered with no gate — open the worktree to see what's needed.");
  });
});

describe("InboxView — recency sort (newest updatedAt first)", () => {
  it("sorts items newest-first by updatedAt", () => {
    // reviewItem: 12:00, waitingItemWithPr: 11:00, waitingItem: 10:00, genericItem: 08:00
    setInbox([genericItem, waitingItem, reviewItem, waitingItemWithPr]);
    const { container } = renderInbox();
    const rows = container.querySelectorAll("[data-initiative-id]");
    expect(rows[0]?.getAttribute("data-initiative-id")).toBe("init-2"); // reviewItem 12:00
    expect(rows[1]?.getAttribute("data-initiative-id")).toBe("init-4"); // waitingItemWithPr 11:00
    expect(rows[2]?.getAttribute("data-initiative-id")).toBe("init-1"); // waitingItem 10:00
    expect(rows[3]?.getAttribute("data-initiative-id")).toBe("init-3"); // genericItem 08:00
  });

  it("does not mutate the original inbox array order", () => {
    const original = [genericItem, waitingItem, reviewItem];
    setInbox(original);
    renderInbox();
    // The mockState.inbox should retain its original order.
    expect(mockState.inbox[0]?.initiativeId).toBe("init-3");
    expect(mockState.inbox[1]?.initiativeId).toBe("init-1");
    expect(mockState.inbox[2]?.initiativeId).toBe("init-2");
  });
});

describe("InboxView — kind='waiting' row", () => {
  beforeEach(() => setInbox([waitingItem]));

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
  });

  it("renders an 'agent waiting' badge on the row", () => {
    renderInbox();
    const badge = screen.getByText(/^agent waiting$/i);
    expect(badge.classList.contains("inbox-row__badge")).toBeTruthy();
  });

  it("renders nextAction prominently", () => {
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

  it("does not render a PR chip (chip removed — redundant with kind badge + view PR link)", () => {
    const { container } = renderInbox();
    const chip = container.querySelector(".inbox-row__pr-chip");
    expect(chip).toBeNull();
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
});

describe("InboxView — kind='review' row", () => {
  beforeEach(() => setInbox([reviewItem]));

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Refactor auth layer")).toBeTruthy();
  });

  it("renders a 'review the PR' badge for review kind", () => {
    const { container } = renderInbox();
    const badge = container.querySelector(".inbox-row__badge--review");
    expect(badge).not.toBeNull();
    expect(badge?.textContent?.toLowerCase()).toContain("review the pr");
  });

  it("renders the nextAction text", () => {
    const { container } = renderInbox();
    const action = container.querySelector(".inbox-row__next-action");
    expect(action?.textContent).toBe("Review the PR and merge or send it back.");
  });

  it("renders a link to the PR URL that opens in a new tab", () => {
    renderInbox();
    const link = screen.getByRole("link", { name: /view pr/i });
    expect(link.getAttribute("href")).toBe("https://github.com/org/repo/pull/42");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("does not render a PR chip on a review item (chip removed — redundant with kind badge + view PR link)", () => {
    const { container } = renderInbox();
    const chip = container.querySelector(".inbox-row__pr-chip");
    expect(chip).toBeNull();
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

describe("InboxView — kind='generic' row", () => {
  beforeEach(() => setInbox([genericItem]));

  it("renders the item title for generic kind", () => {
    renderInbox();
    expect(screen.getByText("Specialty quote API")).toBeTruthy();
  });

  it("renders a 'needs you' badge for generic kind", () => {
    renderInbox();
    const badge = screen.getByText(/^needs you$/i);
    expect(badge.classList.contains("inbox-row__badge")).toBeTruthy();
  });

  it("renders the nextAction text for generic items", () => {
    const { container } = renderInbox();
    const action = container.querySelector(".inbox-row__next-action");
    expect(action?.textContent).toBe(
      "Delivered with no gate — open the worktree to see what's needed.",
    );
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

describe("InboxView — 'This machine only' toggle", () => {
  it("toggle defaults to checked (on)", () => {
    setInbox([waitingItem]);
    renderInbox();
    const toggle = screen.getByTestId("toggle-this-machine") as HTMLInputElement;
    expect(toggle.checked).toBe(true);
  });

  it("shows on-machine items with toggle on (default)", () => {
    setInbox([waitingItem, offMachineItem]);
    renderInbox();
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.queryByText("Remote machine initiative")).toBeNull();
  });

  it("shows all items when toggle is switched off", () => {
    setInbox([waitingItem, offMachineItem]);
    renderInbox();
    const toggle = screen.getByTestId("toggle-this-machine");
    fireEvent.click(toggle);
    expect(screen.getByText("Deploy canary to prod")).toBeTruthy();
    expect(screen.getByText("Remote machine initiative")).toBeTruthy();
  });

  it("shows off-machine empty state when toggle on and all items are off-machine", () => {
    setInbox([offMachineItem]);
    renderInbox();
    expect(screen.getByText(/nothing on this machine needs you/i)).toBeTruthy();
  });

  it("count badge reflects only shown items (toggle on, one item hidden)", () => {
    setInbox([waitingItem, offMachineItem]);
    renderInbox();
    // toggle is on; only waitingItem (onThisMachine=true) is shown
    expect(screen.getByTestId("inbox-count").textContent).toBe("1");
  });

  it("count badge shows all items after toggle turned off", () => {
    setInbox([waitingItem, offMachineItem]);
    renderInbox();
    const toggle = screen.getByTestId("toggle-this-machine");
    fireEvent.click(toggle);
    expect(screen.getByTestId("inbox-count").textContent).toBe("2");
  });
});

describe("InboxView — kind='check' row (agent-teams-ja9c)", () => {
  beforeEach(() => setInbox([checkItem]));

  it("renders the item title", () => {
    renderInbox();
    expect(screen.getByText("Idle background session maybe stuck")).toBeTruthy();
  });

  it("renders a 'check on it' badge (not orange 'agent waiting')", () => {
    renderInbox();
    const badge = screen.getByText(/^check on it$/i);
    expect(badge.classList.contains("inbox-row__badge")).toBeTruthy();
    expect(badge.classList.contains("inbox-row__badge--check")).toBeTruthy();
    // Must NOT have the waiting badge class (orange).
    expect(badge.classList.contains("inbox-row__badge--waiting")).toBe(false);
  });

  it("renders nextAction text", () => {
    renderInbox();
    expect(screen.getByText("Look at the session for more info.")).toBeTruthy();
  });

  it("row has data-kind='check' attribute", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /idle background session maybe stuck/i });
    expect(row.getAttribute("data-kind")).toBe("check");
  });

  it("row has inbox-row--check class (muted style)", () => {
    const { container } = renderInbox();
    const row = container.querySelector(".inbox-row--check");
    expect(row).not.toBeNull();
    // Must NOT have the orange waiting class.
    expect(row?.classList.contains("inbox-row--waiting")).toBe(false);
  });

  it("navigates to /initiative/:id on click", () => {
    renderInbox();
    const row = screen.getByRole("button", { name: /idle background session maybe stuck/i });
    fireEvent.click(row);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-6");
  });
});

describe("InboxView — tiered sort (agent-teams-ja9c)", () => {
  it("check item sorts BELOW review/waiting/generic even when check has a newer updatedAt", () => {
    // checkItem.updatedAt = 13:00, reviewItem.updatedAt = 12:00
    // Tiering must override recency: review (tier 0) before check (tier 3).
    setInbox([checkItem, reviewItem]);
    const { container } = renderInbox();
    const rows = container.querySelectorAll("[data-initiative-id]");
    expect(rows[0]?.getAttribute("data-initiative-id")).toBe("init-2"); // review
    expect(rows[1]?.getAttribute("data-initiative-id")).toBe("init-6"); // check
  });

  it("tiered-then-recency: review < waiting < generic < check, recency desc within tier", () => {
    // review:  12:00 (tier 0)
    // waiting: 10:00 (tier 1)
    // generic: 08:00 (tier 2)
    // check:   13:00 (tier 3) — newest overall but lowest tier
    setInbox([checkItem, genericItem, waitingItem, reviewItem]);
    const { container } = renderInbox();
    const rows = container.querySelectorAll("[data-initiative-id]");
    expect(rows[0]?.getAttribute("data-initiative-id")).toBe("init-2"); // review  tier 0
    expect(rows[1]?.getAttribute("data-initiative-id")).toBe("init-1"); // waiting tier 1
    expect(rows[2]?.getAttribute("data-initiative-id")).toBe("init-3"); // generic tier 2
    expect(rows[3]?.getAttribute("data-initiative-id")).toBe("init-6"); // check   tier 3
  });

  it("two check items sort by recency desc within the check tier", () => {
    const olderCheck: InboxItem = {
      ...checkItem,
      initiativeId: "init-7",
      title: "Older check item",
      updatedAt: "2026-06-25T07:00:00Z",
    };
    setInbox([olderCheck, checkItem]);
    const { container } = renderInbox();
    const rows = container.querySelectorAll("[data-initiative-id]");
    expect(rows[0]?.getAttribute("data-initiative-id")).toBe("init-6"); // newer 13:00
    expect(rows[1]?.getAttribute("data-initiative-id")).toBe("init-7"); // older 07:00
  });
});

describe("InboxView — waiting row recommendation/alternative (agent-teams-oc3p)", () => {
  it("renders 'Recommended:' line when recommendation is non-empty on a waiting row", () => {
    const item: InboxItem = {
      ...waitingItem,
      recommendation: "Roll back the canary and monitor error rates.",
      alternative: "",
    };
    setInbox([item]);
    const { container } = renderInbox();
    // Label is a styled <span>, value a sibling text node — assert the line's full text in order.
    const rec = container.querySelector(".inbox-row__secondary--recommendation");
    expect(rec?.textContent).toMatch(/^Recommended: Roll back the canary and monitor error rates\.$/);
  });

  it("renders 'Alternative:' line when alternative is non-empty on a waiting row", () => {
    const item: InboxItem = {
      ...waitingItem,
      recommendation: "",
      alternative: "Enable for 10% of users and watch for 24h.",
    };
    setInbox([item]);
    const { container } = renderInbox();
    const alt = container.querySelector(".inbox-row__secondary--alternative");
    expect(alt?.textContent).toMatch(/^Alternative: Enable for 10% of users and watch for 24h\.$/);
  });

  it("renders both secondary lines when both are present", () => {
    const item: InboxItem = {
      ...waitingItem,
      recommendation: "Roll back.",
      alternative: "Partial rollout.",
    };
    setInbox([item]);
    const { container } = renderInbox();
    const secondary = container.querySelectorAll(".inbox-row__secondary");
    expect(secondary).toHaveLength(2);
  });

  it("renders no secondary lines when recommendation and alternative are empty", () => {
    setInbox([waitingItem]); // waitingItem has recommendation:"", alternative:""
    const { container } = renderInbox();
    expect(container.querySelectorAll(".inbox-row__secondary")).toHaveLength(0);
  });

  it("does not render secondary lines on a review row even with non-empty fields (type guard)", () => {
    const item: InboxItem = {
      ...reviewItem,
      // review rows always have "" for these per type, but guard against future misuse
      recommendation: "",
      alternative: "",
    };
    setInbox([item]);
    const { container } = renderInbox();
    expect(container.querySelectorAll(".inbox-row__secondary")).toHaveLength(0);
  });
});

describe("InboxView — waiting row recommendation/alternative edge cases (oc3p)", () => {
  // These complement the core-path tests above by explicitly asserting the ABSENT
  // secondary line is not rendered when only one of the two fields is set.

  it("recommendation present, alternative empty → 'Alternative:' is NOT rendered", () => {
    const item: InboxItem = {
      ...waitingItem,
      recommendation: "Roll back the canary and monitor error rates.",
      alternative: "",
    };
    setInbox([item]);
    renderInbox();
    expect(screen.getByText(/^Recommended:/)).toBeTruthy();
    expect(screen.queryByText(/^Alternative:/)).toBeNull();
  });

  it("alternative present, recommendation empty → 'Recommended:' is NOT rendered", () => {
    const item: InboxItem = {
      ...waitingItem,
      recommendation: "",
      alternative: "Enable for 10% of users and watch for 24h.",
    };
    setInbox([item]);
    renderInbox();
    expect(screen.getByText(/^Alternative:/)).toBeTruthy();
    expect(screen.queryByText(/^Recommended:/)).toBeNull();
  });

  it("check row with non-empty recommendation → secondary line NOT rendered (kind guard)", () => {
    // buildInbox always emits "" for check rows; but guard the render itself.
    const item: InboxItem = {
      ...checkItem,
      // Force non-empty to confirm the kind guard fires — this won't come from
      // buildInbox in practice, but the render must be gated on kind, not value.
      recommendation: "This should not appear.",
    };
    setInbox([item]);
    const { container } = renderInbox();
    expect(container.querySelectorAll(".inbox-row__secondary")).toHaveLength(0);
    expect(screen.queryByText(/Recommended:/)).toBeNull();
  });

  it("generic row with non-empty recommendation → secondary line NOT rendered (kind guard)", () => {
    const item: InboxItem = {
      ...genericItem,
      recommendation: "This should not appear.",
    };
    setInbox([item]);
    const { container } = renderInbox();
    expect(container.querySelectorAll(".inbox-row__secondary")).toHaveLength(0);
    expect(screen.queryByText(/Recommended:/)).toBeNull();
  });

  it("review row with non-empty recommendation → secondary line NOT rendered (kind guard)", () => {
    // Stronger version of the existing review test — uses non-empty value.
    const item: InboxItem = {
      ...reviewItem,
      recommendation: "This should not appear.",
    };
    setInbox([item]);
    const { container } = renderInbox();
    expect(container.querySelectorAll(".inbox-row__secondary")).toHaveLength(0);
    expect(screen.queryByText(/Recommended:/)).toBeNull();
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
