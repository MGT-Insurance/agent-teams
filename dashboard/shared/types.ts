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
  // Root epic bead id in the project repo (e.g. "agent-teams-x6ce").
  // Absent for legacy initiatives registered before at-e3m. Dashboard uses
  // this to filter the drill-in work-bead list to just this initiative's subtree.
  epic: string | null;
}

// Shape of one element from `claude agents --json --all`.
// Background sessions have id/name/state; interactive sessions do not.
export interface SessionState {
  pid?: number; // absent on stopped sessions
  cwd: string;
  kind: "background" | "interactive";
  startedAt: number; // epoch ms
  sessionId: string; // uuid
  // Present ONLY while the process is alive (absent/null once it exits — per the
  // Claude Code agent-view docs). "waiting" is added by agent-teams-blo: the
  // session is paused, waiting on human input.
  status?: "idle" | "busy" | "waiting";
  // background-only fields
  id?: string;
  name?: string;
  // Per Claude Code agent-view docs: working | blocked | done | failed | stopped.
  // `failed` = the session errored. Absent on interactive sessions.
  state?: "working" | "blocked" | "done" | "failed" | "stopped";
  // Boundary pass-through: emitted by newer `claude agents --json` builds on a
  // session blocked on a permission prompt (observed value "permissionPrompt").
  // Absent in older builds — tolerate missing, render verbatim when present.
  waitingFor?: string;
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

// ---------------------------------------------------------------------------
// Session taxonomy (agent-teams-rybk.5.2): the ONLY place in the dashboard
// that reads a SessionState's raw status/state fields. sessionKind() and
// deriveSessionSignal() below are pure derivations of readSession()'s
// snapshot — no other call site should re-check status!=null,
// status==="waiting", state==="blocked", etc.
//
// The two taxonomies are ORTHOGONAL, not nested: sessionKind asks "is the
// process still alive", deriveSessionSignal asks "is the agent working,
// waiting, or done". A session can be alive+ended (status="idle", no
// working/blocked state) — a live process where the agent isn't actively
// working or blocked. The only invariant that holds across every input is
// the null boundary: sessionKind==="none" iff deriveSessionSignal==="none"
// iff session===null (see server/src/parse.test.ts's consistency suite).
// ---------------------------------------------------------------------------

function readSession(session: SessionState | null): {
  present: boolean;
  alive: boolean;
  waiting: boolean;
  working: boolean;
} {
  if (session === null) {
    return { present: false, alive: false, waiting: false, working: false };
  }
  return {
    present: true,
    alive: session.status != null,
    waiting: session.status === "waiting" || session.state === "blocked",
    working: session.status === "busy" || session.state === "working",
  };
}

// Session "kind" — the process-liveness axis:
//   "alive" = matched session whose process is still running (status present).
//   "dead"  = matched entry whose process has exited (status absent/null) —
//             lingers in `claude agents --all` history. Won't receive messages.
//   "none"  = no matched session at all.
// Single source for what were two verbatim-duplicated implementations: the
// client's per-row session chip (web/src/views/initiatives/index.tsx) and the
// server's deriveAlert (server/src/parse.ts, formerly its own alertSessionKind).
export type SessionKind = "alive" | "dead" | "none";

export function sessionKind(session: SessionState | null): SessionKind {
  const r = readSession(session);
  if (!r.present) return "none";
  return r.alive ? "alive" : "dead";
}

// Derive the session signal from a matched SessionState (or null) — the
// agent-work-state axis (agent-teams-blo): distinguishes "waiting" (blocked/
// paused on human) from "working" and "ended".
//   "working" -> status=busy / state=working (live, active)
//   "waiting" -> status=waiting / state=blocked (agent paused on human input) — checked
//                first, so a session that is somehow both waiting and working reports waiting.
//   "ended"   -> status=idle / state=done|stopped (session self-stopped), OR any other
//                alive-but-not-working/waiting shape (see the orthogonality note above).
//   "none"    -> no matched session.
export function deriveSessionSignal(session: SessionState | null): SessionSignal {
  const r = readSession(session);
  if (!r.present) return "none";
  if (r.waiting) return "waiting";
  if (r.working) return "working";
  return "ended";
}

// DERIVED: needsHuman — the action-required flag with a flavor (agent-teams-blo, updated agent-teams-0rl, agent-teams-ja9c).
//   "waiting" -> explicit gate:question or human-only label (agent asking a question). AUTHORITATIVE.
//   "review"  -> EXPLICIT gate:review label (AUTHORITATIVE — "review the PR").
//                Wins over session signal; comes only from the gate label.
//   "check"   -> session signal only (status=waiting OR state=blocked) with NO explicit gate.
//                Softer tier — the session may be paused, but there is no declared gate.
//                Sorted BELOW review/waiting/generic in the inbox.
//   "generic" -> fallback: delivered + session ENDED or NONE (no explicit gate).
//                "needs you" — graceful degrade; no specific action asserted.
//   false     -> no action required
//
// KEY principles (updated agent-teams-0rl, agent-teams-ja9c):
// - "review" comes ONLY from explicit gate:review label — NOT inferred from session signal.
// - Explicit gate:review wins over session signal (even working session).
// - Explicit gate:question or human-only -> "waiting" (agent asking a question).
// - Session waiting/blocked with NO gate -> "check" (softer, not a declared ask).
// - Without a gate, delivered + ended/none -> "generic" (needs input; NOT review).
// - "reap" (at-asi): zombie — a closed/merged initiative whose worktree is GONE
//   (!worktreeExists) but a session is still alive (matched by the cwd snapshot).
//   Fires DESPITE the merged short-circuit; the human reaps it via `claude stop`.
export type NeedsHumanFlavor = "waiting" | "review" | "generic" | "check" | "reap";

// TRUTH TABLE (agent-teams-0rl, updated agent-teams-ja9c):
//   merged                            -> needsHuman=false (done, nothing needed)
//   explicit gate:review              -> needsHuman="review" (AUTHORITATIVE; wins over session)
//   explicit gate:question or human   -> needsHuman="waiting" (agent asking a question)
//   else session WAITING/blocked      -> needsHuman="check" (no declared gate; softer tier)
//   else session WORKING              -> needsHuman=false (working / refining, not in inbox)
//   else delivered + session ENDED    -> needsHuman="generic" (needs input; NOT review anymore)
//   else delivered + session NONE     -> needsHuman="generic" (graceful degrade; label "needs you")
//   else active + session ENDED/NONE  -> needsHuman=false (idle/dormant, no PR)
//   done initiative                   -> needsHuman=false

// An anomaly on an initiative row where action should be taken (agent-teams-rybk).
// Derived server-side by deriveAlert() — unifies the client's former rowAlert
// (level) + alertInfo (reason/action) case trees into one shape. null = healthy
// row / off-machine-open (not locally actionable) — no alert.
export interface Alert {
  level: "urgent" | "med" | "low";
  reason: string;
  action: string;
}

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
  // "On this machine" signal (at-gvv): true iff the initiative's worktree path
  // exists on the host running the dashboard server. Worktree paths are
  // host-specific while the registry syncs cross-machine, so this MUST be
  // computed server-side via fs.existsSync — it cannot be derived client-side.
  worktreeExists: boolean;
  // Number of background session ENTRIES matched to this worktree (alive + dead,
  // as listed by `claude agents --json --all`). >1 means multiple sessions on
  // one worktree — a conflict the dashboard flags. `session` above is the chosen
  // primary (prefers an alive one) for the per-row session chip.
  sessionCount: number;
  // Row anomaly (agent-teams-rybk): non-null iff this row needs the 'i'-icon
  // alert (conflict, zombie, stalled, etc.) — independent of needsHuman. A row
  // can have needsHuman=false and alert!=null (e.g. a closed+alive session).
  alert: Alert | null;
}

