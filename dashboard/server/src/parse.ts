// Pure parsing functions over raw CLI output.
// These are the riskiest logic — they are unit-tested against real fixtures.

import type {
  RawInitiative,
  ParsedInitiative,
  SessionState,
  SessionSignal,
  ActivityStatus,
  DeliveryStatus,
  NeedsHumanFlavor,
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
  const fields = parseDescriptionFields(raw.description);

  // Extract the problem line — the very first line of description often
  // starts with "problem: ...".
  const problem = (fields["problem"] ?? "").trim();

  // PR URL may appear in notes (later entries) or description.
  const prUrl = extractPrUrl(raw.notes) ?? extractPrUrl(raw.description);

  return {
    ...raw,
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

// Derive needsHuman with flavor from the two-dimension model (agent-teams-blo).
// Truth table:
//   session WAITING/blocked           -> "waiting" (active OR delivered; MOST URGENT)
//   session WORKING                   -> false (refining — not in inbox)
//   delivered + session ENDED         -> "review" (verify & merge; signal-backed)
//   delivered + session NONE          -> "generic" (graceful degrade; label "needs you")
//   active + session ENDED or NONE    -> false (idle/dormant, no PR)
//   done initiative                   -> false
//   explicit human gate               -> "waiting" (override; wins over inference)
//
// CRITICAL: specificity follows the signal. Only assert "review" when we have
// evidence (ENDED). Without session info -> "generic" (no action asserted).
export function deriveNeedsHuman(
  delivery: DeliveryStatus,
  signal: SessionSignal,
  isHumanGated: boolean,
): false | NeedsHumanFlavor {
  if (delivery === "merged") return false;
  // Session waiting/blocked -> most urgent, regardless of delivery state.
  if (signal === "waiting") return "waiting";
  // Explicit human gate wins over inference -> "waiting" flavor.
  if (isHumanGated) return "waiting";
  // Working session -> refining (not in inbox).
  if (signal === "working") return false;
  // No active working session — check delivery for PR state.
  if (delivery === "pr-open") {
    // Session ended = we know it self-stopped after delivering.
    if (signal === "ended") return "review";
    // No session at all = graceful degrade.
    if (signal === "none") return "generic";
  }
  return false;
}

// Derive an ActivityStatus from initiative + session + human-gate flag.
// This is the legacy flat enum kept for backward compatibility on the constellation
// view while it migrates to the two-dimension model.
// Priority: needs-human > delivered > busy > idle > done.
export function deriveActivity(
  initiative: ParsedInitiative,
  session: SessionState | null,
  isHumanGated: boolean,
): ActivityStatus {
  const delivery = deriveDelivery(initiative);
  const signal = deriveSessionSignal(session);
  const needsHuman = deriveNeedsHuman(delivery, signal, isHumanGated);

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
  const latestEntry = notes.split("\n").filter((l) => l.trim()).pop() ?? "";
  for (const [re, phase] of PHASE_KEYWORDS) {
    if (re.test(latestEntry)) return phase;
  }
  return "active";
}

// Join initiatives with sessions: session.cwd === initiative.worktree.
// humanGatedIds is the set of initiative IDs returned by `bd list --label human`.
export function buildInitiativeNodes(
  initiatives: ParsedInitiative[],
  sessions: SessionState[],
  humanGatedIds: Set<string>,
): InitiativeNode[] {
  return initiatives.map((initiative) => {
    const session =
      sessions.find(
        (s) => s.kind === "background" && s.cwd === initiative.worktree,
      ) ?? null;

    const isHumanGated = humanGatedIds.has(initiative.id);
    const activity = deriveActivity(initiative, session, isHumanGated);
    const phase = derivePhase(initiative.notes);

    // Two-dimension state model fields (agent-teams-blo).
    const delivery = deriveDelivery(initiative);
    const signal = deriveSessionSignal(session);
    const needsHuman = deriveNeedsHuman(delivery, signal, isHumanGated);

    return { initiative, session, activity, phase, delivery, needsHuman };
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

// Build InboxItem[] from already-built InitiativeNode[].
// An item is in the inbox iff node.needsHuman !== false.
//   needsHuman="waiting" -> session blocked/waiting or explicit gate (kind="waiting")
//   needsHuman="review"  -> PR open, session ended (kind="review")
//   needsHuman="generic" -> PR open, no session (kind="generic"; graceful degrade)
// Initiatives with needsHuman=false (working/refining/idle/done) are excluded.
export function buildInbox(nodes: InitiativeNode[]): InboxItem[] {
  const items: InboxItem[] = [];

  for (const node of nodes) {
    if (node.needsHuman === false) continue;

    const { initiative } = node;

    if (node.needsHuman === "waiting") {
      // Agent waiting on human input, or explicit human gate.
      // Question = latest non-empty notes line (may contain the gate question).
      const question =
        initiative.notes
          .split("\n")
          .filter((l) => l.trim())
          .pop() ?? "(no question recorded)";

      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "waiting",
        question,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
      });
    } else if (node.needsHuman === "review") {
      // Delivered + session ended: verify & merge.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "review",
        question: `PR awaiting review: ${initiative.prUrl ?? "(no PR URL)"}`,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
      });
    } else {
      // needsHuman === "generic": delivered + no session; graceful degrade.
      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "generic",
        question: "Needs your attention",
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
      });
    }
  }

  return items;
}
