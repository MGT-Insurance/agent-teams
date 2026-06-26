// Raw JSON shape returned by `ateam list-json`.
// `labels` is an optional array of label strings (e.g. "gate:review", "gate:question", "human").
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
  // Optional: present when the ateam framework has set a gate (PR #14+).
  // Tolerate missing or empty — older registries do not emit this field.
  labels?: string[];
}

// Explicit gate kind derived from labels:
//   "review"   -> "gate:review" label present  (AUTHORITATIVE review signal)
//   "question" -> "gate:question" or "human"-only label (agent asking a question)
//   none       -> no gate label present
export type ExplicitGateKind = "review" | "question";

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
  // "waiting" is added by agent-teams-blo: the session is paused, waiting on human input.
  status: "idle" | "busy" | "waiting";
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

// SESSION SIGNAL — derived from the matched SessionState (if any):
//   "working" -> status=busy / state=working (live session, active)
//   "waiting" -> status=waiting / state=blocked (agent paused, waiting on human)
//   "ended"   -> status=idle / state=done|stopped (session self-stopped)
//   "none"    -> no matched session found
export type SessionSignal = "working" | "waiting" | "ended" | "none";

// DERIVED: needsHuman — the action-required flag with a flavor (agent-teams-blo, updated agent-teams-0rl).
//   "waiting" -> session is blocked/waiting (agent paused on human input) OR explicit question gate.
//                MOST URGENT — works for active OR delivered.
//   "review"  -> EXPLICIT gate:review label (AUTHORITATIVE — "review the PR").
//                Wins over session signal; comes only from the gate label.
//   "generic" -> fallback: delivered + session ENDED or NONE (no explicit gate).
//                "needs you" — graceful degrade; no specific action asserted.
//   false     -> no action required
//
// KEY principles (updated agent-teams-0rl):
// - "review" comes ONLY from explicit gate:review label — NOT inferred from session signal.
// - Explicit gate:review wins over session signal (even working session).
// - Explicit gate:question or human-only -> "waiting" (agent asking a question).
// - Without a gate, delivered + ended/none -> "generic" (needs input; NOT review).
export type NeedsHumanFlavor = "waiting" | "review" | "generic";

// TRUTH TABLE (agent-teams-0rl):
//   merged                            -> needsHuman=false (done, nothing needed)
//   explicit gate:review              -> needsHuman="review" (AUTHORITATIVE; wins over session)
//   explicit gate:question or human   -> needsHuman="waiting" (agent asking a question)
//   else session WAITING/blocked      -> needsHuman="waiting" (active OR delivered; most urgent)
//   else session WORKING              -> needsHuman=false (working / refining, not in inbox)
//   else delivered + session ENDED    -> needsHuman="generic" (needs input; NOT review anymore)
//   else delivered + session NONE     -> needsHuman="generic" (graceful degrade; label "needs you")
//   else active + session ENDED/NONE  -> needsHuman=false (idle/dormant, no PR)
//   done initiative                   -> needsHuman=false

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
// kind mirrors NeedsHumanFlavor (agent-teams-0rl):
//   "waiting" -> session blocked/waiting or explicit gate:question/human (agent waiting on input)
//   "review"  -> explicit gate:review label (AUTHORITATIVE; "review the PR")
//   "generic" -> delivered + no explicit gate (graceful degrade; label "needs you")
export interface InboxItem {
  initiativeId: string;
  title: string;
  kind: "waiting" | "review" | "generic";
  // The one-sentence action for Eric right now.
  //   review  -> "Review the PR and merge or send it back." (prUrl rendered separately)
  //   waiting -> decision field from the latest <<<ateam-ask >>> sentinel block in notes,
  //              or "Look at the session for more info." when no structured ask block exists.
  //   generic -> "Delivered with no gate — open the worktree to see what's needed."
  nextAction: string;
  // ISO-8601 timestamp from RawInitiative.updated_at — drives recency sort in the inbox.
  updatedAt: string;
  worktree: string;
  prUrl: string | null;
  // true when initiative.worktree is non-empty and exists on the local filesystem.
  // Derived server-side (dashboard server runs locally); used for the "This machine only" toggle.
  onThisMachine: boolean;
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
