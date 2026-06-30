import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { attachToInitiative, launchSession } from "../../lib/api.js";
import "./initiatives.css";

// Persist a boolean toggle to localStorage (no server). Reads on init, writes on
// change. localStorage access is wrapped so a blocked/unavailable store degrades
// to in-memory state rather than throwing.
function usePersistedBool(key: string, initial: boolean): [boolean, (v: boolean) => void] {
  const [value, setValue] = useState<boolean>(() => {
    try {
      const raw = localStorage.getItem(key);
      return raw === null ? initial : raw === "true";
    } catch {
      return initial;
    }
  });
  const set = (v: boolean) => {
    setValue(v);
    try {
      localStorage.setItem(key, String(v));
    } catch {
      /* storage unavailable — keep in-memory state */
    }
  };
  return [value, set];
}

// Closed states — status comes from the registry as free TEXT, compare lowercased.
const CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosed(node: InitiativeNode): boolean {
  return CLOSED_STATUSES.has(node.initiative.status.toLowerCase());
}

// Session "kind" — the only session distinction that matters (see truth table):
//   "alive" = a matched background session whose process is still running
//             (status present: busy/idle/waiting — pid alive).
//   "dead"  = a matched entry whose process has exited (status null/absent;
//             lingers in `claude agents --all` history). Won't receive messages.
//   "none"  = no matched session entry at all.
function sessionKind(node: InitiativeNode): "alive" | "dead" | "none" {
  const s = node.session;
  if (s === null) return "none";
  return s.status != null ? "alive" : "dead";
}

// "Completed" = closed AND the session is completely gone (no entry at all).
// A closed initiative with ANY lingering session (alive or dead) is NOT
// completed — it stays visible with a row alert until the session is reaped.
function isCompleted(node: InitiativeNode): boolean {
  return isClosed(node) && sessionKind(node) === "none";
}

// Returns the short 8-hex session id if the session carries a valid attachable id,
// undefined otherwise. A valid id means `claude attach <id>` should work regardless
// of whether the session is alive (status present) or detached (status absent).
// Reserve Launch only for when there is NO matched entry at all.
function sessionAttachId(session: SessionState | null | undefined): string | undefined {
  const id = session?.id;
  return typeof id === "string" && /^[0-9a-f]{8}$/.test(id) ? id : undefined;
}

// Row alert — an anomaly where action should be taken, ranked by urgency.
// null = no alert. Urgency (most→least): urgent {open+none+on-machine (stalled),
// closed+dead (reap it)} > med {closed+alive (close it)} > low {open+dead+
// on-machine (session died)}. Off-machine open cases aren't locally actionable,
// so they don't alert.
type AlertLevel = "urgent" | "med" | "low";
function rowAlert(node: InitiativeNode): AlertLevel | null {
  // Multiple session entries on one worktree is a conflict — wins over the rest.
  if (node.sessionCount > 1) return "urgent";
  const kind = sessionKind(node);
  const onMachine = node.worktreeExists;
  if (isClosed(node)) {
    if (kind === "dead") return "urgent"; // #7 reap the lingering session
    if (kind === "alive") return "med"; //  #6 close the running session
    return null; //                         #8 completed
  }
  if (kind === "none" && onMachine) return "urgent"; // #4 stalled — nothing running
  if (kind === "dead" && onMachine) return "low"; //    #2 session died, won't get messages
  return null; //                                       #1 healthy, #3/#5 off-machine
}

