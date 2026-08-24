// Pure parsing functions over raw CLI output.
// These are the riskiest logic — they are unit-tested against real fixtures.

import { existsSync } from "node:fs";
import type {
  RawInitiative,
  ParsedInitiative,
  SessionState,
  SessionSignal,
  ActivityStatus,
  DeliveryStatus,
  NeedsHumanFlavor,
  ExplicitGateKind,
  PRReview,
  InitiativeWorkstream,
  WorkstreamDiagnostic,
  InitiativeNode,
  InboxItem,
  WorkBead,
  Alert,
} from "@agent-teams/shared";
import { sessionKind, deriveSessionSignal, isValidSessionId } from "@agent-teams/shared";
import { splitNotesBlocks } from "./notes.js";

// Re-exported for existing consumers/tests that import deriveSessionSignal
// from this module — the implementation now lives in @agent-teams/shared
// (agent-teams-rybk.5.2), alongside sessionKind, as the one place that reads
// a SessionState's raw status/state fields. See shared/types.ts.
export { deriveSessionSignal };

// Legacy fallback for the root epic bead id: some initiatives registered before
// at-e3m recorded `epic:` in NOTES rather than in the description's routing
// header. Notes are not routing data — internal/initiative reads Description
// only, by its own scope boundary — so `fields.epic` cannot cover this case and
// this scan remains. It runs over notes ONLY; the description half comes from
// `fields.epic` (see parseInitiative), which is why nothing here ever looks at
// description text.
const EPIC_IN_NOTES_RE = /^epic:\s*(\S+)/m;

export function extractEpicFromNotes(notes: string): string | null {
  const m = EPIC_IN_NOTES_RE.exec(notes);
  return m ? (m[1] ?? null) : null;
}

// Parse a RawInitiative into a ParsedInitiative.
//
// The routing fields arrive already parsed, in raw.fields, produced by the Go
// component internal/initiative and attached by `ateam list-json`
// (agent-teams-ully.12). This file used to re-implement that matching rule in a
// regex of its own; two implementations of one rule stayed in parity only
// because tests said so, and the rule is subtle enough that the drift is what
// caused the at-jno7 incident. Reading raw.fields makes parity structural: a key
// rename or a rule change is a Go-only edit for this consumer.
//
// The PR list (raw.prs) is the same story (agent-teams-ssib.9): this file used
// to run its own extractPrUrl regex over notes/description to find one PR URL.
// That regex was https-only where the Go equivalent (route_match.go) accepts
// http too — a live divergence, not just a duplication — and it only ever
// found the first PR, invisible to any later one. raw.prs is Go's already-
// RESOLVED list (docs/multi-pr-contract.md §2a): the rail when non-empty, else
// the same Notes-then-Description fallback scan, computed once, in one
// language. Reading it removes both the divergence and the first-match bug.
export function parseInitiative(raw: RawInitiative): ParsedInitiative {
  // notes and description are typed as string but the registry can emit undefined
  // for freshly-created initiatives that have no NOTES section yet.  Coerce to ""
  // here so every downstream function receives a guaranteed string.
  const notes = raw.notes ?? "";
  const description = raw.description ?? "";

  // Defensive default: parseAteamListJson rejects a payload whose elements have
  // no fields object, but parseInitiative is also called directly (tests, ad-hoc
  // callers), so tolerate an absent object rather than throwing on property
  // access.
  const fields = raw.fields ?? {};

  return {
    ...raw,
    // Normalise notes/description so downstream code always has real strings.
    notes,
    description,
    problem: fields.problem ?? "",
    repo: fields.repo ?? "",
    worktree: fields.worktree ?? "",
    branch: fields.branch ?? "",
    team: fields.team ?? "",
    mode: fields.mode ?? "",
    // Defensive default for direct/ad-hoc callers (tests, older payloads) that
    // omit the key — a real `ateam list-json` payload always includes it.
    prs: raw.prs ?? [],
    prReviews: raw.pr_reviews ?? [],
    prWorkstreams: raw.pr_workstreams ?? [],
    epic: fields.epic ?? extractEpicFromNotes(notes),
  };
}

// Parse raw JSON output of `ateam list-json` into ParsedInitiative[].
// Throws on JSON parse failure (lets the caller return a structured error).
export function parseAteamListJson(raw: string): ParsedInitiative[] {
  const items: unknown = JSON.parse(raw);
  if (!Array.isArray(items)) {
    throw new Error("ateam list-json did not return an array");
  }
  const first = items[0];
  if (
    items.length > 0 &&
    (typeof first !== "object" ||
      first === null ||
      typeof (first as Record<string, unknown>)["id"] !== "string" ||
      typeof (first as Record<string, unknown>)["title"] !== "string")
  ) {
    throw new Error("ateam list-json: unexpected element shape (missing id or title)");
  }
  // The `fields` object is the ONLY source of routing data here now, so its
  // absence is checked on every element rather than only on the first: a payload
  // without it parses fine and yields initiatives whose repo/worktree/branch are
  // all "" — a silent blanking, which is the exact failure class this initiative
  // exists to remove. An `ateam` too old to emit it must break loudly instead.
  for (const [index, item] of items.entries()) {
    const fields = (item as Record<string, unknown>)["fields"];
    if (typeof fields !== "object" || fields === null || Array.isArray(fields)) {
      throw new Error(
        `ateam list-json: element ${index} has no "fields" object — the installed ateam is ` +
          `too old to attach parsed routing fields (run \`claude plugin update agent-teams\`)`,
      );
    }
  }
  return (items as RawInitiative[]).map(parseInitiative);
}

