import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import type { MemoryEntry } from "@agent-teams/shared";

// api is mocked so tests control fetchMemories/fetchLearnings directly.
const mockFetchMemories = vi.hoisted(() => vi.fn());
const mockFetchLearnings = vi.hoisted(() => vi.fn());
vi.mock("../../lib/api.js", () => ({
  fetchMemories: mockFetchMemories,
  fetchLearnings: mockFetchLearnings,
}));

import MemoriesView, { filterMemories, sortMemories, type MemoryFilters } from "./index.js";

function renderMemories() {
  return render(<MemoriesView />);
}

const plannerHot: MemoryEntry = {
  role: "planner",
  key: "planner:hot:decompose-work",
  slug: "decompose-work",
  tier: "hot",
  body: "Decompose work into file-disjoint tracks.",
  appliedCount: 5,
  lastApplied: "2026-07-10T10:00:00Z",
};

const implementerCold: MemoryEntry = {
  role: "implementer",
  key: "implementer:cold:ask-on-ambiguity",
  slug: "ask-on-ambiguity",
  tier: "cold",
  body: "Stop and ask on design ambiguity.",
  appliedCount: 0,
  lastApplied: null,
};

const plannerFresh: MemoryEntry = {
  role: "planner",
  key: "planner:fresh:surface-questions",
  slug: "surface-questions",
  tier: "fresh",
  body: "Surface clarifying questions early.",
  appliedCount: 2,
  lastApplied: "2026-07-08T08:00:00Z",
};

beforeEach(() => {
  mockFetchMemories.mockReset();
  mockFetchMemories.mockResolvedValue({ memories: [plannerHot, implementerCold, plannerFresh] });
  mockFetchLearnings.mockReset();
  mockFetchLearnings.mockResolvedValue({ role: "implementer", text: "" });
});

afterEach(() => {
  cleanup();
});

describe("MemoriesView — role selection", () => {
  it("auto-selects the first role alphabetically and scopes the list to it", async () => {
    renderMemories();

    // "implementer" sorts before "planner" — should be selected by default.
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());
    expect(screen.queryByTestId(`memories-card-${plannerHot.key}`)).toBeNull();
    expect(screen.queryByTestId(`memories-card-${plannerFresh.key}`)).toBeNull();
  });

  it("shows each role's memory count in the sidebar", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId("memories-role-planner")).toBeTruthy());

    expect(screen.getByTestId("memories-role-planner").textContent).toContain("2");
    expect(screen.getByTestId("memories-role-implementer").textContent).toContain("1");
  });

  it("switches the main panel to only the selected role's memories, never interleaved", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());

    fireEvent.click(screen.getByTestId("memories-role-planner"));

    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());
    expect(screen.getByTestId(`memories-card-${plannerFresh.key}`)).toBeTruthy();
    expect(screen.queryByTestId(`memories-card-${implementerCold.key}`)).toBeNull();
  });

  it("sorts the selected role's memories by appliedCount descending", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());

    fireEvent.click(screen.getByTestId("memories-role-planner"));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());

    const cards = screen.getAllByTestId(/^memories-card-/);
    expect(cards[0]!.getAttribute("data-testid")).toBe(`memories-card-${plannerHot.key}`); // appliedCount 5
    expect(cards[1]!.getAttribute("data-testid")).toBe(`memories-card-${plannerFresh.key}`); // appliedCount 2
  });

  it("shows the full memory body directly, with no expand step", async () => {
    renderMemories();
    fireEvent.click(await screen.findByTestId("memories-role-planner"));

    expect(await screen.findByText("Decompose work into file-disjoint tracks.")).toBeTruthy();
  });

  it("shows an explicit never-applied state and a formatted last-applied date", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());

    expect(screen.getByTestId(`memories-card-${implementerCold.key}`).textContent).toContain("Never applied");

    fireEvent.click(screen.getByTestId("memories-role-planner"));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());
    expect(screen.getByTestId(`memories-card-${plannerHot.key}`).textContent).toContain("Last applied");
  });

  it("shows the header count of roles and total memories", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId("memories-count").textContent).toContain("2 roles"));
    expect(screen.getByTestId("memories-count").textContent).toContain("3 memories");
  });
});