// Why the row is alerted + what to do about it — surfaced via the row's info
// popover. Returns null for non-alerted rows (mirrors rowAlert's cases).
function alertInfo(node: InitiativeNode): { reason: string; action: string } | null {
  if (node.sessionCount > 1)
    return {
      reason: `${node.sessionCount} sessions are attached to this worktree — a conflict.`,
      action: "Stop the extras (claude stop) — only one session should run per worktree.",
    };
  const kind = sessionKind(node);
  const onMachine = node.worktreeExists;
  if (isClosed(node)) {
    if (kind === "dead")
      return {
        reason: "Closed, but a finished session is still lingering in the agent list.",
        action: "Reap it (claude stop) so it clears out.",
      };
    if (kind === "alive")
      return {
        reason: "Closed, but a session is still running on it.",
        action: "Close the session — the work is done.",
      };
    return null;
  }
  if (kind === "none" && onMachine)
    return {
      reason: "Open with a worktree on this machine, but nothing is running — stalled.",
      action: "Resume the session, or close the initiative if it's abandoned.",
    };
  if (kind === "dead" && onMachine)
    return {
      reason: "The session has exited — it won't receive messages.",
      action: "Resume it, or close out the initiative.",
    };
  return null;
}

// Session chip presentation per the truth table: glyph by liveness
// (● alive · ◐ dead · ○ none), color by health (good=healthy live, warn=
// problematic and actionable, muted=dead-but-not-actionable, off=none).
function sessionChip(node: InitiativeNode): { glyph: string; level: ChipLevel; value: string } {
  const kind = sessionKind(node);
  if (kind === "none") return { glyph: "○", level: "off", value: "none" };
  if (kind === "alive") {
    return isClosed(node)
      ? { glyph: "●", level: "warn", value: "running (close it)" }
      : { glyph: "●", level: "good", value: "running" };
  }
  // dead — amber when actionable (closed, or open+on-machine), else muted grey.
  const actionable = isClosed(node) || node.worktreeExists;
  return { glyph: "◐", level: actionable ? "warn" : "muted", value: "dead" };
}

