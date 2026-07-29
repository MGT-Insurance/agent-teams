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

// GitHub PR URL pattern — matches https://github.com/<owner>/<repo>/pull/<n>
const PR_URL_RE = /https:\/\/github\.com\/[^\s/]+\/[^\s/]+\/pull\/\d+/;

export function extractPrUrl(text: string): string | null {
  const m = PR_URL_RE.exec(text);
  return m ? (m[0] ?? null) : null;
}

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

  // PR URL may appear in notes (later entries) or description. This is a URL
  // hunt over freeform text, not a routing-field read — internal/initiative's
  // scope boundary leaves it here deliberately.
  const prUrl = extractPrUrl(notes) ?? extractPrUrl(description);

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
    prUrl,
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

// ---------------------------------------------------------------------------
// Two-dimension state model (agent-teams-3e6, extended by agent-teams-blo)
// ---------------------------------------------------------------------------

// DIMENSION A: derive delivery status from initiative.
// Uses a cheap notes/URL heuristic — no live gh call.
export function deriveDelivery(initiative: ParsedInitiative): DeliveryStatus {
  const s = initiative.status.toLowerCase();
  if (s === "closed" || s === "done") return "merged";
  if (initiative.prUrl !== null) return "pr-open";
  return "none";
}

// deriveSessionSignal (agent-teams-blo: distinguishes "waiting" from
// "working"/"ended") now lives in @agent-teams/shared — imported above and
// re-exported for existing consumers. See shared/types.ts for the doc.

// Derive the explicit gate kind from an initiative's labels array.
// Resilient: tolerates undefined/null/empty labels (missing or unset).
//   "gate:review"   => "review"   (AUTHORITATIVE: initiative is awaiting PR review)
//   "gate:question" => "question" (agent is parked asking a question)
//   "human" (no gate:*) => "question" (legacy/plain gate; treat as question)
//   else none
export function deriveExplicitGate(labels: string[] | undefined): ExplicitGateKind | null {
  if (!labels || labels.length === 0) return null;
  if (labels.includes("gate:review")) return "review";
  if (labels.includes("gate:question")) return "question";
  // Plain "human" with no gate:* label = legacy gate, treat as question.
  if (labels.includes("human")) return "question";
  return null;
}

// Derive needsHuman with flavor (agent-teams-0rl: explicit gate takes priority).
// Truth table:
//   merged + !worktreeExists + signal=working|waiting -> "reap" (zombie: worktree gone, session ALIVE)
//   merged                            -> false (done)
//   explicit gate == "review"         -> "review"  (AUTHORITATIVE; wins over session)
//   explicit gate == "question"       -> "waiting" (agent asking a question)
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

      // Derive explicit gate from labels first; fall back to humanGatedIds legacy path.
      // labels is optional/missing on older entries — deriveExplicitGate handles that safely.
      let gate = deriveExplicitGate(initiative.labels);
      if (gate === null && humanGatedIds.has(initiative.id)) {
        // Legacy: bd list --label human with no labels array -> treat as question gate.
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
//   needsHuman="review"  -> explicit gate:review label (AUTHORITATIVE; "review the PR")
//   needsHuman="waiting" -> explicit gate:question/human (declared ask; may have ask block)
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
        prUrl: initiative.prUrl,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    } else if (node.needsHuman === "review") {
      // Explicit gate:review — AUTHORITATIVE "review the PR" signal.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "review",
        nextAction: notesFallback || "Review the PR and merge or send it back.",
        recommendation: "",
        alternative: "",
        context: "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
    } else if (node.needsHuman === "waiting") {
      // Agent waiting on human input: explicit gate:question/human (declared ask).
      // nextAction = decision from the latest ask block, or constant fallback.
      const ask = extractLatestAsk(initiative.notes);
      const nextAction = ask
        ? ask.decision.slice(0, 120)
        : "Look at the session for more info.";

      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "waiting",
        nextAction,
        recommendation: ask?.recommendation.slice(0, 120) ?? "",
        alternative: ask?.alternative.slice(0, 120) ?? "",
        context: ask?.context.slice(0, 280) ?? "",
        updatedAt: initiative.updated_at,
        lastActivityAt,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
        sessionId,
        status,
        state,
        waitingFor,
        alert: alertField,
        isClosed,
      });
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
        prUrl: initiative.prUrl,
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
        prUrl: initiative.prUrl,
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
        prUrl: initiative.prUrl,
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