describe("MemoriesView — filters scoped to the selected role", () => {
  it("narrows the selected role's list by tier without leaking into other roles", async () => {
    renderMemories();
    fireEvent.click(await screen.findByTestId("memories-role-planner"));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/filter by tier/i), { target: { value: "fresh" } });

    await waitFor(() => expect(screen.queryByTestId(`memories-card-${plannerHot.key}`)).toBeNull());
    expect(screen.getByTestId(`memories-card-${plannerFresh.key}`)).toBeTruthy();
  });

  it("narrows the selected role's list with free-text search over slug/body", async () => {
    renderMemories();
    fireEvent.click(await screen.findByTestId("memories-role-planner"));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/search memories/i), { target: { value: "clarifying" } });

    await waitFor(() => expect(screen.queryByTestId(`memories-card-${plannerHot.key}`)).toBeNull());
    expect(screen.getByTestId(`memories-card-${plannerFresh.key}`)).toBeTruthy();
  });

  it("shows the no-match empty state and clears filters via the Clear filters button", async () => {
    renderMemories();
    fireEvent.click(await screen.findByTestId("memories-role-planner"));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/search memories/i), { target: { value: "nonexistent" } });
    await screen.findByText(/no memories match the current filters/i);

    fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());
  });

  it("resets active filters when switching roles", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/search memories/i), { target: { value: "ambiguity" } });
    await waitFor(() => expect(screen.getByTestId(`memories-card-${implementerCold.key}`)).toBeTruthy());

    fireEvent.click(screen.getByTestId("memories-role-planner"));

    await waitFor(() => expect(screen.getByTestId(`memories-card-${plannerHot.key}`)).toBeTruthy());
    expect(screen.getByTestId(`memories-card-${plannerFresh.key}`)).toBeTruthy();
    expect((screen.getByLabelText(/search memories/i) as HTMLInputElement).value).toBe("");
  });
});

describe("filterMemories (unit)", () => {
  const base: MemoryFilters = { tier: "all", text: "" };

  it("tier filter keeps only entries matching the tier", () => {
    const result = filterMemories([plannerHot, plannerFresh], { ...base, tier: "fresh" });
    expect(result).toEqual([plannerFresh]);
  });

  it("text filter matches case-insensitively across slug/body", () => {
    const result = filterMemories([plannerHot, implementerCold], { ...base, text: "AMBIGUITY" });
    expect(result).toEqual([implementerCold]);
  });

  it("returns all entries when no filters are active", () => {
    const result = filterMemories([plannerHot, implementerCold], base);
    expect(result).toEqual([plannerHot, implementerCold]);
  });
});

describe("sortMemories (unit)", () => {
  it("sorts by appliedCount descending, tiebreak by key ascending", () => {
    const tieA: MemoryEntry = { ...plannerHot, key: "b:hot:x", appliedCount: 3 };
    const tieB: MemoryEntry = { ...plannerHot, key: "a:hot:x", appliedCount: 3 };
    const result = sortMemories([implementerCold, tieA, tieB, plannerFresh]);
    expect(result.map((m) => m.key)).toEqual(["a:hot:x", "b:hot:x", plannerFresh.key, implementerCold.key]);
  });
});

