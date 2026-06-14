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
  pid: number;
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

// The join of a ParsedInitiative with its matched SessionState (null = no live session).
export interface InitiativeNode {
  initiative: ParsedInitiative;
  session: SessionState | null;
  activity: ActivityStatus;
  // Human-readable phase token, e.g. "executing", "planning", "parked".
  phase: string;
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
