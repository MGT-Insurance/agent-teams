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
  it("shares three initiatives across realistic PR and active-effort scenarios", () => {
    expect(initiatives.map((initiative) => initiative.title)).toEqual([
      "Dashboard initiative refresh",
      "Reliable session recovery",
      "Plugin release parity",
    ]);
    expect(workScenarios.map((work) => work.title)).toEqual([
      "Prototype interaction lab",
      "Responsive hardening",
      "Investigate reconnect regression",
      "Preserve session identity",
      "Publish matching runtimes",
    ]);
    expect(workScenarios.flatMap((work) => work.pullRequests.map((pullRequest) => pullRequest.number))).toEqual([
      180, 176, 179, 174,
    ]);
    expect(workScenarios.filter((work) => work.pullRequests.length === 0).map((work) => work.pipelineStage)).toEqual([
      "building",
      "investigating",
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
  it("keeps pipeline operational detail hidden until a sparse card is activated", () => {
    renderLab("/initiatives/lab?concept=pipeline");

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText("Concept comparison ready")).toBeNull();
    expect(screen.queryByText("Sample Bead ids")).toBeNull();
    expect(screen.queryByText("Implementation metadata")).toBeNull();

    const card = screen.getByRole("button", { name: /PR #180.*Prototype interaction lab/i });
    expect(within(card).getByText("Rowan", { exact: false })).toBeTruthy();
    expect(within(card).getByText("Choose a direction after comparing all three concepts")).toBeTruthy();
    const activeBuild = screen.getByRole("button", { name: /Active effort.*Responsive hardening/i });
    expect(within(activeBuild).queryByText("Finish the responsive layout and open the first PR")).toBeNull();
    fireEvent.click(card);

    const detail = screen.getByRole("dialog", { name: "Prototype interaction lab" });
    expect(within(detail).getByRole("heading", { name: "Prototype interaction lab" })).toBeTruthy();
    expect(within(detail).getByText("Dashboard initiative refresh")).toBeTruthy();
    expect(within(detail).getByText("Concept comparison ready")).toBeTruthy();
    expect(within(detail).getByText("Owner")).toBeTruthy();
    expect(within(detail).getByText("Review")).toBeTruthy();
    expect(within(detail).getByText("Checks")).toBeTruthy();
    expect(within(detail).getByRole("heading", { name: "Recent activity" })).toBeTruthy();
    expect(within(detail).getByRole("progressbar", { name: "Outcome progress" })).toBeTruthy();
    expect(within(detail).getByText("Implementation metadata")).toBeTruthy();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(card);
  });

  it("filters Needs you orthogonally without changing lifecycle columns or card stage", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    const stageNames = ["Investigating", "Building", "In Review", "Ready to Land", "Done"];
    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual(stageNames);

    const inReview = screen.getByRole("heading", { name: "In Review" }).closest("section");
    const readyToLand = screen.getByRole("heading", { name: "Ready to Land" }).closest("section");
    expect(inReview).toBeTruthy();
    expect(readyToLand).toBeTruthy();
    expect(within(inReview!).getByRole("button", { name: /PR #180.*Prototype interaction lab/i })).toBeTruthy();
    expect(within(readyToLand!).getByRole("button", { name: /PR #176/ })).toBeTruthy();
    expect(within(readyToLand!).getByRole("button", { name: /PR #179/ })).toBeTruthy();

    fireEvent.click(screen.getByRole("checkbox", { name: "Needs you only" }));

    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual(stageNames);
    expect(within(inReview!).getByRole("button", { name: /PR #180.*Prototype interaction lab/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Responsive hardening/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Investigate reconnect regression/ })).toBeNull();
  });

  it("changes cockpit initiative context and its Now, Next, Risks, and timeline", () => {
    renderLab("/initiatives/lab?concept=cockpit");
    fireEvent.click(screen.getByRole("button", { name: /Reliable session recovery/ }));
    expect(screen.getByRole("heading", { name: "Reliable session recovery", level: 3 })).toBeTruthy();
    expect(screen.getByText("Now")).toBeTruthy();
    expect(screen.getByText("Next")).toBeTruthy();
    expect(screen.getByText("Risks")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Delivery timeline" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Preserve session identity" })).toBeTruthy();
    expect(screen.getAllByText("Owner").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Agent state").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Review").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Checks").length).toBeGreaterThan(0);
  });

  it("expands an action queue item to show review, checks, blocker, progress, and effort identity", () => {
    renderLab("/initiatives/lab?concept=queue");
    const itemSummary = screen.getByText("Investigate reconnect regression").closest("summary");
    expect(itemSummary).toBeTruthy();
    fireEvent.click(itemSummary!);
    const item = itemSummary!.closest("details");
    expect(item?.open).toBe(true);
    expect(within(item!).getByText("Not requested — investigation may conclude without a PR")).toBeTruthy();
    expect(within(item!).getByText("Reproduction matrix in progress")).toBeTruthy();
    expect(within(item!).getByText("The failure is intermittent across recovery modes")).toBeTruthy();
    const progress = within(item!).getByRole("progressbar", { name: "Outcome progress" });
    expect(progress.getAttribute("aria-valuenow")).toBe("1");
    expect(progress.getAttribute("aria-valuemax")).toBe("3");
    expect(within(item!).getByText("Active effort · no PR yet")).toBeTruthy();
    expect(within(item!).getByText(/Unblocker:/)).toBeTruthy();
  });

  it("keeps sample Bead ids inside collapsed implementation metadata", () => {
    renderLab("/initiatives/lab?concept=queue");
    const itemSummary = screen.getByText("Responsive hardening").closest("summary");
    fireEvent.click(itemSummary!);
    const item = itemSummary!.closest("details");

    const metadataSummary = within(item!).getByText("Implementation metadata");
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