// agent-teams-orb7.1: "View injected context" button + panel.
describe("MemoriesView — injected context panel", () => {
  it("renders the 'View injected context' button for the selected role", async () => {
    renderMemories();
    const btn = await screen.findByTestId("memories-injected-toggle");
    expect(btn.textContent).toContain("View injected context");
  });

  it("fetches and shows the panel content on click, scoped to the selected role", async () => {
    mockFetchLearnings.mockResolvedValue({
      role: "implementer",
      text: "implementer:hot:verify-live\nAlways verify live before declaring done.",
    });

    renderMemories();
    await screen.findByTestId(`memories-card-${implementerCold.key}`);

    fireEvent.click(screen.getByTestId("memories-injected-toggle"));

    expect(await screen.findByTestId("memories-injected-panel")).toBeTruthy();
    await waitFor(() => expect(mockFetchLearnings).toHaveBeenCalledWith("implementer"));
    await waitFor(() =>
      expect(screen.getByTestId("memories-injected-panel").textContent).toContain(
        "implementer:hot:verify-live",
      ),
    );
    expect(screen.getByTestId("memories-injected-panel").textContent).toContain(
      "Always verify live before declaring done.",
    );
  });

  it("shows the empty-injection message when the role has nothing injected", async () => {
    mockFetchLearnings.mockResolvedValue({ role: "implementer", text: "" });

    renderMemories();
    await screen.findByTestId(`memories-card-${implementerCold.key}`);
    fireEvent.click(screen.getByTestId("memories-injected-toggle"));

    expect(
      await screen.findByText(/nothing injected — no hot\/fresh memories for this role/i),
    ).toBeTruthy();
  });

  it("shows an error banner when fetchLearnings fails", async () => {
    mockFetchLearnings.mockRejectedValue(new Error("ateam learnings exited with code 1"));

    renderMemories();
    await screen.findByTestId(`memories-card-${implementerCold.key}`);
    fireEvent.click(screen.getByTestId("memories-injected-toggle"));

    expect(await screen.findByText(/failed to load injected context/i)).toBeTruthy();
  });

  it("closes the panel via the Close button", async () => {
    mockFetchLearnings.mockResolvedValue({ role: "implementer", text: "some text" });

    renderMemories();
    await screen.findByTestId(`memories-card-${implementerCold.key}`);
    fireEvent.click(screen.getByTestId("memories-injected-toggle"));
    await screen.findByTestId("memories-injected-panel");

    fireEvent.click(screen.getByRole("button", { name: /close injected context panel/i }));

    await waitFor(() => expect(screen.queryByTestId("memories-injected-panel")).toBeNull());
  });

  it("exercises the user-role case — fetches with role='user' and shows prime's output", async () => {
    const userFresh: MemoryEntry = {
      role: "user",
      key: "user:fresh:prefers-concise-reports",
      slug: "prefers-concise-reports",
      tier: "fresh",
      body: "Keep summaries under 200 words.",
      appliedCount: 1,
      lastApplied: "2026-07-09T00:00:00Z",
    };
    mockFetchMemories.mockResolvedValue({ memories: [plannerHot, implementerCold, plannerFresh, userFresh] });
    mockFetchLearnings.mockResolvedValue({
      role: "user",
      text: "user:fresh:prefers-concise-reports\nKeep summaries under 200 words.",
    });

    renderMemories();
    fireEvent.click(await screen.findByTestId("memories-role-user"));
    await screen.findByTestId(`memories-card-${userFresh.key}`);

    fireEvent.click(screen.getByTestId("memories-injected-toggle"));

    await waitFor(() => expect(mockFetchLearnings).toHaveBeenCalledWith("user"));
    // Server-side special-casing of "user" -> `ateam prime` (not `ateam
    // learnings user`) is covered by cli.test.ts's ateamLearnings suite —
    // this test only proves the web panel plumbs the role param through and
    // renders whatever LearningsResponse.text the endpoint returns.
    await waitFor(() =>
      expect(screen.getByTestId("memories-injected-panel").textContent).toContain(
        "user:fresh:prefers-concise-reports",
      ),
    );
    expect(screen.getByTestId("memories-injected-panel").textContent).toContain(
      "Keep summaries under 200 words.",
    );
  });
});