// Phase token hue is keyed by phase so categories read at a glance: delivered
// (shipped), parked (needs attention), and done (complete) each get their own
// treatment; the in-progress phases keep the base accent. Normalized so the
// free-text phase maps to a stable selector (see initiatives.css).
function phaseClass(phase: string): string {
  return `init-row__phase--${phase.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
}

// Per-signal hue so the three chips are distinguishable when lit:
// machine=blue, pr=violet, session=green (see initiatives.css).
type ChipTone = "machine" | "pr" | "session";

// Chip intensity. machine/PR use on|off; session uses good|warn|muted|off
// (see sessionChip + initiatives.css).
type ChipLevel = "on" | "good" | "warn" | "muted" | "off";

interface SignalChipProps {
  level: ChipLevel;
  tone: ChipTone;
  icon: string;
  label: string;
  value: string; // aria value, e.g. "yes" | "no" | "live" | "dormant" | "none"
  title: string;
}

function SignalChip({ level, tone, icon, label, value, title }: SignalChipProps) {
  return (
    <span
      className={`init-chip init-chip--${level} init-chip--${tone}`}
      title={title}
      aria-label={`${label}: ${value}`}
    >
      <span className="init-chip__icon" aria-hidden="true">{icon}</span>
      <span className="init-chip__label">{label}</span>
    </span>
  );
}

function RowAttachButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
  const [state, setState] = useState<"idle" | "pending" | "ok" | "err">("idle");

  async function handleClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.stopPropagation();
    if (state === "pending") return;
    setState("pending");
    try {
      await attachToInitiative(initiativeId, sessionId);
      setState("ok");
      setTimeout(() => setState("idle"), 1500);
    } catch {
      setState("err");
      setTimeout(() => setState("idle"), 3000);
    }
  }

  return (
    <span className="init-row__attach">
      <button
        className="attach-btn attach-btn--row"
        onClick={(e) => { void handleClick(e); }}
        disabled={state === "pending"}
        title="attach"
        aria-label="Attach to session"
      >
        {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "attach"}
      </button>
    </span>
  );
}

type LaunchState = "idle" | "pending" | "ok" | "err";

function LaunchButton({ initiativeId }: { initiativeId: string }) {
  const [state, setState] = useState<LaunchState>("idle");
  const [errMsg, setErrMsg] = useState<string>("");

  const launch = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (state === "pending") return;
    setState("pending");
    setErrMsg("");
    try {
      await launchSession(initiativeId);
      setState("ok");
      setTimeout(() => setState("idle"), 3000);
    } catch (err) {
      setErrMsg(err instanceof Error ? err.message : String(err));
      setState("err");
      setTimeout(() => { setState("idle"); setErrMsg(""); }, 5000);
    }
  };

  const label = state === "idle" ? "launch" : state === "pending" ? "…" : state === "ok" ? "✓" : "✗";
  // In error state, set the title to the full error so it's inspectable on hover.
  const title =
    state === "err" && errMsg ? errMsg : "Launch a new DRI session for this initiative";
  // First line of error message — brief inline hint so the failure is legible at a glance.
  const errFirst = errMsg.split("\n")[0] ?? "";

  return (
    <div className="init-row__launch">
      <button
        className={`launch-btn${state === "err" ? " launch-btn--err" : ""}`}
        onClick={(e) => { void launch(e); }}
        title={title}
      >
        {label}
      </button>
      {state === "err" && errFirst && (
        <span className="launch-btn__err-msg">{errFirst}</span>
      )}
    </div>
  );
}

function InitiativeRow({ node }: { node: InitiativeNode }) {
  const navigate = useNavigate();
  const { initiative } = node;

  const onMachine = node.worktreeExists;
  const hasPr = node.delivery === "pr-open";
  const sess = sessionChip(node);
  const alert = rowAlert(node);
  const info = alertInfo(node);
  const attachId = sessionAttachId(node.session);

  function handleRowClick() {
    navigate(`/initiative/${initiative.id}`);
  }

  function handlePrLinkClick(e: React.MouseEvent<HTMLAnchorElement>) {
    // Don't let the PR link bubble up to the row's drill-in navigation.
    e.stopPropagation();
  }

  // status and/or state can be absent in the agent data — join only what's
  // present so the tooltip never renders "undefined".
  const sessionDetail = node.session
    ? [node.session.status, node.session.state].filter(Boolean).join(" / ")
    : "";
  const suffix = sessionDetail ? ` (${sessionDetail})` : "";
  const kind = sessionKind(node);
  const sessionTitle =
    kind === "none"
      ? "No session"
      : kind === "alive"
        ? isClosed(node)
          ? `Session still running on a closed initiative — close it${suffix}`
          : `Live session${suffix}`
        : `Dead session — process exited, won't receive messages${suffix}`;

  return (
    <div
      className="init-row"
      data-initiative-id={initiative.id}
      data-closed={isClosed(node) ? "true" : "false"}
      data-alert={alert ?? undefined}
      onClick={handleRowClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleRowClick(); }}
      aria-label={`Open initiative: ${initiative.title}`}
    >
      <div className="init-row__main">
        <span className="init-row__title">{initiative.title}</span>
        <span className="init-row__id">{initiative.id}</span>
        <span className={`init-row__phase ${phaseClass(node.phase)}`}>{node.phase}</span>
      </div>
      <div className="init-row__signals">
        <SignalChip
          level={onMachine ? "on" : "off"}
          tone="machine"
          icon="▣"
          label="on machine"
          value={onMachine ? "yes" : "no"}
          title={onMachine ? "Worktree exists on this machine" : "Worktree not on this machine"}
        />
        {hasPr && initiative.prUrl ? (
          <a
            href={initiative.prUrl}
            target="_blank"
            rel="noreferrer"
            className="init-chip init-chip--on init-chip--pr init-chip--link"
            onClick={handlePrLinkClick}
            title={`Open PR: ${initiative.prUrl}`}
            aria-label="open PR: yes"
          >
            <span className="init-chip__icon" aria-hidden="true">⎘</span>
            <span className="init-chip__label">PR ↗</span>
          </a>
        ) : (
          <SignalChip
            level={hasPr ? "on" : "off"}
            tone="pr"
            icon="⎘"
            label="PR"
            value={hasPr ? "yes" : "no"}
            title={hasPr ? "Has an open PR" : "No open PR"}
          />
        )}
        <SignalChip
          level={sess.level}
          tone="session"
          icon={sess.glyph}
          label="session"
          value={sess.value}
          title={sessionTitle}
        />
      </div>
      {attachId ? (
        <RowAttachButton initiativeId={initiative.id} sessionId={attachId} />
      ) : node.worktreeExists && !isClosed(node) ? (
        <LaunchButton initiativeId={initiative.id} />
      ) : null}
      {/* Always-present fixed-width slot so the signals column holds the same
          horizontal position on every row. Icon + popover render only when the
          row is actually alerted, so non-alerted rows expose no tooltip. */}
      <div className="init-row__info" data-tier={alert ?? undefined}>
        {info && (
          <>
            <span className="init-row__info-icon" aria-hidden="true">i</span>
            <span className="init-row__info-pop" role="tooltip">
              <span className="init-row__info-why"><strong>Why:</strong> {info.reason}</span>
              <span className="init-row__info-do"><strong>Do:</strong> {info.action}</span>
            </span>
          </>
        )}
      </div>
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="initiatives-empty">
      <p className="initiatives-empty__message">{message}</p>
    </div>
  );
}