// Parse raw JSON output of `claude agents --json --all`.
export function parseClaudeAgents(raw: string): SessionState[] {
  const items: unknown = JSON.parse(raw);
  if (!Array.isArray(items)) {
    throw new Error("claude agents --json --all did not return an array");
  }
  const first = items[0];
  // sessionId is the only field present on EVERY entry. pid is absent on
  // stopped sessions; id/name/state are absent on interactive sessions — so
  // validating on those wrongly rejects legitimate shapes.
  if (
    items.length > 0 &&
    (typeof first !== "object" ||
      first === null ||
      typeof (first as Record<string, unknown>)["sessionId"] !== "string")
  ) {
    throw new Error("claude agents --json --all: unexpected element shape (missing sessionId)");
  }
  return items as SessionState[];
}

// Parse raw JSON output of `bd list --json`.
export function parseBdList(raw: string): WorkBead[] {
  const items: unknown = JSON.parse(raw);
  if (!Array.isArray(items)) {
    throw new Error("bd list --json did not return an array");
  }
  const first = items[0];
  if (
    items.length > 0 &&
    (typeof first !== "object" ||
      first === null ||
      typeof (first as Record<string, unknown>)["id"] !== "string" ||
      typeof (first as Record<string, unknown>)["title"] !== "string")
  ) {
    throw new Error("bd list --json: unexpected element shape (missing id or title)");
  }
  return items as WorkBead[];
}

export interface WorkstreamProjection {
  workstreams: InitiativeWorkstream[];
  workstreamDiagnostics: WorkstreamDiagnostic[];
}

type AncestryResult =
  | { kind: "descendant"; directId: string }
  | { kind: "cycle" }
  | { kind: "orphan" }
  | { kind: "other" };

function traceToEpic(
  bead: WorkBead,
  epicId: string,
  byId: ReadonlyMap<string, WorkBead>,
): AncestryResult {
  let current = bead;
  const seen = new Set<string>();
  while (true) {
    if (current.id === epicId) return { kind: "other" };
    if (seen.has(current.id)) return { kind: "cycle" };
    seen.add(current.id);
    const parentId = current.parent;
    if (!parentId) return { kind: "other" };
    if (parentId === epicId) return { kind: "descendant", directId: current.id };
    const parent = byId.get(parentId);
    if (!parent) return { kind: "orphan" };
    current = parent;
  }
}

function isClosedWorkBeadStatus(status: string): boolean {
  const normalized = (status ?? "").toLowerCase();
  return normalized === "closed" || normalized === "done";
}

function fallbackWorkstream(initiative: ParsedInitiative): InitiativeWorkstream {
  return {
    id: `initiative:${initiative.id}`,
    title: initiative.title,
    status: initiative.status,
    issueType: initiative.issue_type,
    priority: initiative.priority,
    labels: initiative.labels ?? [],
    progress: { total: 0, closed: 0 },
    memberIds: [],
    sourceOrder: 0,
    kind: "fallback",
  };
}

function buildProjectedWorkstreams(
  directIds: readonly string[],
  membersByDirect: ReadonlyMap<string, WorkBead[]>,
  sourceOrder: ReadonlyMap<string, number>,
  byId: ReadonlyMap<string, WorkBead>,
): InitiativeWorkstream[] {
  return directIds
    .map((id) => byId.get(id))
    .filter((bead): bead is WorkBead => bead !== undefined)
    .sort((a, b) => (sourceOrder.get(a.id) ?? 0) - (sourceOrder.get(b.id) ?? 0) || a.id.localeCompare(b.id))
    .map((direct) => {
      const orderedMembers = [...(membersByDirect.get(direct.id) ?? [direct])].sort(
        (a, b) => (sourceOrder.get(a.id) ?? 0) - (sourceOrder.get(b.id) ?? 0) || a.id.localeCompare(b.id),
      );
      const descendants = orderedMembers.filter((member) => member.id !== direct.id);
      return {
        id: direct.id,
        title: direct.title,
        status: direct.status,
        issueType: direct.issue_type,
        priority: direct.priority,
        labels: direct.labels ?? [],
        progress: {
          total: descendants.length,
          closed: descendants.filter((member) => isClosedWorkBeadStatus(member.status)).length,
        },
        memberIds: [direct.id, ...descendants.map((member) => member.id)],
        sourceOrder: sourceOrder.get(direct.id) ?? 0,
        kind: "workstream" as const,
      };
    });
}

