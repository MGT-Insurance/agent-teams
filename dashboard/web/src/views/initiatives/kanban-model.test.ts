// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  INITIATIVE_KANBAN_COLUMNS,
  buildInitiativeBoard,
  type BoardInitiativeNode,
  type BoardPRReviewInput,
  type BoardPRWorkstreamInput,
  type BoardWorkstreamInput,
  type InitiativeKanbanColumnId,
} from "./kanban-model.js";

const PR_BASE = "https://github.com/example/repo/pull/";

function pr(number: number): string {
  return `${PR_BASE}${number}`;
}

function makeNode(
  over: Partial<BoardInitiativeNode> = {},
  initiativeOver: Partial<BoardInitiativeNode["initiative"]> = {},
): BoardInitiativeNode {
  return {
    initiative: {
      id: "at-board",
      title: "Board initiative",
      status: "open",
      priority: "P1",
      issue_type: "epic",
      prs: [],
      prReviews: [],
      prWorkstreams: [],
      ...initiativeOver,
    },
    workstreams: [],
    workstreamDiagnostics: [],
    ...over,
  };
}

function workstream(id: string, over: Partial<BoardWorkstreamInput> = {}): BoardWorkstreamInput {
  return {
    id,
    title: `Workstream ${id}`,
    status: "open",
    issueType: "task",
    priority: "P2",
    labels: [],
    progress: { total: 0, closed: 0 },
    ...over,
  };
}

function withPRs(
  workstreams: readonly BoardWorkstreamInput[],
  reviews: readonly BoardPRReviewInput[],
  associations: readonly BoardPRWorkstreamInput[],
): BoardInitiativeNode {
  return makeNode(
    { workstreams },
    {
      prs: reviews.map((review) => review.pr),
      prReviews: reviews,
      prWorkstreams: associations,
    },
  );
}

function onlyColumn(node: BoardInitiativeNode): InitiativeKanbanColumnId {
  const lane = buildInitiativeBoard([node]).lanes[0];
  expect(lane).toBeDefined();
  expect(lane?.cards).toHaveLength(1);
  return lane?.cards[0]?.columnId ?? "planned";
}

describe("initiative kanban columns", () => {
  it("exports the frozen seven columns in exact display order", () => {
    expect(INITIATIVE_KANBAN_COLUMNS).toEqual([
      { id: "planned", label: "Planned" },
      { id: "active", label: "Active" },
      { id: "verifying", label: "Verifying" },
      { id: "your-review", label: "Your Review" },
      { id: "external-review", label: "External Review" },
      { id: "blocked", label: "Blocked" },
      { id: "done", label: "Done" },
    ]);
  });
});

