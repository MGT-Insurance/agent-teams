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
} from "@agent-teams/shared";

// GitHub PR URL pattern — matches https://github.com/<owner>/<repo>/pull/<n>
const PR_URL_RE = /https:\/\/github\.com\/[^\s/]+\/[^\s/]+\/pull\/\d+/;

export function extractPrUrl(text: string): string | null {
  const m = PR_URL_RE.exec(text);
  return m ? (m[0] ?? null) : null;
}

// Parse the `key: value` lines embedded in description text.
// Returns a partial record; missing keys are empty string.
function parseDescriptionFields(
  desc: string,
): Record<string, string> {
  const result: Record<string, string> = {};
  for (const line of desc.split("\n")) {
    const colon = line.indexOf(":");
    if (colon === -1) continue;
    const key = line.slice(0, colon).trim().toLowerCase();
    const value = line.slice(colon + 1).trim();
    if (key && value) {
      result[key] = value;
    }
  }
  return result;
}

// Parse a RawInitiative into a ParsedInitiative by extracting structured
// fields from the description text and finding the first PR URL in
// notes + description.
export function parseInitiative(raw: RawInitiative): ParsedInitiative {
  // notes and description are typed as string but the registry can emit undefined
  // for freshly-created initiatives that have no NOTES section yet.  Coerce to ""
  // here so every downstream function receives a guaranteed string.
  const notes = raw.notes ?? "";
  const description = raw.description ?? "";

  const fields = parseDescriptionFields(description);

  // Extract the problem line — the very first line of description often
  // starts with "problem: ...".
  const problem = (fields["problem"] ?? "").trim();

  // PR URL may appear in notes (later entries) or description.
  const prUrl = extractPrUrl(notes) ?? extractPrUrl(description);

  return {
    ...raw,
    // Normalise notes/description so downstream code always has real strings.
    notes,
    description,
    problem,
    repo: fields["repo"] ?? "",
    worktree: fields["worktree"] ?? "",
    branch: fields["branch"] ?? "",
    team: fields["team"] ?? "",
    mode: fields["mode"] ?? "",
    goal: fields["goal"] ?? "",
    prUrl,
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

// Derive the session signal from a matched SessionState (or null).
// This is the key extension from agent-teams-blo: we now distinguish
// "waiting" (blocked/paused on human) from "working" and "ended".
//   "working" -> status=busy / state=working (live, active)
//   "waiting" -> status=waiting / state=blocked (agent paused on human input) — THE BUG FIX
//   "ended"   -> status=idle / state=done|stopped (session self-stopped)
//   "none"    -> no matched session
export function deriveSessionSignal(session: SessionState | null): SessionSignal {
  if (session === null) return "none";
  // Blocked state: agent is waiting on human input. Matches both:
  //   status="waiting" (newer API) and state="blocked" (older API).
  if (session.status === "waiting" || session.state === "blocked") return "waiting";
  // Working: actively running.
  if (session.status === "busy" || session.state === "working") return "working";
  // Ended: session self-stopped or completed.
  return "ended";
}

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
// Gate checks come BEFORE signal check — gate:question wins over a blocked session.
export function deriveNeedsHuman(
  delivery: DeliveryStatus,
  signal: SessionSignal,
  gate: ExplicitGateKind | null,
): false | NeedsHumanFlavor {
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
    const session = matched.find((s) => s.status != null) ?? matched[0] ?? null;
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
      const needsHuman = deriveNeedsHuman(delivery, signal, gate);

      return { initiative, session, activity, phase, delivery, needsHuman, worktreeExists, sessionCount };
    } catch (err) {
      console.warn(
        `[buildInitiativeNodes] skipping bad initiative ${initiative.id}: ${err instanceof Error ? err.message : String(err)}`,
      );
      // Minimal safe node: idle, no session, no PR, no needs-human.
      return {
        initiative,
        session: null,
        activity: "idle" as const,
        phase: "active",
        delivery: "none" as const,
        needsHuman: false as const,
        worktreeExists,
        sessionCount,
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
// and return {decision, recommendation, alternative} when found, null otherwise.
// Mirrors the Go implementation in internal/verbs/query.go (extractLatestAsk/parseAskBody).
//
// Grammar:
//   open:  literal "<<<ateam-ask"
//   close: ">>>" anchored at start of a line (or start of remaining text)
//   A block is valid only if the decision field is non-empty; unclosed blocks are skipped.
//   The LAST valid block wins.
export function extractLatestAsk(notes: string): { decision: string; recommendation: string; alternative: string } | null {
  const OPEN = "<<<ateam-ask";

  // Returns the index of the start of ">>>" that anchors to a line boundary,
  // or -1 when no closing sentinel is found.
  function closeLine(s: string): number {
    if (s.startsWith(">>>")) return 0;
    const idx = s.indexOf("\n>>>");
    if (idx === -1) return -1;
    return idx + 1; // position of the first ">" that starts ">>>"
  }

  let last: { decision: string; recommendation: string; alternative: string } | null = null;
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
      };
    }
    remaining = after.slice(end + ">>>".length);
  }
  return last;
}

// Build InboxItem[] from already-built InitiativeNode[].
// An item is in the inbox iff node.needsHuman !== false.
//   needsHuman="review"  -> explicit gate:review label (AUTHORITATIVE; "review the PR")
//   needsHuman="waiting" -> explicit gate:question/human (declared ask; may have ask block)
//   needsHuman="generic" -> delivered + no explicit gate (graceful degrade)
//   needsHuman="check"   -> session waiting/blocked with NO gate (soft tier; check on it)
// Initiatives with needsHuman=false (working/refining/idle/done) are excluded.
export function buildInbox(nodes: InitiativeNode[]): InboxItem[] {
  const items: InboxItem[] = [];

  for (const node of nodes) {
    if (node.needsHuman === false) continue;

    const { initiative } = node;

    const onThisMachine = initiative.worktree !== "" && existsSync(initiative.worktree);

    if (node.needsHuman === "review") {
      // Explicit gate:review — AUTHORITATIVE "review the PR" signal.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "review",
        nextAction: "Review the PR and merge or send it back.",
        recommendation: "",
        alternative: "",
        updatedAt: initiative.updated_at,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
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
        updatedAt: initiative.updated_at,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
      });
    } else if (node.needsHuman === "check") {
      // Session waiting/blocked with no explicit gate — soft "check on it" tier.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "check",
        nextAction: "Look at the session for more info.",
        recommendation: "",
        alternative: "",
        updatedAt: initiative.updated_at,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
      });
    } else {
      // needsHuman === "generic": delivered + no explicit gate; graceful degrade.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "generic",
        nextAction: "Delivered with no gate — open the worktree to see what's needed.",
        recommendation: "",
        alternative: "",
        updatedAt: initiative.updated_at,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
        onThisMachine,
      });
    }
  }

  return items;
}