function legacyLabelProjection(
  initiative: ParsedInitiative,
  beads: WorkBead[],
  byId: ReadonlyMap<string, WorkBead>,
  sourceOrder: ReadonlyMap<string, number>,
  addDiagnostic: (diagnostic: WorkstreamDiagnostic) => void,
): InitiativeWorkstream[] {
  const matched = beads.filter((bead) => bead.labels?.includes(initiative.id));
  if (matched.length === 0) return [];

  addDiagnostic({
    kind: "legacy-label",
    message: `Using exact ${initiative.id} labels because no usable epic ancestry was found.`,
  });

  const matchedById = new Map(matched.map((bead) => [bead.id, bead]));
  const matchedEpicIds = new Set(
    matched.filter((bead) => (bead.issue_type ?? "").toLowerCase() === "epic").map((bead) => bead.id),
  );
  const membersByDirect = new Map<string, WorkBead[]>();

  for (const bead of matched) {
    if (matchedEpicIds.has(bead.id)) continue;
    let current = bead;
    const seen = new Set<string>();
    let directId: string | null = null;
    while (directId === null) {
      if (seen.has(current.id)) {
        addDiagnostic({
          kind: "cycle",
          message: `Legacy label ancestry for ${bead.id} contains a parent cycle.`,
          beadId: bead.id,
        });
        break;
      }
      seen.add(current.id);
      const parentId = current.parent;
      if (!parentId || matchedEpicIds.has(parentId)) {
        directId = current.id;
        break;
      }
      const matchedParent = matchedById.get(parentId);
      if (!matchedParent) {
        if (!byId.has(parentId)) {
          addDiagnostic({
            kind: "orphan",
            message: `Legacy label ancestry for ${bead.id} references missing parent ${parentId}.`,
            beadId: bead.id,
          });
        }
        directId = current.id;
        break;
      }
      current = matchedParent;
    }
    if (directId === null) continue;
    const members = membersByDirect.get(directId) ?? [];
    members.push(bead);
    membersByDirect.set(directId, members);
  }

  return buildProjectedWorkstreams([...membersByDirect.keys()], membersByDirect, sourceOrder, byId);
}

// Project one initiative's workstream cards from a repo-wide Beads list. The
// caller performs one read per distinct repo; this helper stays pure so parent
// ancestry, legacy fallback, progress, and diagnostics are independently tested.
export function projectInitiativeWorkstreams(
  initiative: ParsedInitiative,
  beads: WorkBead[],
  initialDiagnostics: readonly WorkstreamDiagnostic[] = [],
): WorkstreamProjection {
  const diagnostics: WorkstreamDiagnostic[] = [...initialDiagnostics];
  const diagnosticKeys = new Set(
    diagnostics.map((diagnostic) => `${diagnostic.kind}\u0000${diagnostic.beadId ?? ""}\u0000${diagnostic.message}`),
  );
  const addDiagnostic = (diagnostic: WorkstreamDiagnostic): void => {
    const key = `${diagnostic.kind}\u0000${diagnostic.beadId ?? ""}\u0000${diagnostic.message}`;
    if (!diagnosticKeys.has(key)) {
      diagnosticKeys.add(key);
      diagnostics.push(diagnostic);
    }
  };
  const byId = new Map(beads.map((bead) => [bead.id, bead]));
  const sourceOrder = new Map(beads.map((bead, index) => [bead.id, index]));
  const associations = initiative.prWorkstreams ?? [];
  const mappedIds = new Set(associations.map((association) => association.workstream));
  const epicId = initiative.epic;
  let usableAncestry = epicId !== null && byId.has(epicId);
  const membersByDirect = new Map<string, WorkBead[]>();

  if (!initiative.repo.trim()) {
    addDiagnostic({ kind: "no-repo", message: "Initiative has no project repository." });
  }

  if (epicId !== null) {
    if (!byId.has(epicId)) {
      addDiagnostic({
        kind: "no-root",
        message: `Project epic ${epicId} was not present in the repository snapshot.`,
        beadId: epicId,
      });
    }
    for (const bead of beads) {
      if (bead.id === epicId) continue;
      const ancestry = traceToEpic(bead, epicId, byId);
      if (ancestry.kind === "descendant") {
        usableAncestry = true;
        const members = membersByDirect.get(ancestry.directId) ?? [];
        members.push(bead);
        membersByDirect.set(ancestry.directId, members);
        continue;
      }
      const relevant = bead.labels?.includes(initiative.id) || mappedIds.has(bead.id);
      if (relevant && (ancestry.kind === "cycle" || ancestry.kind === "orphan")) {
        addDiagnostic({
          kind: ancestry.kind,
          message:
            ancestry.kind === "cycle"
              ? `Bead ${bead.id} contains a parent cycle before reaching ${epicId}.`
              : `Bead ${bead.id} has a missing parent before reaching ${epicId}.`,
          beadId: bead.id,
        });
      }
    }
  }

  let workstreams: InitiativeWorkstream[];
  if (usableAncestry) {
    workstreams = buildProjectedWorkstreams(
      [...membersByDirect.keys()],
      membersByDirect,
      sourceOrder,
      byId,
    );
  } else {
    workstreams = legacyLabelProjection(initiative, beads, byId, sourceOrder, addDiagnostic);
  }

  const memberIds = new Set(workstreams.flatMap((workstream) => workstream.memberIds ?? [workstream.id]));
  for (const association of associations) {
    if (memberIds.has(association.workstream)) continue;
    const targetExists = byId.has(association.workstream);
    addDiagnostic({
      kind: targetExists ? "association-outside-initiative" : "association-missing",
      message: targetExists
        ? `PR association target ${association.workstream} is outside the initiative workstream ancestry.`
        : `PR association target ${association.workstream} is missing from the repository snapshot.`,
      beadId: association.workstream,
    });
  }

  if (workstreams.length === 0) workstreams = [fallbackWorkstream(initiative)];
  return { workstreams, workstreamDiagnostics: diagnostics };
}

