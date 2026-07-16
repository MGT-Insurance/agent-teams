import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import type { MemoryEntry } from "@agent-teams/shared";

// api is mocked so tests control fetchMemories directly.
const mockFetchMemories = vi.hoisted(() => vi.fn());
vi.mock("../../lib/api.js", () => ({
  fetchMemories: mockFetchMemories,
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