describe("buildInitiativeBoard placement", () => {
  it("places one explicit workstream in each of all seven columns", () => {
    const reviewPR = pr(1);
    const externalPR = pr(2);
    const node = makeNode(
      {
        workstreams: [
          workstream("ws-planned"),
          workstream("ws-active", { status: "in_progress" }),
          workstream("ws-verifying", { status: "in_progress", labels: ["track:test"] }),
          workstream("ws-review"),
          workstream("ws-external"),
          workstream("ws-blocked", { status: "blocked" }),
          workstream("ws-done", { status: "closed" }),
        ],
      },
      {
        prs: [reviewPR, externalPR],
        prReviews: [
          { pr: reviewPR, gate: "review" },
          { pr: externalPR, gate: "external" },
        ],
        prWorkstreams: [
          { pr: reviewPR, workstream: "ws-review" },
          { pr: externalPR, workstream: "ws-external" },
        ],
      },
    );

    const lane = buildInitiativeBoard([node]).lanes[0];
    expect(lane).toBeDefined();
    for (const column of INITIATIVE_KANBAN_COLUMNS) {
      expect(lane?.cells[column.id].count, column.label).toBe(1);
      expect(lane?.cells[column.id].cards[0]?.columnId).toBe(column.id);
    }
  });

  it.each([
    {
      name: "Done beats blocked and question",
      initiativeStatus: "done",
      workstreamStatus: "blocked",
      labels: [] as string[],
      gates: ["question"],
      expected: "done",
    },
    {
      name: "Blocked beats review",
      initiativeStatus: "open",
      workstreamStatus: "blocked",
      labels: [] as string[],
      gates: ["review"],
      expected: "blocked",
    },
    {
      name: "question beats review and ungated",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: [] as string[],
      gates: ["external", "", "review", "question"],
      expected: "blocked",
    },
    {
      name: "review beats unknown and external",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: [] as string[],
      gates: ["external", "future-gate", "review"],
      expected: "your-review",
    },
    {
      name: "ungated beats all-external",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: [] as string[],
      gates: ["external", ""],
      expected: "verifying",
    },
    {
      name: "unknown beats all-external",
      initiativeStatus: "open",
      workstreamStatus: "open",
      labels: [] as string[],
      gates: ["external", "future-gate"],
      expected: "verifying",
    },
    {
      name: "exact test track beats all-external",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: ["track:test"],
      gates: ["external"],
      expected: "verifying",
    },
    {
      name: "all-external beats active",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: [] as string[],
      gates: ["external", "external"],
      expected: "external-review",
    },
    {
      name: "active requires in_progress",
      initiativeStatus: "open",
      workstreamStatus: "in_progress",
      labels: ["track:testing"],
      gates: [] as string[],
      expected: "active",
    },
    {
      name: "unknown nonterminal status is planned",
      initiativeStatus: "open",
      workstreamStatus: "waiting-for-something-new",
      labels: [] as string[],
      gates: [] as string[],
      expected: "planned",
    },
  ])("applies exact precedence: $name", ({ initiativeStatus, workstreamStatus, labels, gates, expected }) => {
    const reviews = gates.map((gate, index) => ({ pr: pr(index + 10), gate }));
    const node = withPRs(
      [workstream("ws", { status: workstreamStatus, labels })],
      reviews,
      reviews.map(({ pr: href }) => ({ pr: href, workstream: "ws" })),
    );
    node.initiative.status = initiativeStatus;

    expect(onlyColumn(node)).toBe(expected);
  });

  it("keeps one card identity before and after exact PR attachment", () => {
    const href = pr(30);
    const before = buildInitiativeBoard([
      makeNode({ workstreams: [workstream("ws-stable", { status: "in_progress" })] }),
    ]).lanes[0];
    const after = buildInitiativeBoard([
      withPRs(
        [workstream("ws-stable", { status: "in_progress" })],
        [{ pr: href, gate: "external" }],
        [{ pr: href, workstream: "ws-stable" }],
      ),
    ]).lanes[0];

    expect(before?.cards.map((card) => card.key)).toEqual(["ws-stable"]);
    expect(after?.cards.map((card) => card.key)).toEqual(["ws-stable"]);
    expect(before?.cards[0]?.columnId).toBe("active");
    expect(after?.cards[0]).toMatchObject({
      key: "ws-stable",
      columnId: "external-review",
      pullRequests: [{ href, rawGate: "external", gateKind: "external" }],
    });
  });

  it("aggregates disagreeing PRs on the same card without creating PR cards", () => {
    const reviews = [
      { pr: pr(40), gate: "external" },
      { pr: pr(41), gate: "review" },
      { pr: pr(42), gate: "question" },
      { pr: pr(43), gate: "" },
    ];
    const lane = buildInitiativeBoard([
      withPRs(
        [workstream("ws-many", { status: "in_progress" })],
        reviews,
        reviews.map(({ pr: href }) => ({ pr: href, workstream: "ws-many" })),
      ),
    ]).lanes[0];

    expect(lane?.cards).toHaveLength(1);
    expect(lane?.cards[0]?.key).toBe("ws-many");
    expect(lane?.cards[0]?.pullRequests.map((item) => item.href)).toEqual(reviews.map((item) => item.pr));
    expect(lane?.cards[0]?.columnId).toBe("blocked");
    expect(lane?.accounting).toMatchObject({ cardCount: 1, assignedPRCount: 4 });
  });
});