// ---------------------------------------------------------------------------
// Two-dimension state model (agent-teams-3e6, extended by agent-teams-blo)
// ---------------------------------------------------------------------------

// DIMENSION A: derive delivery status from initiative.
// Uses a cheap notes/URL heuristic — no live gh call.
export function deriveDelivery(initiative: ParsedInitiative): DeliveryStatus {
  const s = initiative.status.toLowerCase();
  if (s === "closed" || s === "done") return "merged";
  if (initiative.prs.length > 0) return "pr-open";
  return "none";
}

// deriveSessionSignal (agent-teams-blo: distinguishes "waiting" from
// "working"/"ended") now lives in @agent-teams/shared — imported above and
// re-exported for existing consumers. See shared/types.ts for the doc.

// Roll up the Go-computed per-PR gate array (docs/multi-pr-contract.md §5)
// into the single ExplicitGateKind | null that deriveNeedsHuman/deriveActivity
// below still need. This is an EXISTENTIAL check across PRs ("does ANY PR
// carry this gate kind") — not a re-derivation of which label wins ON one PR;
// that precedence is Go's job now (already baked into each entry's own
// `gate` value by internal/verbs/status.go's gateForPR). The old
// deriveExplicitGate, which re-derived that per-PR precedence a third time
// here from raw labels, is deleted (agent-teams-ssib.10) — it also silently
// stopped matching anything the moment Track G's per-PR label grammar
// (`gate:review:<pr-url>`) replaced the bare `gate:review` label it scanned for.
//
// Order mirrors internal/verbs/status.go's computeExecutionStatus rollup
// (rule 1 checks "any gate:question" before rule 3's "any gate:review"), so
// the dashboard's aggregate signal can't disagree with the CLI's:
//   any "question" -> "question" (a live ask on ANY PR is as urgent as one on
//                     the only PR — matches Go's NEEDS-DECISION rule)
//   else any "review" -> "review"
//   else any "external" -> "external" (every review-worthy PR has been handed
//                     off; deriveNeedsHuman must still short-circuit this to
//                     false rather than falling into the check/generic
//                     session-based branches below)
//   else -> null (no PR carries any gate)
export function rollupGate(prReviews: PRReview[]): ExplicitGateKind | null {
  if (prReviews.some((r) => r.gate === "question")) return "question";
  if (prReviews.some((r) => r.gate === "review")) return "review";
  if (prReviews.some((r) => r.gate === "external")) return "external";
  return null;
}

// Derive needsHuman with flavor (agent-teams-0rl: explicit gate takes priority).
// Truth table:
//   merged + !worktreeExists + signal=working|waiting -> "reap" (zombie: worktree gone, session ALIVE)
//   merged                            -> false (done)
//   explicit gate == "review"         -> "review"  (AUTHORITATIVE; wins over session)
//   explicit gate == "question"       -> "waiting" (agent asking a question)
//   explicit gate == "external"       -> false (handed off: Eric declared he's done)
//   else session WAITING/blocked      -> "check"   (no declared gate; softer tier)
//   else session WORKING              -> false (refining — not in inbox)
//   else delivered + session ENDED    -> "generic" (needs input; NOT "review" anymore)
//   else delivered + session NONE     -> "generic" (graceful degrade; label "needs you")
//   else active + session ENDED/NONE  -> false (idle/dormant, no PR)
//
// KEY CHANGE (agent-teams-0rl): "review" flavor comes ONLY from explicit gate:review label.
// KEY CHANGE (agent-teams-ja9c): session-only waiting/blocked with NO gate -> "check" (soft tier).
// KEY CHANGE (agent-teams-d10b.2): reap row — zombie detection (merged, worktree gone, session alive).
// Gate checks come BEFORE signal check — gate:question wins over a blocked session.
export function deriveNeedsHuman(
  delivery: DeliveryStatus,
  signal: SessionSignal,
  gate: ExplicitGateKind | null,
  worktreeExists: boolean = true,
): false | NeedsHumanFlavor {
  if (delivery === "merged" && !worktreeExists && (signal === "working" || signal === "waiting")) return "reap";
  if (delivery === "merged") return false;
  // Explicit gate:review -> AUTHORITATIVE review signal (wins over everything).
  if (gate === "review") return "review";
  // Explicit gate:question (or legacy human-only) -> agent is waiting on your answer.
  if (gate === "question") return "waiting";
  // Handed off (external-review) -> Eric declared he is done looking; NOT his queue.
  // Sits with the other gate checks deliberately: a declaration about Eric's own
  // attention is authoritative over anything inferred from session or delivery
  // state. In particular it must pre-empt the "generic" branch below — a handed-off
  // initiative has an open PR and usually no live session, which is exactly the
  // shape that branch labels "needs you".
  if (gate === "external") return false;
  // Session waiting/blocked with NO gate -> soft "check" tier (not a declared ask).
  if (signal === "waiting") return "check";
  // Working session -> refining (not in inbox).
  if (signal === "working") return false;
  // No active working session — check delivery for PR state.
  // NOTE: delivered + ended was previously "review"; now demoted to "generic".
  if (delivery === "pr-open") {
    if (signal === "ended") return "generic";
    if (signal === "none") return "generic";
  }
  return false;
}

