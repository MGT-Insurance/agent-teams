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

describe("MemoriesView — list rendering", () => {
  it("renders rows sorted by appliedCount descending by default", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    const rows = container.querySelectorAll(".memories-row");
    const firstCells = rows[0]!.querySelectorAll("td");
    expect(firstCells[0]!.textContent).toBe("planner"); // highest appliedCount (5)
    expect(firstCells[3]!.textContent).toBe("5");

    const lastCells = rows[2]!.querySelectorAll("td");
    expect(lastCells[3]!.textContent).toBe("0"); // lowest appliedCount
  });

  it("expands a row to reveal the full memory body on click", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    const rows = container.querySelectorAll(".memories-row");
    fireEvent.click(rows[0]!);
    expect(screen.getByText("Decompose work into file-disjoint tracks.")).toBeTruthy();
  });

  it("shows the header count of total memories", async () => {
    renderMemories();
    await waitFor(() => expect(screen.getByTestId("memories-count").textContent).toBe("3"));
  });
});

describe("MemoriesView — filters", () => {
  it("narrows the list by role", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    fireEvent.change(screen.getByLabelText(/filter by role/i), { target: { value: "implementer" } });

    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(1));
    const cells = container.querySelector(".memories-row")!.querySelectorAll("td");
    expect(cells[0]!.textContent).toBe("implementer");
  });

  it("narrows the list by tier", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    fireEvent.change(screen.getByLabelText(/filter by tier/i), { target: { value: "cold" } });

    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(1));
    const cells = container.querySelector(".memories-row")!.querySelectorAll("td");
    expect(cells[0]!.textContent).toBe("implementer");
  });

  it("narrows the list with the free-text search over role/slug/body", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    fireEvent.change(screen.getByLabelText(/search memories/i), { target: { value: "clarifying" } });

    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(1));
    const cells = container.querySelector(".memories-row")!.querySelectorAll("td");
    expect(cells[1]!.textContent).toBe("surface-questions");
  });

  it("shows the no-match empty state and clears filters via the Clear filters button", async () => {
    const { container } = renderMemories();
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));

    fireEvent.change(screen.getByLabelText(/search memories/i), { target: { value: "nonexistent" } });
    await screen.findByText(/no memories match the current filters/i);

    fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
    await waitFor(() => expect(container.querySelectorAll(".memories-row")).toHaveLength(3));
  });
});

describe("filterMemories (unit)", () => {
  const base: MemoryFilters = { role: "", tier: "all", text: "" };

  it("role filter keeps only entries for that role", () => {
    const result = filterMemories([plannerHot, implementerCold], { ...base, role: "planner" });
    expect(result).toEqual([plannerHot]);
  });

  it("tier filter keeps only entries matching the tier", () => {
    const result = filterMemories([plannerHot, implementerCold, plannerFresh], { ...base, tier: "fresh" });
    expect(result).toEqual([plannerFresh]);
  });

  it("text filter matches case-insensitively across role/slug/body", () => {
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