describe("buildInitiativeBoard identity and diagnostics", () => {
  it("synthesizes exactly one canonical fallback only when no direct child exists", () => {
    const fallbackOnly = buildInitiativeBoard([
      makeNode(
        {
          workstreams: [
            workstream("server-fallback", {
              kind: "fallback",
              title: "Stale server fallback title",
              status: "blocked",
            }),
          ],
        },
        { id: "at-empty", title: "Canonical initiative", status: "deferred" },
      ),
    ]).lanes[0];
    const withChild = buildInitiativeBoard([
      makeNode(
        {
          workstreams: [
            workstream("server-fallback", { kind: "fallback" }),
            workstream("direct-child"),
          ],
        },
        { id: "at-child" },
      ),
    ]).lanes[0];

    expect(fallbackOnly?.cards).toEqual([
      expect.objectContaining({
        key: "initiative:at-empty",
        kind: "fallback",
        workstreamId: null,
        title: "Canonical initiative",
        rawStatus: "deferred",
        columnId: "planned",
      }),
    ]);
    expect(withChild?.cards.map((card) => card.key)).toEqual(["direct-child"]);
  });

  it("attaches an exact nested-descendant association to its projected direct-child owner", () => {
    const href = pr(50);
    const lane = buildInitiativeBoard([
      withPRs(
        [
          workstream("direct", {
            memberIds: ["direct", "nested-a", "nested-b"],
            progress: { total: 4, closed: 3 },
          }),
        ],
        [{ pr: href, gate: "review" }],
        [{ pr: href, workstream: "nested-b" }],
      ),
    ]).lanes[0];

    expect(lane?.cards[0]).toMatchObject({
      key: "direct",
      progress: { total: 4, closed: 3 },
      memberIds: ["direct", "nested-a", "nested-b"],
      pullRequests: [{ href }],
      columnId: "your-review",
    });
  });

  it("keeps omitted, stale, and unmatched PR associations visible as diagnostics", () => {
    const assigned = pr(60);
    const omitted = pr(61);
    const staleTarget = pr(62);
    const missingPR = pr(63);
    const lane = buildInitiativeBoard([
      makeNode(
        { workstreams: [workstream("ws-a"), workstream("ws-b")] },
        {
          prs: [assigned, omitted, staleTarget],
          prReviews: [
            { pr: assigned, gate: "external" },
            { pr: omitted, gate: "review" },
            { pr: staleTarget, gate: "future-gate" },
          ],
          prWorkstreams: [
            { pr: assigned, workstream: "ws-a" },
            { pr: staleTarget, workstream: "deleted-workstream" },
            { pr: missingPR, workstream: "ws-b" },
            { pr: assigned, workstream: "ws-b" },
          ],
        },
      ),
    ]).lanes[0];

    expect(lane?.cards.find((card) => card.key === "ws-a")?.pullRequests).toEqual([
      { href: assigned, rawGate: "external", gateKind: "external" },
    ]);
    expect(lane?.cards.find((card) => card.key === "ws-b")?.pullRequests).toEqual([]);
    expect(lane?.diagnostics.unassignedPRs).toEqual([
      { href: omitted, rawGate: "review", gateKind: "review" },
      { href: staleTarget, rawGate: "future-gate", gateKind: "unknown" },
    ]);
    expect(lane?.diagnostics.staleAssociations.map(({ reason }) => reason)).toEqual([
      "missing-workstream",
      "missing-pr",
      "conflicting-workstream",
    ]);
    expect(lane?.accounting).toMatchObject({
      sourcePRCount: 3,
      assignedPRCount: 1,
      unassignedPRCount: 2,
      staleAssociationCount: 3,
    });
  });

  it("preserves orphan, cycle, no-root, and load diagnostics from the server projection", () => {
    const source = [
      { kind: "orphan", beadId: "orphan-1", message: "Parent not found" },
      { kind: "cycle", beadId: "cycle-1", message: "Parent chain cycles" },
      { kind: "no-root", message: "Initiative has no project epic" },
      { kind: "load-error", message: "Using last-known-good workstreams" },
    ];
    const lane = buildInitiativeBoard([
      makeNode({ workstreamDiagnostics: source, workstreams: [workstream("healthy")] }),
    ]).lanes[0];

    expect(lane?.diagnostics.source).toEqual(source);
    expect(lane?.cards.map((card) => card.key)).toEqual(["healthy"]);
  });

  it("renders raw unknown status and gate values honestly", () => {
    const unknownGatePR = pr(70);
    const lane = buildInitiativeBoard([
      makeNode(
        {
          workstreams: [
            workstream("unknown-status", { status: "awaiting_launch" }),
            workstream("unknown-gate", { status: "open" }),
          ],
        },
        {
          prs: [unknownGatePR],
          prReviews: [{ pr: unknownGatePR, gate: "vendor-hold" }],
          prWorkstreams: [{ pr: unknownGatePR, workstream: "unknown-gate" }],
        },
      ),
    ]).lanes[0];

    expect(lane?.cards.find((card) => card.key === "unknown-status")).toMatchObject({
      rawStatus: "awaiting_launch",
      columnId: "planned",
    });
    expect(lane?.cards.find((card) => card.key === "unknown-gate")?.pullRequests).toEqual([
      { href: unknownGatePR, rawGate: "vendor-hold", gateKind: "unknown" },
    ]);
    expect(lane?.cards.find((card) => card.key === "unknown-gate")?.columnId).toBe("verifying");
  });
});

