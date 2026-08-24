import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { AppRoutes } from "../../router.js";
import InitiativeLabView from "./index.js";
import { initiatives, workScenarios } from "./scenarios.js";

const originalScrollYDescriptor = Object.getOwnPropertyDescriptor(window, "scrollY");

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

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  if (originalScrollYDescriptor) {
    Object.defineProperty(window, "scrollY", originalScrollYDescriptor);
  }
});

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
      "Harden recovery handoff",
      "Publish matching runtimes",
    ]);
    expect(workScenarios.flatMap((work) => work.pullRequests.map((pullRequest) => pullRequest.number))).toEqual([
      180, 176, 179, 174,
    ]);
    expect(workScenarios.filter((work) => work.pullRequests.length === 0).map((work) => work.pipelineStage)).toEqual([
      "building",
      "building",
    ]);

    const recoveryWork = workScenarios.filter((work) => work.initiativeId === "session-recovery");
    expect(new Set(recoveryWork.map((work) => work.id)).size).toBe(3);
    expect(recoveryWork.map((work) => ({
      title: work.title,
      stage: work.pipelineStage,
      pullRequests: work.pullRequests.map((pullRequest) => pullRequest.number),
    }))).toEqual([
      { title: "Investigate reconnect regression", stage: "building", pullRequests: [] },
      { title: "Preserve session identity", stage: "in-review", pullRequests: [176] },
      { title: "Harden recovery handoff", stage: "ready-to-land", pullRequests: [179] },
    ]);
    expect(recoveryWork[1]?.pullRequests[0]?.externalReview).toEqual({
      status: "Waiting on external review",
      reviewer: "Agent-teams maintainer",
    });
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
    expect(screen.queryByText("Checks in flight")).toBeNull();
    expect(screen.queryByText("Latest log")).toBeNull();
    expect(screen.queryByText("Verification history")).toBeNull();
    expect(screen.queryByRole("heading", { name: "Initiative distribution" })).toBeNull();

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

  it("keeps five exact stages and a primary delivery identity plus initiative affiliation on every card", () => {
    renderLab("/initiatives/lab?concept=pipeline");

    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual([
      "Investigating",
      "Building",
      "In Review",
      "Ready to Land",
      "Done",
    ]);
    const cards = screen.getAllByRole("button", { name: /Open details/i });
    expect(cards).toHaveLength(6);
    for (const card of cards) {
      const identity = card.querySelector(".pipeline-card__identity");
      expect(identity?.textContent).toMatch(/^(PR #\d+|Active effort)$/);
      expect(within(card).getByText(/^Initiative · /)).toBeTruthy();
    }

    const inReview = screen.getByRole("heading", { name: "In Review" }).closest("section");
    const externalReviewCard = within(inReview!).getByRole("button", { name: /PR #176.*Preserve session identity/i });
    expect(within(externalReviewCard).getByText("Waiting on external review")).toBeTruthy();
    expect(screen.queryByText("Agent-teams maintainer")).toBeNull();
    expect(screen.queryByRole("heading", { name: /External Review|Waiting/i })).toBeNull();
  });

  it.each([
    [/Active effort.*Investigate reconnect regression/i, "Building", "mouse"],
    [/PR #176.*Preserve session identity/i, "In Review", "keyboard"],
    [/PR #179.*Harden recovery handoff/i, "Ready to Land", "mouse"],
  ] as const)("selecting %s reveals the same initiative distribution without moving cards", (name, stageName, input) => {
    renderLab("/initiatives/lab?concept=pipeline");
    expect(screen.queryByRole("heading", { name: "Initiative distribution" })).toBeNull();

    const trigger = screen.getByRole("button", { name });
    if (input === "keyboard") {
      trigger.focus();
      fireEvent.click(trigger, { detail: 0 });
    } else {
      fireEvent.click(trigger);
    }

    const detail = screen.getByRole("dialog");
    const distribution = within(detail).getByRole("heading", { name: "Initiative distribution" }).closest("section");
    const rows = within(distribution!).getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(rows.map((row) => row.textContent)).toEqual([
      "Active effortInvestigate reconnect regressionBuilding",
      "PR #176Preserve session identityIn Review",
      "PR #179Harden recovery handoffReady to Land",
    ]);

    const relatedCards = Array.from(document.querySelectorAll<HTMLElement>('[data-initiative-related="true"]'));
    expect(relatedCards).toHaveLength(3);
    expect(relatedCards.map((card) => card.querySelector(".pipeline-card__identity")?.textContent)).toEqual([
      "Active effort",
      "PR #176",
      "PR #179",
    ]);
    expect(screen.getByRole("button", { name: /PR #180.*Prototype interaction lab/i }).closest("article")
      ?.hasAttribute("data-initiative-related")).toBe(false);
    expect(screen.getByRole("button", { name }).closest("section")
      ?.querySelector("h3")?.textContent).toBe(stageName);

    expect(within(screen.getByRole("heading", { name: "Building" }).closest("section")!)
      .getByRole("button", { name: /Active effort.*Investigate reconnect regression/i })).toBeTruthy();
    expect(within(screen.getByRole("heading", { name: "In Review" }).closest("section")!)
      .getByRole("button", { name: /PR #176.*Preserve session identity/i })).toBeTruthy();
    expect(within(screen.getByRole("heading", { name: "Ready to Land" }).closest("section")!)
      .getByRole("button", { name: /PR #179.*Harden recovery handoff/i })).toBeTruthy();
  });

  it("coordinates pointer hover across only initiative siblings without changing stages or selection", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    const hoveredTrigger = screen.getByRole("button", { name: /PR #176.*Preserve session identity/i });
    const hoveredCard = hoveredTrigger.closest("article");
    const unrelatedCard = screen.getByRole("button", { name: /PR #180.*Prototype interaction lab/i })
      .closest("article");
    const originalStages = new Map([
      [/Active effort.*Investigate reconnect regression/i, "Building"],
      [/PR #176.*Preserve session identity/i, "In Review"],
      [/PR #179.*Harden recovery handoff/i, "Ready to Land"],
    ] as const);

    fireEvent.pointerEnter(hoveredCard!, { pointerType: "mouse" });

    expect(hoveredCard?.getAttribute("data-pipeline-hovered")).toBe("true");
    const hoverRelatedCards = Array.from(
      document.querySelectorAll<HTMLElement>('[data-initiative-hover-related="true"]'),
    );
    expect(hoverRelatedCards.map((card) => card.querySelector(".pipeline-card__identity")?.textContent)).toEqual([
      "Active effort",
      "PR #179",
    ]);
    expect(unrelatedCard?.hasAttribute("data-pipeline-hovered")).toBe(false);
    expect(unrelatedCard?.hasAttribute("data-initiative-hover-related")).toBe(false);
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(0);
    for (const [name, stageName] of originalStages) {
      expect(screen.getByRole("button", { name }).closest("section")?.querySelector("h3")?.textContent)
        .toBe(stageName);
    }

    fireEvent.click(hoveredTrigger);
    const persistentlyRelatedCards = Array.from(
      document.querySelectorAll<HTMLElement>('[data-initiative-related="true"]'),
    );
    expect(persistentlyRelatedCards).toHaveLength(3);
    expect(persistentlyRelatedCards.every((card) => card.classList.contains("pipeline-card--related"))).toBe(true);
    expect(hoveredCard?.classList.contains("pipeline-card--selected")).toBe(true);
    expect(unrelatedCard?.classList.contains("pipeline-card--related")).toBe(false);
    expect(unrelatedCard?.classList.contains("pipeline-card--selected")).toBe(false);
    expect(document.querySelectorAll('[data-pipeline-hovered="true"]')).toHaveLength(1);
    expect(document.querySelectorAll('[data-initiative-hover-related="true"]')).toHaveLength(2);

    fireEvent.pointerLeave(hoveredCard!, { pointerType: "mouse" });
    expect(document.querySelectorAll('[data-pipeline-hovered="true"]')).toHaveLength(0);
    expect(document.querySelectorAll('[data-initiative-hover-related="true"]')).toHaveLength(0);
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(3);
    expect(hoveredCard?.classList.contains("pipeline-card--selected")).toBe(true);
    expect(persistentlyRelatedCards.every((card) => card.classList.contains("pipeline-card--related"))).toBe(true);
    expect(screen.getByRole("dialog", { name: "Preserve session identity" })).toBeTruthy();
    for (const [name, stageName] of originalStages) {
      expect(screen.getByRole("button", { name }).closest("section")?.querySelector("h3")?.textContent)
        .toBe(stageName);
    }
  });

  it.each([
    ["mouse"],
    ["keyboard"],
  ] as const)("opens and closes detail by %s without changing page or board scroll", (input) => {
    renderLab("/initiatives/lab?concept=pipeline");
    const focusSpy = vi.spyOn(HTMLElement.prototype, "focus");
    Object.defineProperty(window, "scrollY", { configurable: true, value: 731, writable: true });
    const scroller = screen.getByRole("region", { name: /Outcome Pipeline board/ });
    scroller.scrollLeft = 487;
    const trigger = screen.getByRole("button", { name: /PR #179.*Harden recovery handoff/i });

    if (input === "keyboard") {
      trigger.focus();
      fireEvent.click(trigger, { detail: 0 });
    } else {
      fireEvent.click(trigger);
    }

    const detail = screen.getByRole("dialog", { name: "Harden recovery handoff" });
    expect(document.activeElement).toBe(detail);
    expect(focusSpy).toHaveBeenLastCalledWith({ preventScroll: true });
    expect(window.scrollY).toBe(731);
    expect(scroller.scrollLeft).toBe(487);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(focusSpy).toHaveBeenLastCalledWith({ preventScroll: true });
    expect(window.scrollY).toBe(731);
    expect(scroller.scrollLeft).toBe(487);
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(0);
  });

  it("shows external reviewer detail only after #176 activation", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    expect(screen.queryByText("Agent-teams maintainer")).toBeNull();

    const inReview = screen.getByRole("heading", { name: "In Review" }).closest("section");
    fireEvent.click(within(inReview!).getByRole("button", { name: /PR #176.*Preserve session identity/i }));

    const detail = screen.getByRole("dialog", { name: "Preserve session identity" });
    expect(within(detail).getByRole("heading", { name: "Waiting on external review" })).toBeTruthy();
    expect(within(detail).getByText("Agent-teams maintainer")).toBeTruthy();
  });

  it("clears the initiative footprint and restores focus through Close and Escape", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    const focusSpy = vi.spyOn(HTMLElement.prototype, "focus");
    Object.defineProperty(window, "scrollY", { configurable: true, value: 619, writable: true });
    const scroller = screen.getByRole("region", { name: /Outcome Pipeline board/ });
    scroller.scrollLeft = 403;
    const inReview = screen.getByRole("heading", { name: "In Review" }).closest("section");
    const reviewCard = within(inReview!).getByRole("button", { name: /PR #176.*Preserve session identity/i });
    fireEvent.click(reviewCard);
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { name: "Close details for Preserve session identity" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(0);
    expect(document.activeElement).toBe(reviewCard);
    expect(focusSpy).toHaveBeenLastCalledWith({ preventScroll: true });
    expect(window.scrollY).toBe(619);
    expect(scroller.scrollLeft).toBe(403);

    const readyToLand = screen.getByRole("heading", { name: "Ready to Land" }).closest("section");
    const landCard = within(readyToLand!).getByRole("button", { name: /PR #179.*Harden recovery handoff/i });
    fireEvent.click(landCard);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(0);
    expect(document.activeElement).toBe(landCard);
    expect(focusSpy).toHaveBeenLastCalledWith({ preventScroll: true });
    expect(window.scrollY).toBe(619);
    expect(scroller.scrollLeft).toBe(403);
  });

  it("keeps live verification visible on its lifecycle card and detail behind activation", () => {
    renderLab("/initiatives/lab?concept=pipeline");

    const stage = screen.getByRole("heading", { name: "Building" }).closest("section");
    expect(stage).toBeTruthy();
    const card = within(stage!).getByRole("button", { name: /Active effort.*Responsive hardening/i });
    expect(within(card).getByText("Live verification")).toBeTruthy();
    expect(within(card).getByText("Nadia · Mobile pipeline · 390 × 844")).toBeTruthy();
    expect(
      screen.queryByText("The five-stage board stays inside its local horizontal scroller at the mobile boundary."),
    ).toBeNull();
    expect(screen.queryByText(/Playwright pass 18 of 24/)).toBeNull();

    fireEvent.click(card);
    const detail = screen.getByRole("dialog", { name: "Responsive hardening" });
    expect(
      within(detail).getByRole("heading", { name: "Nadia is verifying Mobile pipeline · 390 × 844" }),
    ).toBeTruthy();
    expect(
      within(detail).getByText("The five-stage board stays inside its local horizontal scroller at the mobile boundary."),
    ).toBeTruthy();
    expect(within(detail).getByText("No document-level horizontal overflow")).toBeTruthy();
    expect(within(detail).getByText(/Playwright pass 18 of 24/)).toBeTruthy();
    expect(within(detail).getByText("Mobile overflow check running")).toBeTruthy();
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
    expect(within(inReview!).getByRole("button", { name: /PR #176/ })).toBeTruthy();
    expect(within(readyToLand!).getByRole("button", { name: /PR #179/ })).toBeTruthy();

    fireEvent.click(screen.getByRole("checkbox", { name: "Needs you only" }));

    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual(stageNames);
    expect(within(inReview!).getByRole("button", { name: /PR #180.*Prototype interaction lab/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Responsive hardening/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Investigate reconnect regression/ })).toBeNull();
  });

  it("filters live verification independently and composes both filters with an honest empty state", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    const stageNames = ["Investigating", "Building", "In Review", "Ready to Land", "Done"];
    const liveFilter = screen.getByRole("checkbox", { name: "Live verification only" });
    const needsYouFilter = screen.getByRole("checkbox", { name: "Needs you only" });
    const controls = screen.getByRole("group", { name: "Filter Outcome Pipeline" });

    expect(screen.getByLabelText("1 live verification item").textContent).toBe("1");
    fireEvent.click(liveFilter);
    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual(stageNames);
    const building = screen.getByRole("heading", { name: "Building" }).closest("section");
    expect(within(building!).getByRole("button", { name: /Responsive hardening/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Prototype interaction lab/ })).toBeNull();
    expect(within(controls).getByRole("status").textContent).toContain("1 live verification item");

    fireEvent.click(liveFilter);
    fireEvent.click(needsYouFilter);
    expect(screen.getByRole("button", { name: /Prototype interaction lab/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Responsive hardening/ })).toBeNull();

    fireEvent.click(liveFilter);
    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual(stageNames);
    expect(screen.queryByRole("button", { name: /Prototype interaction lab/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Responsive hardening/ })).toBeNull();
    expect(within(controls).getByRole("status").textContent).toBe("No delivery items match both active filters.");
    expect(screen.getAllByText("No matching items")).toHaveLength(5);
  });

  it("clears selected detail and related treatment when filters remove its card", () => {
    renderLab("/initiatives/lab?concept=pipeline");
    const responsiveCard = screen.getByRole("button", { name: /Active effort.*Responsive hardening/i });
    fireEvent.click(responsiveCard);
    expect(screen.getByRole("dialog", { name: "Responsive hardening" })).toBeTruthy();
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(2);

    fireEvent.click(screen.getByRole("checkbox", { name: "Needs you only" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelectorAll('[data-initiative-related="true"]')).toHaveLength(0);
    expect(screen.getByRole("button", { name: /PR #180.*Prototype interaction lab/i })).toBeTruthy();
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
    expect(within(item!).getByText("Not requested — no PR yet")).toBeTruthy();
    expect(within(item!).getByText("Reconnect checks running during the build")).toBeTruthy();
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