// Derive an ActivityStatus from initiative + session + explicit gate.
// This is the legacy flat enum kept for backward compatibility on the constellation
// view while it migrates to the two-dimension model.
// Priority: needs-human > delivered > busy > idle > done.
export function deriveActivity(
  initiative: ParsedInitiative,
  session: SessionState | null,
  gate: ExplicitGateKind | null,
): ActivityStatus {
  const delivery = deriveDelivery(initiative);
  const signal = deriveSessionSignal(session);
  const needsHuman = deriveNeedsHuman(delivery, signal, gate);

  if (needsHuman !== false) return "needs-human";

  if (delivery === "merged") return "done";

  if (delivery === "pr-open" && signal !== "working") return "delivered";

  if (signal === "working") return "busy";

  const s = initiative.status.toLowerCase();
  if (s === "closed" || s === "done") return "done";

  return "idle";
}

// Derive a human-readable phase token from the latest notes entry.
const PHASE_KEYWORDS: [RegExp, string][] = [
  [/delivered|awaiting.?merge|pr.open/i, "delivered"],
  [/needs.?human|parked|blocked|waiting/i, "parked"],
  [/execut|implement|build/i, "executing"],
  [/investigat|discover|research/i, "investigating"],
  [/plan|decompos|design/i, "planning"],
  [/done|closed|complete/i, "done"],
];

export function derivePhase(notes: string): string {
  // Latest entry is the last non-empty line of notes.
  // Guard against undefined/null passed in from call sites that haven't gone
  // through parseInitiative (e.g. direct test helpers or future callers).
  const latestEntry = (notes ?? "").split("\n").filter((l) => l.trim()).pop() ?? "";
  for (const [re, phase] of PHASE_KEYWORDS) {
    if (re.test(latestEntry)) return phase;
  }
  return "active";
}

// ---------------------------------------------------------------------------
// Row alert (agent-teams-rybk): unifies the client's former rowAlert (level) +
// alertInfo (reason/action) case trees — web/src/views/initiatives/index.tsx:73-130
// — into ONE function. Cases carry over verbatim; do NOT import from web/.
// ---------------------------------------------------------------------------

// Closed statuses for alert purposes — mirrors the client's CLOSED_STATUSES
// (web/src/views/initiatives/index.tsx:33), compared lowercased.
const ALERT_CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosedStatus(status: string): boolean {
  return ALERT_CLOSED_STATUSES.has(status.toLowerCase());
}

// Derive the row's alert (or null for a healthy/off-machine-open row). Ranked
// urgent > med > low; see web/src/views/initiatives/index.tsx:67-71 for the
// full urgency rationale. Six anomaly cases, verbatim from rowAlert + alertInfo:
//   1. sessionCount>1                          -> urgent, conflict
//   2. needsHuman==="reap"                     -> urgent, zombie
//   3. closed + session dead                   -> urgent, reap the lingering session
//   4. closed + session alive                  -> med, close the running session
//   5. open + no session + on this machine     -> urgent, stalled
//   6. open + session dead + on this machine   -> low, session died
// Healthy rows and off-machine-open rows (not locally actionable) return null.
export function deriveAlert(input: {
  sessionCount: number;
  needsHuman: false | NeedsHumanFlavor;
  session: SessionState | null;
  worktreeExists: boolean;
  status: string;
}): Alert | null {
  const { sessionCount, needsHuman, session, worktreeExists, status } = input;

  // Multiple session entries on one worktree is a conflict — wins over the rest.
  if (sessionCount > 1) {
    return {
      level: "urgent",
      reason: `${sessionCount} sessions are attached to this worktree — a conflict.`,
      action: "Stop the extras (claude stop) — only one session should run per worktree.",
    };
  }
  // Reap zombie wins over the generic closed+alive case — check it first.
  if (needsHuman === "reap") {
    return {
      level: "urgent",
      reason: "Closed and the worktree is gone, but a session is still running — a zombie.",
      action: "Stop the session (claude stop) to reap it.",
    };
  }

  const kind = sessionKind(session);
  const onMachine = worktreeExists;

  if (isClosedStatus(status)) {
    if (kind === "dead") {
      return {
        level: "urgent",
        reason: "Closed, but a finished session is still lingering in the agent list.",
        action: "Reap it (claude stop) so it clears out.",
      };
    }
    if (kind === "alive") {
      return {
        level: "med",
        reason: "Closed, but a session is still running on it.",
        action: "Close the session — the work is done.",
      };
    }
    return null; // completed
  }

  if (kind === "none" && onMachine) {
    return {
      level: "urgent",
      reason: "Open with a worktree on this machine, but nothing is running — stalled.",
      action: "Resume the session, or close the initiative if it's abandoned.",
    };
  }
  if (kind === "dead" && onMachine) {
    return {
      level: "low",
      reason: "The session has exited — it won't receive messages.",
      action: "Resume it, or close out the initiative.",
    };
  }
  return null; // healthy, or off-machine-open (not locally actionable)
}