// An item in the inbox requiring Eric's attention.
// kind mirrors NeedsHumanFlavor (agent-teams-0rl, agent-teams-ja9c), plus "alert":
//   "waiting" -> explicit gate:question/human (agent waiting on input, declared ask)
//   "review"  -> explicit gate:review label (AUTHORITATIVE; "review the PR")
//   "generic" -> delivered + no explicit gate (graceful degrade; label "needs you")
//   "check"   -> session waiting/blocked with NO explicit gate (softer tier; check on it)
//                Sorted BELOW review/waiting/generic rows.
//   "reap"    -> zombie (at-asi): closed/merged initiative whose worktree is gone but a
//                session is still alive. Stop it via `claude stop`. Top of inbox.
//   "alert"   -> (agent-teams-rybk) needsHuman=false but node.alert!=null (e.g. a closed
//                initiative with a still-running session). Any initiative with the 'i'-icon
//                alert now surfaces in the inbox even when no gate/session signal fires.
export interface InboxItem {
  initiativeId: string;
  title: string;
  kind: "waiting" | "review" | "generic" | "check" | "reap" | "alert";
  // The one-sentence action for Eric right now.
  //   review  -> "Review the PR and merge or send it back." (prUrl rendered separately)
  //   waiting -> decision field from the latest <<<ateam-ask >>> sentinel block in notes,
  //              or "Look at the session for more info." when no structured ask block exists.
  //   generic -> "Delivered with no gate — open the worktree to see what's needed."
  //   check   -> "Look at the session for more info." (soft check — no declared ask block)
  nextAction: string;
  // Agent's recommendation and alternative for waiting rows; "" for all other kinds.
  // Sourced from the recommendation:/alternative: fields in the latest <<<ateam-ask >>> block.
  recommendation: string;
  alternative: string;
  // Free-form "what it's waiting on" prose from the context: field in the same ask block
  // (mirrors Go's askBlock.context, internal/verbs/query.go parseAskBody). "" when absent
  // or for non-waiting kinds. This is the declared-ask waiting-reason; waitingFor below
  // is a separate, live-session permission-prompt signal — the two coexist.
  context: string;
  // ISO-8601 timestamp from RawInitiative.updated_at — the literal bead timestamp,
  // kept for display + relative-time (agent-teams-ni2y, agent-teams-ni2y.6).
  updatedAt: string;
  // ISO-8601 timestamp: max(updatedAt, matched session's last status/state transition
  // stamped server-side) — the PRIMARY recency sort key for the inbox (agent-teams-ni2y.8).
  // Falls back to updatedAt when no session matched, or when built without a transition
  // map (ad-hoc/endpoint fallback before the first poll).
  lastActivityAt: string;
  worktree: string;
  prUrl: string | null;
  // true when initiative.worktree is non-empty and exists on the local filesystem.
  // Derived server-side (dashboard server runs locally); used for the "This machine only" toggle.
  onThisMachine: boolean;
  // Short 8-hex claude session id from any matched entry (alive or detached).
  // A valid id means `claude attach <id>` should work regardless of session liveness.
  // Absent when no matched session entry carries a valid 8-hex id.
  sessionId?: string;
  // RAW node.session.status / node.session.state, passed through VERBATIM — never
  // collapsed into `kind` above. null when node.session is null (no matched session).
  // status=="waiting" and state=="blocked" are the high-visibility drivers: rows where
  // either holds should read as urgent regardless of `kind`.
  status: string | null;
  state: string | null;
  // Raw session waitingFor pass-through (see SessionState.waitingFor) — the live-session
  // permission-prompt reason. Absent when the session doesn't carry one (common today;
  // newer `claude agents --json` builds emit it). Render verbatim when present.
  waitingFor?: string;
  // node.alert pass-through (agent-teams-rybk) — the 'i'-icon anomaly, independent of
  // `kind`. A gate row (waiting/review/check/generic) can ALSO carry a non-null alert;
  // both render (merge semantics). Reap dedup: always null when kind === "reap" — the
  // reap row's nextAction already states the identical zombie condition, so the alert
  // isn't double-rendered here (node.alert on InitiativeNode stays populated for reap).
  alert: Alert | null;
  // True iff initiative.status is "closed" or "done" (case-insensitive). Lets the inbox's
  // row-action seam (selectRowAction, web/src/lib) pick stop-vs-launch without importing
  // server-only status logic.
  isClosed: boolean;
}

// A work bead from `bd list --json` scoped to an initiative's project repo.
export interface WorkBead {
  id: string;
  title: string;
  status: string;
  priority: string;
  issue_type: string;
  parent?: string;      // set on child beads; value is the parent epic's id
  labels?: string[];    // initiative-id label lives here
}

// Full drill-in payload for a single initiative.
export interface DrillInDetail extends ParsedInitiative {
  notesHistory: string[];
  sessions: SessionState[];
  workBeads: WorkBead[];
}

// The SSE payload shape pushed on each tick and returned by GET /api/snapshot.
export interface SnapshotEvent {
  // May include CLOSED/DONE initiatives in addition to open ones (at-gvv): the
  // Initiatives tab offers a "show closed" toggle and filters client-side.
  // Closed initiatives derive delivery="merged" -> needsHuman=false, so the
  // inbox (which keys off needsHuman) is unaffected.
  initiatives: InitiativeNode[];
  // Background claude sessions that matched no registered initiative worktree.
  // Interactive sessions are excluded — these are only unregistered background processes.
  unmatchedSessions: SessionState[];
  inbox: InboxItem[];
  ts: number; // epoch ms
}