describe("buildInitiativeBoard stability and accounting", () => {
  it("preserves snapshot initiative order and uses source order with an id tie-break for cards", () => {
    const first = makeNode(
      {
        workstreams: [
          workstream("later", { sourceOrder: 9 }),
          workstream("tie-z", { sourceOrder: 2 }),
          workstream("tie-a", { sourceOrder: 2 }),
          workstream("middle", { sourceOrder: 5 }),
        ],
      },
      { id: "initiative-z" },
    );
    const second = makeNode(
      { workstreams: [workstream("second-card")] },
      { id: "initiative-a" },
    );

    const board = buildInitiativeBoard([first, second]);
    expect(board.lanes.map((lane) => lane.key)).toEqual(["initiative-z", "initiative-a"]);
    expect(board.lanes[0]?.cards.map((card) => card.key)).toEqual([
      "tie-a",
      "tie-z",
      "middle",
      "later",
    ]);
  });

  it("preserves array order when the producer supplies no explicit source order", () => {
    const lane = buildInitiativeBoard([
      makeNode({ workstreams: [workstream("z"), workstream("a"), workstream("m")] }),
    ]).lanes[0];

    expect(lane?.cards.map((card) => card.key)).toEqual(["z", "a", "m"]);
  });

  it("accounts for every unique workstream exactly once across one cell", () => {
    const board = buildInitiativeBoard([
      makeNode(
        {
          workstreams: [
            workstream("one", { status: "open" }),
            workstream("two", { status: "open" }),
            workstream("three", { status: "in_progress" }),
          ],
        },
        { id: "first" },
      ),
      makeNode({ workstreams: [] }, { id: "fallback" }),
    ]);
    const cardKeys = board.lanes.flatMap((lane) => lane.cards.map((card) => `${lane.key}:${card.key}`));

    expect(new Set(cardKeys).size).toBe(cardKeys.length);
    expect(board.lanes.every((lane) => lane.accounting.cardCount === lane.accounting.placedCardCount)).toBe(true);
    expect(board.lanes.every((lane) => lane.accounting.cardCount === lane.accounting.expectedCardCount)).toBe(true);
    expect(board.accounting).toMatchObject({
      initiativeCount: 2,
      expectedCardCount: 4,
      cardCount: 4,
      placedCardCount: 4,
    });
    expect(
      board.lanes.flatMap((lane) => INITIATIVE_KANBAN_COLUMNS.map(({ id }) => lane.cells[id].count)).reduce(
        (sum, count) => sum + count,
        0,
      ),
    ).toBe(4);
  });

  it("deduplicates malformed repeated workstream and association rows without duplicating cards or PRs", () => {
    const href = pr(80);
    const repeated = workstream("repeat", { status: "in_progress" });
    const lane = buildInitiativeBoard([
      makeNode(
        { workstreams: [repeated, { ...repeated }, workstream("other")] },
        {
          prs: [href, href],
          prReviews: [{ pr: href, gate: "external" }],
          prWorkstreams: [
            { pr: href, workstream: "repeat" },
            { pr: href, workstream: "repeat" },
          ],
        },
      ),
    ]).lanes[0];

    expect(lane?.cards.map((card) => card.key)).toEqual(["repeat", "other"]);
    expect(lane?.cards.find((card) => card.key === "repeat")?.pullRequests).toHaveLength(1);
    expect(lane?.diagnostics.duplicateWorkstreamIds).toEqual(["repeat"]);
    expect(lane?.accounting).toMatchObject({ cardCount: 2, placedCardCount: 2, sourcePRCount: 1 });
  });

  it("does not depend on browser globals", () => {
    expect(typeof document).toBe("undefined");
    expect(typeof window).toBe("undefined");

    expect(buildInitiativeBoard([makeNode()]).accounting).toMatchObject({
      initiativeCount: 1,
      cardCount: 1,
    });
  });
});