// Join initiatives with sessions: session.cwd === initiative.worktree.
// humanGatedIds is the set of initiative IDs returned by `bd list --label human`
// (kept for resilience: used to supplement labels when labels array is absent).
//
// RESILIENCE: each initiative is processed independently.  If deriving state for
// one initiative throws (e.g. malformed data from a freshly-registered entry), that
// initiative degrades to a minimal safe node and a warning is logged.  The rest of
// the snapshot is unaffected — the dashboard stays live.
// existsFn checks whether a worktree path exists on the host. Injected so parse.ts
// stays pure (no fs import); snapshot.ts passes fs.existsSync. Defaults to a no-op
// that reports "not present" — keeps the many existing unit-test callers unchanged.
export function buildInitiativeNodes(
  initiatives: ParsedInitiative[],
  sessions: SessionState[],
  humanGatedIds: Set<string>,
  existsFn: (path: string) => boolean = () => false,
): InitiativeNode[] {
  return initiatives.map((initiative) => {
    // "On this machine" signal (at-gvv): empty/missing worktree path => false.
    const worktreeExists = initiative.worktree ? existsFn(initiative.worktree) : false;
    // All background session entries (alive + dead) matched to this worktree.
    // sessionCount drives the "multiple sessions on one worktree" alert; the
    // primary `session` prefers an alive entry (status present) so the chip
    // reflects a running session over a dead corpse when both exist.
    const matched = initiative.worktree
      ? sessions.filter((s) => s.kind === "background" && s.cwd === initiative.worktree)
      : [];
    const sessionCount = matched.length;
    const session = matched.find((s) => sessionKind(s) === "alive") ?? matched[0] ?? null;
    try {

      // Roll up the per-PR gate array first; fall back to humanGatedIds legacy path.
      let gate = rollupGate(initiative.prReviews);
      if (gate === null && humanGatedIds.has(initiative.id)) {
        // Legacy: bd list --label human with no labels array -> treat as question gate.
        // A handed-off initiative resolves to gate "external", never null, so this
        // fallback cannot resurrect its gate — which matters because it keeps the
        // "human" label and so is always in humanGatedIds.
        gate = "question";
      }

      const activity = deriveActivity(initiative, session, gate);
      const phase = derivePhase(initiative.notes);

      // Two-dimension state model fields (agent-teams-blo).
      const delivery = deriveDelivery(initiative);
      const signal = deriveSessionSignal(session);
      const needsHuman = deriveNeedsHuman(delivery, signal, gate, worktreeExists);
      const alert = deriveAlert({ sessionCount, needsHuman, session, worktreeExists, status: initiative.status });

      return { initiative, session, activity, phase, delivery, needsHuman, worktreeExists, sessionCount, alert };
    } catch (err) {
      console.warn(
        `[buildInitiativeNodes] skipping bad initiative ${initiative.id}: ${err instanceof Error ? err.message : String(err)}`,
      );
      // Minimal safe node: idle, no session, no PR, no needs-human, no alert.
      return {
        initiative,
        session: null,
        activity: "idle" as const,
        phase: "active",
        delivery: "none" as const,
        needsHuman: false as const,
        worktreeExists,
        sessionCount,
        alert: null,
      };
    }
  });
}

// Return background sessions whose cwd matched no initiative worktree.
// Interactive sessions are always excluded.
export function buildOrphanSessions(
  initiatives: ParsedInitiative[],
  sessions: SessionState[],
): SessionState[] {
  const worktreePaths = new Set(initiatives.map((i) => i.worktree).filter(Boolean));
  return sessions.filter(
    (s) => s.kind === "background" && !worktreePaths.has(s.cwd),
  );
}

// Parse a named field from the interior of a single ateam-ask block body.
// Returns the trimmed value string, or "" when the field is absent/empty.
function parseAskField(body: string, field: string): string {
  const prefix = `${field}:`;
  for (const line of body.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.startsWith(prefix)) {
      return trimmed.slice(prefix.length).trim();
    }
  }
  return "";
}

