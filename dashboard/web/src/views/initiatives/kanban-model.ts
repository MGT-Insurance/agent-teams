export const INITIATIVE_KANBAN_COLUMNS = [
  { id: "planned", label: "Planned" },
  { id: "active", label: "Active" },
  { id: "verifying", label: "Verifying" },
  { id: "your-review", label: "Your Review" },
  { id: "external-review", label: "External Review" },
  { id: "blocked", label: "Blocked" },
  { id: "done", label: "Done" },
] as const;

export type InitiativeKanbanColumnId = (typeof INITIATIVE_KANBAN_COLUMNS)[number]["id"];

export interface BoardPRReviewInput {
  pr: string;
  /** Empty means the producer found no gate. Unknown non-empty values stay visible. */
  gate: string;
}

export interface BoardPRWorkstreamInput {
  pr: string;
  workstream: string;
}

/**
 * Structural subset of ParsedInitiative consumed by the board model.
 *
 * The server/shared track can extend its existing ParsedInitiative with
 * `prWorkstreams` and remain assignable without this module importing shared
 * runtime code.
 */
export interface BoardInitiativeInput {
  id: string;
  title: string;
  status: string;
  priority?: string | number;
  issue_type?: string;
  prs?: readonly string[];
  prReviews?: readonly BoardPRReviewInput[];
  prWorkstreams?: readonly BoardPRWorkstreamInput[];
}

export interface BoardWorkstreamProgress {
  total: number;
  closed: number;
}

/** A server-projected direct child of the initiative epic. */
export interface BoardWorkstreamInput {
  id: string;
  title: string;
  status: string;
  issueType?: string;
  priority?: string | number;
  labels?: readonly string[];
  progress?: BoardWorkstreamProgress;
  /**
   * Direct child id plus any nested descendant ids rolled into this card.
   * This lets an exact persisted association to a nested Bead reach its
   * projected direct-child card without client-side ancestry inference.
   */
  memberIds?: readonly string[];
  /** Stable producer order; equal values use the Bead id as a tie-break. */
  sourceOrder?: number;
  /** A server may include its fallback projection; the model canonicalizes it. */
  kind?: "workstream" | "fallback";
}

export interface BoardWorkstreamDiagnosticInput {
  kind: string;
  message: string;
  beadId?: string;
}

/**
 * Local structural seam for InitiativeNode until the shared/server data track
 * adds these projection fields.
 */
export interface BoardInitiativeNode {
  initiative: BoardInitiativeInput;
  workstreams?: readonly BoardWorkstreamInput[];
  workstreamDiagnostics?: readonly BoardWorkstreamDiagnosticInput[];
}

export type BoardPRGateKind = "question" | "review" | "external" | "ungated" | "unknown";

export interface BoardPullRequest {
  href: string;
  rawGate: string;
  gateKind: BoardPRGateKind;
}

export interface InitiativeKanbanCard {
  key: string;
  kind: "workstream" | "fallback";
  workstreamId: string | null;
  title: string;
  rawStatus: string;
  issueType: string;
  priority: string | number | null;
  labels: readonly string[];
  progress: BoardWorkstreamProgress;
  memberIds: readonly string[];
  pullRequests: readonly BoardPullRequest[];
  columnId: InitiativeKanbanColumnId;
}

export interface StalePRWorkstreamAssociation {
  association: BoardPRWorkstreamInput;
  reason: "missing-pr" | "missing-workstream" | "conflicting-workstream";
}

export interface InitiativeLaneDiagnostics {
  unassignedPRs: readonly BoardPullRequest[];
  staleAssociations: readonly StalePRWorkstreamAssociation[];
  source: readonly BoardWorkstreamDiagnosticInput[];
  duplicateWorkstreamIds: readonly string[];
}

export interface InitiativeKanbanCell {
  columnId: InitiativeKanbanColumnId;
  count: number;
  cards: readonly InitiativeKanbanCard[];
}

export interface InitiativeLaneAccounting {
  sourceWorkstreamCount: number;
  expectedCardCount: number;
  cardCount: number;
  placedCardCount: number;
  sourcePRCount: number;
  assignedPRCount: number;
  unassignedPRCount: number;
  staleAssociationCount: number;
}

export interface InitiativeKanbanLane<TNode extends BoardInitiativeNode = BoardInitiativeNode> {
  key: string;
  node: TNode;
  cells: Record<InitiativeKanbanColumnId, InitiativeKanbanCell>;
  cards: readonly InitiativeKanbanCard[];
  diagnostics: InitiativeLaneDiagnostics;
  accounting: InitiativeLaneAccounting;
}

export interface InitiativeBoardAccounting {
  initiativeCount: number;
  expectedCardCount: number;
  cardCount: number;
  placedCardCount: number;
  sourcePRCount: number;
  assignedPRCount: number;
  unassignedPRCount: number;
  staleAssociationCount: number;
}

export interface InitiativeKanbanBoard<TNode extends BoardInitiativeNode = BoardInitiativeNode> {
  columns: typeof INITIATIVE_KANBAN_COLUMNS;
  lanes: readonly InitiativeKanbanLane<TNode>[];
  accounting: InitiativeBoardAccounting;
}

