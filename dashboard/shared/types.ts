// Raw JSON shape returned by `ateam list-json`.
// CRITICAL: there is NO `labels` field in this output (verified).
// Structured fields (repo, worktree, branch, team, mode, goal) are embedded
// as `key: value` TEXT lines inside `description` — backend must parse them.
export interface RawInitiative {
  id: string;
  title: string;
  description: string;
  notes: string;
  status: string;
  priority: string;
  issue_type: string;
  owner: string;
  created_at: string;
  updated_at: string;
}

// RawInitiative plus fields parsed out of description text, and a derived PR URL.
export interface ParsedInitiative extends RawInitiative {
  problem: string;
  repo: string;
  worktree: string;
  branch: string;
  team: string;
  mode: string;
  goal: string;
  prUrl: string | null;
}

// Shape of one element from `claude agents --json --all`.
// Background sessions have id/name/state; interactive sessions do not.
export interface SessionState {
  pid?: number; // absent on stopped sessions
  cwd: string;
  kind: "background" | "interactive";
  startedAt: number; // epoch ms
  sessionId: string; // uuid
  status: "idle" | "busy";
  // background-only fields
  id?: string;
  name?: string;
  state?: "working" | "blocked" | "done" | "stopped";
}

// Derived activity enum for constellation rendering.
// Heuristics: needs-human > delivered/PR-open > executing > investigating > planning > parked.
export type ActivityStatus =
  | "busy"
  | "idle"
  | "needs-human"
  | "delivered"
  | "done";

// ---------------------------------------------------------------------------
// Two-dimension initiative state model (added 2026-06-15, agent-teams-3e6)
// ---------------------------------------------------------------------------

// DIMENSION A: delivery — about the PR, independent of activity.
//   none     -> no PR URL, or initiative already merged/done
//   pr-open  -> PR URL present in notes AND initiative status is OPEN
//   merged   -> initiative status is CLOSED / DONE
export type DeliveryStatus = "none" | "pr-open" | "merged";

// DIMENSION B captured on InitiativeNode as a boolean-ish field — see below.
// "working" = a live background session is busy/working in the initiative's worktree.
// "idle"    = no live working session.
// (Kept as a separate field; the existing ActivityStatus.activity covers this too.)

// DERIVED: needsHuman — the action-required flag with a flavor.
//   "answer" -> initiative is parked on a gate/question (the `human` label / ateam human-list)
//   "review" -> delivery == pr-open AND no live working session (PR awaiting Eric's review/merge)
//   false    -> no action required
//
// KEY principle: awaiting-merge is NOT flagged as a human gate in agent-teams.
// "review" MUST be derived structurally (open + PR + idle), not read from a flag.
export type NeedsHumanFlavor = "answer" | "review";

// TRUTH TABLE:
//   delivery=none  + working -> needsHuman=false            (initial work)
//   delivery=none  + idle + gate -> needsHuman="answer"     (initial work, blocked)
//   delivery=pr-open + working -> needsHuman=false          (refining after delivery)
//   delivery=pr-open + idle    -> needsHuman="review"       (PR delivered, awaiting review)
//   delivery=merged            -> needsHuman=false, done

// The join of a ParsedInitiative with its matched SessionState (null = no live session).
export interface InitiativeNode {
  initiative: ParsedInitiative;
  session: SessionState | null;
  activity: ActivityStatus;
  // Human-readable phase token, e.g. "executing", "planning", "parked".
  phase: string;
  // Two-dimension state model fields (agent-teams-3e6).
  delivery: DeliveryStatus;
  needsHuman: false | NeedsHumanFlavor;
}

// An item in the inbox requiring Eric's attention.
export interface InboxItem {
  initiativeId: string;
  title: string;
  kind: "human-gate" | "pr-awaiting-merge";
  // The parked question text, parsed from the latest notes entry.
  question: string;
  worktree: string;
  prUrl: string | null;
}

// A work bead from `bd list --json` scoped to an initiative's project repo.
export interface WorkBead {
  id: string;
  title: string;
  status: string;
  priority: string;
  issue_type: string;
}

// Full drill-in payload for a single initiative.
export interface DrillInDetail extends ParsedInitiative {
  notesHistory: string[];
  sessions: SessionState[];
  workBeads: WorkBead[];
}

// The SSE payload shape pushed on each tick and returned by GET /api/snapshot.
export interface SnapshotEvent {
  initiatives: InitiativeNode[];
  // Background claude sessions that matched no registered initiative worktree.
  // Interactive sessions are excluded — these are only unregistered background processes.
  unmatchedSessions: SessionState[];
  inbox: InboxItem[];
  ts: number; // epoch ms
}
