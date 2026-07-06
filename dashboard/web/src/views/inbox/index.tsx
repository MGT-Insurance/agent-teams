import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { InboxItem } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { RowActions } from "../../components/RowActions.js";
import "./inbox.css";

// Row label per flavor — matches the spec's specificity-follows-signal principle.
// reap:    zombie — closed initiative, worktree gone, session still alive. Stop it.
// review:  AUTHORITATIVE gate:review label; "review the PR".
// waiting: explicit gate:question/human; agent declared a blocking question.
// generic: delivered + no explicit gate; graceful degrade, no specific action asserted.
// check:   session waiting/blocked but no declared gate; softer "check on it" tier.
// alert:   (agent-teams-rybk) needsHuman=false but node.alert!=null — the 'i'-icon
//          anomaly surfaces here even with no gate/session signal.
function rowBadgeLabel(kind: InboxItem["kind"]): string {
  if (kind === "reap") return "reap";
  if (kind === "review") return "review the PR";
  if (kind === "waiting") return "agent waiting";
  if (kind === "check") return "check on it";
  if (kind === "alert") return "needs attention";
  return "needs you";
}

interface InboxRowProps {
  item: InboxItem;
  // Action slot: left intentionally empty for v1, shaped for future triage layer.
  actionSlot?: React.ReactNode;
}

function InboxRow({ item, actionSlot }: InboxRowProps) {
  const navigate = useNavigate();

  function handleRowClick() {
    navigate(`/initiative/${item.initiativeId}`);
  }

  function handlePrLinkClick(e: React.MouseEvent<HTMLAnchorElement>) {
    // Stop propagation so the row click (navigate to drill-in) doesn't also fire.
    e.stopPropagation();
  }

  // High-visibility trigger — keyed off the RAW session fields, independent of `kind`.
  // A row is loud when the agent declared itself waiting or its session is blocked,
  // regardless of which flavor badge it happens to carry.
  const isLoud = item.status === "waiting" || item.state === "blocked";

  return (
    <div
      className={`inbox-row inbox-row--${item.kind}${isLoud ? " inbox-row--loud" : ""}`}
      data-kind={item.kind}
      data-initiative-id={item.initiativeId}
      onClick={handleRowClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleRowClick(); }}
      aria-label={`Open initiative: ${item.title}`}
    >
      <div className="inbox-row__header">
        <span className={`inbox-row__badge inbox-row__badge--${item.kind}`}>
          {rowBadgeLabel(item.kind)}
        </span>
        {/* Raw status/state readout — always on, independent of the derived kind badge above. */}
        <span className={`inbox-row__status-chip${item.status === "waiting" ? " inbox-row__status-chip--loud" : ""}`}>
          status: {item.status ?? "—"}
        </span>
        <span className={`inbox-row__state-chip${item.state === "blocked" ? " inbox-row__state-chip--loud" : ""}`}>
          state: {item.state ?? "—"}
        </span>
        <span className="inbox-row__title">{item.title}</span>
        {/* PR link whenever a URL is present — delivery is orthogonal to flavor. */}
        {item.prUrl && (
          <a
            href={item.prUrl}
            target="_blank"
            rel="noreferrer"
            className="inbox-row__pr-link"
            onClick={handlePrLinkClick}
          >
            view PR ↗
          </a>
        )}
        {actionSlot && (
          <div className="inbox-row__action-slot">{actionSlot}</div>
        )}
      </div>
      <p className="inbox-row__next-action">{item.nextAction}</p>
      {/* Live-session permission-prompt reason — kind-independent, tolerant pass-through. */}
      {item.waitingFor && (
        <p className="inbox-row__secondary inbox-row__secondary--waiting-for">
          <span className="inbox-row__secondary-label">Waiting on:</span> {item.waitingFor}
        </p>
      )}
      {item.kind === "waiting" && (item.context || item.recommendation || item.alternative) && (
        <div className="inbox-row__suggestion">
          {item.context && (
            <p className="inbox-row__secondary inbox-row__secondary--context">
              <span className="inbox-row__secondary-label">Context:</span> {item.context}
            </p>
          )}
          {item.recommendation && (
            <p className="inbox-row__secondary inbox-row__secondary--recommendation">
              <span className="inbox-row__secondary-label">Recommended:</span> {item.recommendation}
            </p>
          )}
          {item.alternative && (
            <p className="inbox-row__secondary inbox-row__secondary--alternative">
              <span className="inbox-row__secondary-label">Alternative:</span> {item.alternative}
            </p>
          )}
        </div>
      )}
      {/* "i" affordance for the alert Why/Do detail (agent-teams-rybk data) — mirrors
          the Initiatives view's hover-revealed info icon so alerted inbox rows stay
          compact instead of always showing the Why/Do text inline. Reachable via
          keyboard focus (Tab), not hover-only — the icon is a real focusable element. */}
      {item.alert && (
        <div className="inbox-row__info" data-tier={item.alert.level}>
          <span
            className="inbox-row__info-icon"
            tabIndex={0}
            aria-label={`Why: ${item.alert.reason} Do: ${item.alert.action}`}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
          >
            i
          </span>
          <span className="inbox-row__info-pop" role="tooltip">
            <p className="inbox-row__secondary inbox-row__secondary--alert-why">
              <span className="inbox-row__secondary-label">Why:</span> {item.alert.reason}
            </p>
            <p className="inbox-row__secondary inbox-row__secondary--alert-do">
              <span className="inbox-row__secondary-label">Do:</span> {item.alert.action}
            </p>
          </span>
        </div>
      )}
    </div>
  );
}