interface OrderedWorkstream {
  value: BoardWorkstreamInput;
  index: number;
}

function normalizedStatus(status: string): string {
  return status.toLowerCase();
}

function gateKind(rawGate: string): BoardPRGateKind {
  if (rawGate === "question" || rawGate === "review" || rawGate === "external") {
    return rawGate;
  }
  return rawGate === "" ? "ungated" : "unknown";
}

function makePullRequest(href: string, reviewsByPR: ReadonlyMap<string, string>): BoardPullRequest {
  const rawGate = reviewsByPR.get(href) ?? "";
  return { href, rawGate, gateKind: gateKind(rawGate) };
}

function compareWorkstreams(a: OrderedWorkstream, b: OrderedWorkstream): number {
  const aOrder = Number.isFinite(a.value.sourceOrder) ? (a.value.sourceOrder as number) : a.index;
  const bOrder = Number.isFinite(b.value.sourceOrder) ? (b.value.sourceOrder as number) : b.index;
  return aOrder - bOrder || a.value.id.localeCompare(b.value.id) || a.index - b.index;
}

function placeCard(
  initiativeStatus: string,
  workstreamStatus: string,
  labels: readonly string[],
  pullRequests: readonly BoardPullRequest[],
): InitiativeKanbanColumnId {
  const initiativeState = normalizedStatus(initiativeStatus);
  const workstreamState = normalizedStatus(workstreamStatus);

  if (
    workstreamState === "closed" ||
    workstreamState === "done" ||
    initiativeState === "closed" ||
    initiativeState === "done"
  ) {
    return "done";
  }
  if (workstreamState === "blocked" || pullRequests.some((pr) => pr.gateKind === "question")) {
    return "blocked";
  }
  if (pullRequests.some((pr) => pr.gateKind === "review")) {
    return "your-review";
  }
  if (
    pullRequests.some((pr) => pr.gateKind === "ungated" || pr.gateKind === "unknown") ||
    (workstreamState === "in_progress" && labels.includes("track:test"))
  ) {
    return "verifying";
  }
  if (pullRequests.length > 0 && pullRequests.every((pr) => pr.gateKind === "external")) {
    return "external-review";
  }
  if (workstreamState === "in_progress") {
    return "active";
  }
  return "planned";
}

function emptyCells(): Record<InitiativeKanbanColumnId, InitiativeKanbanCell> {
  return {
    planned: { columnId: "planned", count: 0, cards: [] },
    active: { columnId: "active", count: 0, cards: [] },
    verifying: { columnId: "verifying", count: 0, cards: [] },
    "your-review": { columnId: "your-review", count: 0, cards: [] },
    "external-review": { columnId: "external-review", count: 0, cards: [] },
    blocked: { columnId: "blocked", count: 0, cards: [] },
    done: { columnId: "done", count: 0, cards: [] },
  };
}

function uniqueStrings(values: readonly string[]): string[] {
  return [...new Set(values)];
}

function explicitWorkstreams(
  inputs: readonly BoardWorkstreamInput[],
): { workstreams: BoardWorkstreamInput[]; duplicateIds: string[] } {
  const seen = new Set<string>();
  const duplicateIds: string[] = [];
  const workstreams: BoardWorkstreamInput[] = [];

  for (const { value } of inputs
    .map((value, index) => ({ value, index }))
    .filter(({ value }) => value.kind !== "fallback")
    .sort(compareWorkstreams)) {
    if (seen.has(value.id)) {
      duplicateIds.push(value.id);
      continue;
    }
    seen.add(value.id);
    workstreams.push(value);
  }
  return { workstreams, duplicateIds: uniqueStrings(duplicateIds) };
}

function sumAccounting(lanes: readonly InitiativeKanbanLane[]): InitiativeBoardAccounting {
  return lanes.reduce<InitiativeBoardAccounting>(
    (total, lane) => ({
      initiativeCount: total.initiativeCount + 1,
      expectedCardCount: total.expectedCardCount + lane.accounting.expectedCardCount,
      cardCount: total.cardCount + lane.accounting.cardCount,
      placedCardCount: total.placedCardCount + lane.accounting.placedCardCount,
      sourcePRCount: total.sourcePRCount + lane.accounting.sourcePRCount,
      assignedPRCount: total.assignedPRCount + lane.accounting.assignedPRCount,
      unassignedPRCount: total.unassignedPRCount + lane.accounting.unassignedPRCount,
      staleAssociationCount: total.staleAssociationCount + lane.accounting.staleAssociationCount,
    }),
    {
      initiativeCount: 0,
      expectedCardCount: 0,
      cardCount: 0,
      placedCardCount: 0,
      sourcePRCount: 0,
      assignedPRCount: 0,
      unassignedPRCount: 0,
      staleAssociationCount: 0,
    },
  );
}

