// Pure parsing functions over raw CLI output.
// These are the riskiest logic — they are unit-tested against real fixtures.

import type {
  RawInitiative,
  ParsedInitiative,
  SessionState,
  ActivityStatus,
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
  if (
    items.length > 0 &&
    (typeof first !== "object" ||
      first === null ||
      typeof (first as Record<string, unknown>)["pid"] !== "number" ||
      typeof (first as Record<string, unknown>)["sessionId"] !== "string")
  ) {
    throw new Error("claude agents --json --all: unexpected element shape (missing pid or sessionId)");
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

// Derive an ActivityStatus from initiative + session + human-gate flag.
// Priority: needs-human > delivered > busy > idle > done.
export function deriveActivity(
  initiative: ParsedInitiative,
  session: SessionState | null,
  isHumanGated: boolean,
): ActivityStatus {
  if (isHumanGated) return "needs-human";

  if (initiative.prUrl !== null && session?.state === "done") {
    return "delivered";
  }

  if (session !== null) {
    if (session.status === "busy" || session.state === "working") return "busy";
    return "idle";
  }

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

    return { initiative, session, activity, phase };
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

// Build InboxItem[] from initiatives + human-gated ids.
// Includes human-gate items and PR-awaiting-merge items.
export function buildInbox(
  initiatives: ParsedInitiative[],
  humanGatedIds: Set<string>,
): InboxItem[] {
  const items: InboxItem[] = [];

  for (const initiative of initiatives) {
    const s = initiative.status.toLowerCase();
    if (humanGatedIds.has(initiative.id)) {
      if (s === "closed" || s === "done") continue;

      // Parked question = latest non-empty notes line.
      const question =
        initiative.notes
          .split("\n")
          .filter((l) => l.trim())
          .pop() ?? "(no question recorded)";

      items.push({
        initiativeId: initiative.id,
        title: initiative.title,
        kind: "human-gate",
        question,
        worktree: initiative.worktree,
        prUrl: initiative.prUrl,
      });
      continue;
    }

    // PR-awaiting-merge: has a PR URL, not closed/done, not already human-gated.
    if (initiative.prUrl !== null) {
      if (s !== "closed" && s !== "done") {
        items.push({
          initiativeId: initiative.id,
          title: initiative.title,
          kind: "pr-awaiting-merge",
          question: `PR awaiting merge: ${initiative.prUrl}`,
          worktree: initiative.worktree,
          prUrl: initiative.prUrl,
        });
      }
    }
  }

  return items;
}
