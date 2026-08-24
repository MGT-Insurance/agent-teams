import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { AppRoutes } from "../../router.js";
import InitiativeLabView from "./index.js";
import { initiatives, workScenarios } from "./scenarios.js";

vi.mock("../../SnapshotContext.js", () => ({
  useSnapshotContext: () => ({
    initiatives: [],
    unmatchedSessions: [],
    inbox: [],
    ts: null,
    connectionState: "connected",
    error: null,
  }),
}));

vi.mock("../drillin/index.js", () => ({ default: () => <div>Drill-in test stub</div> }));

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}{location.search}</output>;
}

function renderLab(entry = "/initiatives/lab") {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <InitiativeLabView />
      <LocationProbe />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("initiative lab fixture", () => {
  it("keeps the frozen three-initiative, four-PR-group identities", () => {
    expect(initiatives.map((initiative) => initiative.title)).toEqual([
      "Dashboard initiative refresh",
      "Reliable session recovery",
      "Plugin release parity",
    ]);
    expect(workScenarios.map((work) => work.title)).toEqual([
      "Prototype interaction lab",
      "Responsive hardening",
      "Preserve session identity",
      "Publish matching runtimes",
    ]);
    expect(workScenarios.flatMap((work) => work.pullRequests.map((pullRequest) => pullRequest.number))).toEqual([
      180, 181, 182, 176, 179, 174,
    ]);
  });
});

describe("initiative lab routing and concept switcher", () => {
  it.each([
    ["pipeline", "Outcome Pipeline"],
    ["cockpit", "Initiative Cockpit"],
    ["queue", "Action Queue"],
  ])("renders the %s deep link directly", (concept, heading) => {
    renderLab(`/initiatives/lab?concept=${concept}`);
    expect(screen.getByRole("heading", { name: heading, level: 2 })).toBeTruthy();
    expect(screen.getByText("Sample data")).toBeTruthy();
    expect(screen.getByRole("tab", { name: new RegExp(heading) }).getAttribute("aria-selected")).toBe("true");
  });

  it("falls back to pipeline for invalid concepts and updates the URL by mouse and keyboard", async () => {
    renderLab("/initiatives/lab?concept=unknown&note=keep");
    expect(screen.getByRole("heading", { name: "Outcome Pipeline" })).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByTestId("location").textContent).toBe("/initiatives/lab?concept=pipeline&note=keep");
    });

    fireEvent.click(screen.getByRole("tab", { name: /Initiative Cockpit/ }));
    expect(screen.getByTestId("location").textContent).toBe("/initiatives/lab?concept=cockpit&note=keep");

    fireEvent.keyDown(screen.getByRole("tab", { name: /Initiative Cockpit/ }), { key: "ArrowRight" });
    expect(screen.getByTestId("location").textContent).toBe("/initiatives/lab?concept=queue&note=keep");
    expect(screen.getByRole("heading", { name: "Action Queue" })).toBeTruthy();
  });

  it("registers the lab without replacing the existing initiatives route", () => {
    const view = render(
      <MemoryRouter initialEntries={["/initiatives"]}>
        <AppRoutes />
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "Initiatives", level: 1 })).toBeTruthy();
    view.unmount();

    render(
      <MemoryRouter initialEntries={["/initiatives/lab?concept=queue"]}>
        <AppRoutes />
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "Action Queue" })).toBeTruthy();
  });
});

describe("initiative lab concept interactions", () => {
  it("selects a pipeline PR group and exposes its non-mutating detail", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    fireEvent.click(screen.getByRole("button", { name: /Responsive hardening/ }));
    const detail = screen.getByRole("complementary", { name: "Selected PR group detail" });
    expect(within(detail).getByRole("heading", { name: "Responsive hardening" })).toBeTruthy();
    expect(within(detail).getByText("Dashboard initiative refresh")).toBeTruthy();
    expect(within(detail).getByText(/Resolve the mobile navigation decision/)).toBeTruthy();
    expect(screen.getAllByText("Owner").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Agent state").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Review").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Checks").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Blocker").length).toBeGreaterThan(0);
  });

  it("changes cockpit initiative context and its Now, Next, Risks, and timeline", () => {
    renderLab("/initiatives/lab?concept=cockpit");
    fireEvent.click(screen.getByRole("button", { name: /Reliable session recovery/ }));
    expect(screen.getByRole("heading", { name: "Reliable session recovery", level: 3 })).toBeTruthy();
    expect(screen.getByText("Now")).toBeTruthy();
    expect(screen.getByText("Next")).toBeTruthy();
    expect(screen.getByText("Risks")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "PR-group timeline" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Preserve session identity" })).toBeTruthy();
    expect(screen.getByText("Owner")).toBeTruthy();
    expect(screen.getByText("Agent state")).toBeTruthy();
    expect(screen.getByText("Review")).toBeTruthy();
    expect(screen.getByText("Checks")).toBeTruthy();
  });

  it("expands an action queue item to show review, checks, blocker, progress, and PR identity", () => {
    renderLab("/initiatives/lab?concept=queue");
    const itemSummary = screen.getByText("Responsive hardening").closest("summary");
    expect(itemSummary).toBeTruthy();
    fireEvent.click(itemSummary!);
    const item = itemSummary!.closest("details");
    expect(item?.open).toBe(true);
    expect(within(item!).getByText("Review not requested")).toBeTruthy();
    expect(within(item!).getByText("Checks wait on the design decision")).toBeTruthy();
    expect(within(item!).getByText("The mobile navigation direction is unresolved")).toBeTruthy();
    const progress = within(item!).getByRole("progressbar", { name: "Outcome progress" });
    expect(progress.getAttribute("aria-valuenow")).toBe("1");
    expect(progress.getAttribute("aria-valuemax")).toBe("4");
    expect(within(item!).getByRole("link", { name: /PR #181/ })).toBeTruthy();
    expect(within(item!).getByText(/Unblocker:/)).toBeTruthy();
  });

  it("keeps sample Bead ids inside collapsed implementation metadata", () => {
    renderLab("/initiatives/lab?concept=queue");
    const itemSummary = screen.getByText("Responsive hardening").closest("summary");
    fireEvent.click(itemSummary!);

    const metadataSummary = screen.getAllByText("Implementation metadata")[0];
    const metadata = metadataSummary?.closest("details");
    expect(metadata).toBeTruthy();
    expect(metadata?.open).toBe(false);
    const id = within(metadata!).getByText(/agent-teams-tuy9\.21/);
    expect(id.closest("details")?.open).toBe(false);

    fireEvent.click(metadataSummary!);
    expect(metadata?.open).toBe(true);
    expect(within(metadata!).getByText("Sample Bead ids")).toBeTruthy();
  });
});