function buildLane<TNode extends BoardInitiativeNode>(node: TNode): InitiativeKanbanLane<TNode> {
  const initiative = node.initiative;
  const sourcePRs = uniqueStrings(initiative.prs ?? []);
  const sourcePRSet = new Set(sourcePRs);
  const reviewsByPR = new Map<string, string>();
  for (const review of initiative.prReviews ?? []) {
    if (!reviewsByPR.has(review.pr)) reviewsByPR.set(review.pr, review.gate);
  }
  const pullRequestsByHref = new Map(
    sourcePRs.map((href) => [href, makePullRequest(href, reviewsByPR)] as const),
  );

  const explicit = explicitWorkstreams(node.workstreams ?? []);
  const targetToWorkstream = new Map<string, string>();
  for (const workstream of explicit.workstreams) {
    targetToWorkstream.set(workstream.id, workstream.id);
    for (const memberId of workstream.memberIds ?? []) {
      if (!targetToWorkstream.has(memberId)) targetToWorkstream.set(memberId, workstream.id);
    }
  }

  const assignedWorkstreamByPR = new Map<string, string>();
  const attachedByWorkstream = new Map<string, BoardPullRequest[]>();
  const staleAssociations: StalePRWorkstreamAssociation[] = [];
  for (const association of initiative.prWorkstreams ?? []) {
    if (!sourcePRSet.has(association.pr)) {
      staleAssociations.push({ association, reason: "missing-pr" });
      continue;
    }
    const workstreamId = targetToWorkstream.get(association.workstream);
    if (workstreamId === undefined) {
      staleAssociations.push({ association, reason: "missing-workstream" });
      continue;
    }
    const assigned = assignedWorkstreamByPR.get(association.pr);
    if (assigned !== undefined) {
      if (assigned !== workstreamId) {
        staleAssociations.push({ association, reason: "conflicting-workstream" });
      }
      continue;
    }
    assignedWorkstreamByPR.set(association.pr, workstreamId);
    const pullRequest = pullRequestsByHref.get(association.pr);
    if (pullRequest !== undefined) {
      const attached = attachedByWorkstream.get(workstreamId) ?? [];
      attached.push(pullRequest);
      attachedByWorkstream.set(workstreamId, attached);
    }
  }

  const cards: InitiativeKanbanCard[] =
    explicit.workstreams.length > 0
      ? explicit.workstreams.map((workstream) => {
          const labels = [...(workstream.labels ?? [])];
          const pullRequests = attachedByWorkstream.get(workstream.id) ?? [];
          return {
            key: workstream.id,
            kind: "workstream" as const,
            workstreamId: workstream.id,
            title: workstream.title,
            rawStatus: workstream.status,
            issueType: workstream.issueType ?? "",
            priority: workstream.priority ?? null,
            labels,
            progress: workstream.progress ?? { total: 0, closed: 0 },
            memberIds: uniqueStrings([workstream.id, ...(workstream.memberIds ?? [])]),
            pullRequests,
            columnId: placeCard(initiative.status, workstream.status, labels, pullRequests),
          };
        })
      : [
          {
            key: `initiative:${initiative.id}`,
            kind: "fallback" as const,
            workstreamId: null,
            title: initiative.title,
            rawStatus: initiative.status,
            issueType: initiative.issue_type ?? "initiative",
            priority: initiative.priority ?? null,
            labels: [],
            progress: { total: 0, closed: 0 },
            memberIds: [],
            pullRequests: [],
            columnId: placeCard(initiative.status, initiative.status, [], []),
          },
        ];

  const cells = emptyCells();
  for (const card of cards) {
    const cell = cells[card.columnId];
    const cellCards = [...cell.cards, card];
    cells[card.columnId] = { ...cell, count: cellCards.length, cards: cellCards };
  }

  const unassignedPRs = sourcePRs
    .filter((href) => !assignedWorkstreamByPR.has(href))
    .map((href) => pullRequestsByHref.get(href))
    .filter((pr): pr is BoardPullRequest => pr !== undefined);
  const assignedPRCount = cards.reduce((count, card) => count + card.pullRequests.length, 0);
  const placedCardCount = INITIATIVE_KANBAN_COLUMNS.reduce(
    (count, column) => count + cells[column.id].count,
    0,
  );

  return {
    key: initiative.id,
    node,
    cells,
    cards,
    diagnostics: {
      unassignedPRs,
      staleAssociations,
      source: [...(node.workstreamDiagnostics ?? [])],
      duplicateWorkstreamIds: explicit.duplicateIds,
    },
    accounting: {
      sourceWorkstreamCount: explicit.workstreams.length,
      expectedCardCount: explicit.workstreams.length || 1,
      cardCount: cards.length,
      placedCardCount,
      sourcePRCount: sourcePRs.length,
      assignedPRCount,
      unassignedPRCount: unassignedPRs.length,
      staleAssociationCount: staleAssociations.length,
    },
  };
}

/** Build the complete dashboard-wide board without DOM, network, or global state. */
export function buildInitiativeBoard<TNode extends BoardInitiativeNode>(
  nodes: readonly TNode[],
): InitiativeKanbanBoard<TNode> {
  const lanes = nodes.map(buildLane);
  return {
    columns: INITIATIVE_KANBAN_COLUMNS,
    lanes,
    accounting: sumAccounting(lanes),
  };
}