// Pure helper: scan notes for the LAST valid <<<ateam-ask ... >>> sentinel block
// and return {decision, recommendation, alternative, context} when found, null otherwise.
// Mirrors the Go implementation in internal/verbs/query.go (extractLatestAsk/parseAskBody).
//
// Grammar:
//   open:  literal "<<<ateam-ask"
//   close: ">>>" anchored at start of a line (or start of remaining text)
//   A block is valid only if the decision field is non-empty; unclosed blocks are skipped.
//   The LAST valid block wins.
export function extractLatestAsk(
  notes: string,
): { decision: string; recommendation: string; alternative: string; context: string } | null {
  const OPEN = "<<<ateam-ask";

  // Returns the index of the start of ">>>" that anchors to a line boundary,
  // or -1 when no closing sentinel is found.
  function closeLine(s: string): number {
    if (s.startsWith(">>>")) return 0;
    const idx = s.indexOf("\n>>>");
    if (idx === -1) return -1;
    return idx + 1; // position of the first ">" that starts ">>>"
  }

  let last: { decision: string; recommendation: string; alternative: string; context: string } | null = null;
  let remaining = notes;
  for (;;) {
    const start = remaining.indexOf(OPEN);
    if (start === -1) break;
    const after = remaining.slice(start + OPEN.length);
    const end = closeLine(after);
    if (end === -1) {
      // Unclosed block — skip, advance past the open sentinel.
      remaining = after;
      continue;
    }
    const body = after.slice(0, end);
    const decision = parseAskField(body, "decision");
    if (decision) {
      last = {
        decision,
        recommendation: parseAskField(body, "recommendation"),
        alternative: parseAskField(body, "alternative"),
        context: parseAskField(body, "context"),
      };
    }
    remaining = after.slice(end + ">>>".length);
  }
  return last;
}

// Pure helper: the last non-empty "notes block" from initiative.notes, for the
// check/generic/review nextAction fallback (agent-teams-ni2y.2). Splits via the
// shared splitNotesBlocks (agent-teams-rybk.5.3) — the same split index.ts uses
// to build DrillInDetail.notesHistory — so "last block" means the same thing
// everywhere in the dashboard. Skips blocks that carry a `<<<ateam-ask` sentinel
// (e.g. left behind by `ateam clear-gate` without `--file`) so its raw markup
// never leaks into nextAction.
// Returns "" when notes is empty/whitespace-only or every block is an ask sentinel.
function lastNotesBlock(notes: string): string {
  const blocks = splitNotesBlocks(notes).filter((b) => !b.includes("<<<ateam-ask"));
  return blocks.length > 0 ? (blocks[blocks.length - 1] ?? "") : "";
}