function DisconnectedBanner({ connectionState, error }: { connectionState: string; error: string | null }) {
  const isError = connectionState === "error";
  return (
    <div className={`initiatives-banner initiatives-banner--${isError ? "error" : "warn"}`}>
      {isError
        ? `Connection error${error ? `: ${error}` : ""}`
        : "Reconnecting to agent stream…"}
    </div>
  );
}

export default function InitiativesView() {
  const { initiatives, connectionState, error } = useSnapshotContext();
  const [query, setQuery] = useState("");
  const [showCompleted, setShowCompleted] = usePersistedBool("initiatives.showCompleted", false);
  const [onlyOnMachine, setOnlyOnMachine] = usePersistedBool("initiatives.onlyOnMachine", false);

  const q = query.trim().toLowerCase();
  const filtered = initiatives.filter((node) => {
    if (!showCompleted && isCompleted(node)) return false;
    if (onlyOnMachine && !node.worktreeExists) return false;
    if (q === "") return true;
    const { id, title } = node.initiative;
    return id.toLowerCase().includes(q) || title.toLowerCase().includes(q);
  });

  const showBanner = connectionState !== "connected";
  const emptyMessage =
    q !== "" ? "No initiatives match your search." : "No initiatives to show.";

  return (
    <div className="initiatives-view">
      <header className="initiatives-header">
        <h1 className="initiatives-header__title">Initiatives</h1>
        <span className="initiatives-header__count" data-testid="initiatives-count">
          {filtered.length}
        </span>
      </header>

      {showBanner && (
        <DisconnectedBanner connectionState={connectionState} error={error} />
      )}

      <div className="initiatives-controls">
        <input
          type="search"
          className="initiatives-search"
          placeholder="Search initiatives…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Search initiatives by id or title"
        />
        <label className="initiatives-toggle">
          <input
            type="checkbox"
            checked={onlyOnMachine}
            onChange={(e) => setOnlyOnMachine(e.target.checked)}
          />
          On this machine
        </label>
        <label className="initiatives-toggle">
          <input
            type="checkbox"
            checked={showCompleted}
            onChange={(e) => setShowCompleted(e.target.checked)}
          />
          Show completed
        </label>
      </div>

      {filtered.length === 0 ? (
        <EmptyState message={emptyMessage} />
      ) : (
        <ul className="initiatives-list" aria-label="Initiatives">
          {filtered.map((node) => (
            <li key={node.initiative.id} className="initiatives-list__item">
              <InitiativeRow node={node} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