function EmptyState({ message = "Nothing needs you right now." }: { message?: string }) {
  return (
    <div className="inbox-empty">
      <span className="inbox-empty__icon">✓</span>
      <p className="inbox-empty__message">{message}</p>
    </div>
  );
}

function DisconnectedBanner({ connectionState, error }: { connectionState: string; error: string | null }) {
  const isError = connectionState === "error";
  return (
    <div className={`inbox-banner inbox-banner--${isError ? "error" : "warn"}`}>
      {isError
        ? `Connection error${error ? `: ${error}` : ""}`
        : "Reconnecting to agent stream…"}
    </div>
  );
}

export default function InboxView() {
  const { inbox, connectionState, error } = useSnapshotContext();
  const [thisMachineOnly, setThisMachineOnly] = useState(true);

  // Filter BEFORE sort (spec: filter then sort).
  // Bypass on item.sessionId (not just kind==="reap"): sessionId is only ever set when
  // buildInbox found a real LOCAL claude-agents match (parse.ts), so "has a sessionId" is
  // the general "something on this machine to act on" signal. This subsumes reap (reap rows
  // always have a sessionId) and also correctly surfaces alert rows with onThisMachine:false
  // but a lingering local session (agent-teams-rybk.7) that the old reap-only check missed.
  const filtered = thisMachineOnly ? inbox.filter((item) => item.onThisMachine || item.sessionId != null) : inbox;

  // Recency-only sort: lastActivityAt desc is the PRIMARY key across ALL kinds (agent-teams-ni2y,
  // agent-teams-ni2y.8). lastActivityAt = max(bead updatedAt, session transition), so a session
  // flipping status/state (e.g. busy->waiting) rises even without a bead edit. No kind tiering —
  // a fresh waiting row outranks a stale review row. Waiting/blocked rows are made loud instead
  // of pinned (ni2y.4); reap stays in the same recency ordering too.
  const sorted = [...filtered].sort((a, b) => b.lastActivityAt.localeCompare(a.lastActivityAt));

  const showBanner = connectionState !== "connected";
  const totalCount = sorted.length;
  // true when the toggle hid all items (inbox non-empty but nothing on this machine).
  const allOffMachine = thisMachineOnly && inbox.length > 0 && filtered.length === 0;

  return (
    <div className="inbox-view">
      <header className="inbox-header">
        <h1 className="inbox-header__title">Inbox</h1>
        {totalCount > 0 && (
          <span className="inbox-header__count" data-testid="inbox-count">{totalCount}</span>
        )}
        <label className="inbox-header__toggle">
          <input
            type="checkbox"
            checked={thisMachineOnly}
            onChange={(e) => setThisMachineOnly(e.target.checked)}
            data-testid="toggle-this-machine"
          />
          This machine only
        </label>
      </header>

      {showBanner && (
        <DisconnectedBanner connectionState={connectionState} error={error} />
      )}

      {totalCount === 0 ? (
        <EmptyState
          message={allOffMachine ? "Nothing on this machine needs you." : undefined}
        />
      ) : (
        <ul className="inbox-list" aria-label="Inbox items">
          {sorted.map((item) => (
            <li key={item.initiativeId} className="inbox-list__item">
              <InboxRow
                item={item}
                actionSlot={
                  <RowActions
                    input={{
                      initiativeId: item.initiativeId,
                      isReap: item.kind === "reap",
                      isClosed: item.isClosed,
                      worktreeExists: item.onThisMachine,
                      rawSessionId: item.sessionId,
                      attachId: item.sessionId,
                    }}
                  />
                }
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