// Build InboxItem[] from already-built InitiativeNode[].
// An item is in the inbox iff node.needsHuman !== false OR node.alert !== null
// (agent-teams-rybk: any initiative with the 'i'-icon alert surfaces, even with
// no gate/session signal — e.g. a closed initiative with a still-running session).
//   needsHuman="reap"    -> zombie (at-asi): merged + worktree gone + alive session (stop to reap)
//   needsHuman="review"  -> ANY PR carries a "review" gate (agent-teams-ssib.10: one row PER
//                           such PR, read from pr_reviews — AUTHORITATIVE; "review the PR")
//   needsHuman="waiting" -> ANY PR carries a "question" gate, or legacy human-only (declared
//                           ask; may have ask block) — likewise one row per gated PR
//   needsHuman="generic" -> delivered + no explicit gate (graceful degrade)
//   needsHuman="check"   -> session waiting/blocked with NO gate (soft tier; check on it)
//   needsHuman=false + alert!=null -> kind="alert" (no declared gate/session signal, but anomalous)
// A gate row (waiting/review/check/generic) can ALSO carry a non-null alert — both the gate
// kind and the alert render (merge semantics); only "reap" suppresses the alert (dedup: the
// reap row's nextAction already states the identical zombie condition).
// Initiatives with needsHuman=false AND alert=null (working/refining/idle/done) are excluded.
//
// sessionTransitions: sessionId -> lastTransitionAt (epoch ms), from snapshot.ts's
// stampTransitions (agent-teams-ni2y.8). Undefined for ad-hoc/endpoint-fallback callers
// (before the first poll) -> lastActivityAt degrades to updated_at.
export function buildInbox(
  nodes: InitiativeNode[],
  sessionTransitions?: Map<string, number>,
): InboxItem[] {
  const items: InboxItem[] = [];

  for (const node of nodes) {
    if (node.needsHuman === false && node.alert === null) continue;

    const { initiative } = node;

    const onThisMachine = initiative.worktree !== "" && existsSync(initiative.worktree);
    const isClosed = isClosedStatus(initiative.status);
    // Reap dedup: the reap row's nextAction already states the identical zombie
    // condition as node.alert's reap branch, so we don't double-render it here.
    // node.alert on InitiativeNode stays populated for reap (the initiatives-table
    // popover still needs it) — only the InboxItem's alert is suppressed.
    const alertField = node.needsHuman === "reap" ? null : node.alert;

    // lastActivityAt = max(bead updated_at, matched session's last transition) — the
    // PRIMARY recency sort key (agent-teams-ni2y.8). node.session is the primary/alive
    // session (buildInitiativeNodes already prefers it); no match or no map -> updated_at.
    const transitionMs = node.session ? sessionTransitions?.get(node.session.sessionId) : undefined;
    const lastActivityAt =
      transitionMs === undefined
        ? initiative.updated_at
        : new Date(Math.max(Date.parse(initiative.updated_at), transitionMs)).toISOString();

    // Any matched entry with a valid short 8-hex id is attachable via `claude attach <id>`,
    // regardless of whether the session is alive (status present) or detached (status absent).
    const sessionId =
      typeof node.session?.id === "string" && isValidSessionId(node.session.id)
        ? node.session.id
        : undefined;

    // RAW session status/state/waitingFor — verbatim pass-through on every row,
    // never collapsed into `kind` (agent-teams-ni2y.2). waitingFor is tolerant:
    // absent on older `claude agents --json` builds, real once emitted.
    const status = node.session?.status ?? null;
    const state = node.session?.state ?? null;
    const waitingFor = node.session?.waitingFor;

    // Fallback next-action text for the boilerplate kinds (review/check/generic):
    // the last meaningful notes block, when notes carry one, else the row's
    // usual constant. "Meaningful" mirrors DrillInDetail.notesHistory's
    // session-block split — see lastNotesBlock above.
    const notesFallback = lastNotesBlock(initiative.notes).slice(0, 120);

    if (node.needsHuman === "reap") {
      // Zombie: merged initiative, worktree gone, but session is still alive.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "reap",
        nextAction: "Session still running after teardown — stop it to reap it.",
        recommendation: "",
        alternative: "",
        context: "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrls: initiative.prs,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    } else if (node.needsHuman === "review" || node.needsHuman === "waiting") {
      // Per-PR gate rows (agent-teams-ssib.10): one row per PR that carries
      // its OWN "review" or "question" gate, read directly off Go's
      // pr_reviews array (docs/multi-pr-contract.md §5) — never re-derived
      // from labels. A single-PR initiative has exactly one gated entry, so
      // this is 1:1 with the old single-row behavior; a multi-gated
      // initiative (the agent-teams-ssib.2 bug this whole initiative exists
      // to fix) now surfaces every gated PR instead of silently showing one.
      const gatedReviews = initiative.prReviews.filter(
        (r) => r.gate === "review" || r.gate === "question",
      );
      // Legacy fallback: humanGatedIds (buildInitiativeNodes) resolved a
      // "question" gate with no matching pr_reviews entry at all — e.g. an
      // old `ateam` build that doesn't emit `labels`/`pr_reviews`. Keep the
      // old single-row shape (all PRs in prUrls) so the initiative doesn't
      // silently vanish from the inbox instead.
      const reviewsForRow: Array<{ pr?: string; kind: "review" | "waiting" }> =
        gatedReviews.length > 0
          ? gatedReviews.map((r) => ({ pr: r.pr, kind: r.gate === "review" ? "review" : "waiting" }))
          : [{ kind: node.needsHuman }];

      for (const { pr, kind } of reviewsForRow) {
        const ask = kind === "waiting" ? extractLatestAsk(initiative.notes) : null;
        items.push({
          initiativeId: initiative.id,
          title: initiative.title,
          kind,
          nextAction:
            kind === "review"
              ? notesFallback || "Review the PR and merge or send it back."
              : ask
                ? ask.decision.slice(0, 120)
                : "Look at the session for more info.",
          recommendation: ask?.recommendation.slice(0, 120) ?? "",
          alternative: ask?.alternative.slice(0, 120) ?? "",
          context: ask?.context.slice(0, 280) ?? "",
          updatedAt: initiative.updated_at,
          lastActivityAt,
          worktree: initiative.worktree,
          prUrls: pr !== undefined ? [pr] : initiative.prs,
          onThisMachine,
          sessionId,
          status,
          state,
          waitingFor,
          alert: alertField,
          isClosed,
        });
      }
    } else if (node.needsHuman === "check") {
      // Session waiting/blocked with no explicit gate — soft "check on it" tier.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "check",
        nextAction: notesFallback || "Look at the session for more info.",
        recommendation: "",
        alternative: "",
        context: "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrls: initiative.prs,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    } else if (node.needsHuman === "generic") {
      // delivered + no explicit gate; graceful degrade.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "generic",
        nextAction:
          notesFallback || "Delivered with no gate — open the worktree to see what's needed.",
        recommendation: "",
        alternative: "",
        context: "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrls: initiative.prs,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    } else {
      // needsHuman === false && node.alert !== null (membership guarantee above):
      // no declared gate/session signal, but the row is anomalous — surface it.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "alert",
        nextAction: alertField!.action,
        recommendation: "",
        alternative: "",
        context: "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrls: initiative.prs,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    }
  }

  return items;
}
