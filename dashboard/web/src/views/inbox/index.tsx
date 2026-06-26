import { useNavigate } from "react-router-dom";
import type { InboxItem } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import "./inbox.css";

// Row label per flavor — matches the spec's specificity-follows-signal principle.
// review: AUTHORITATIVE gate:review label; "review the PR".
// waiting: agent is paused, waiting for input (explicit question gate or session blocked).
// generic: delivered + no explicit gate; graceful degrade, no specific action asserted.
function rowBadgeLabel(kind: InboxItem["kind"]): string {
  if (kind === "review") return "review the PR";
  if (kind === "waiting") return "agent waiting";
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

  return (
    <div
      className={`inbox-row inbox-row--${item.kind}`}
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
        {/* PR chip on review and waiting items with a PR URL. */}
        {(item.kind === "review" || item.kind === "waiting") && item.prUrl && (
          <span className="inbox-row__pr-chip" aria-label="open PR">PR</span>
        )}
        {actionSlot && (
          <div className="inbox-row__action-slot">{actionSlot}</div>
        )}
      </div>
      <p className="inbox-row__next-action">{item.nextAction}</p>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="inbox-empty">
      <span className="inbox-empty__icon">✓</span>
      <p className="inbox-empty__message">Nothing needs you right now.</p>
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

  // Sort newest-updated-first. ISO-8601 Zulu strings compare correctly as strings.
  const sorted = [...inbox].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

  const showBanner = connectionState !== "connected";
  const totalCount = inbox.length;

  return (
    <div className="inbox-view">
      <header className="inbox-header">
        <h1 className="inbox-header__title">Inbox</h1>
        {totalCount > 0 && (
          <span className="inbox-header__count" data-testid="inbox-count">{totalCount}</span>
        )}
      </header>

      {showBanner && (
        <DisconnectedBanner connectionState={connectionState} error={error} />
      )}

      {totalCount === 0 ? (
        <EmptyState />
      ) : (
        <ul className="inbox-list" aria-label="Inbox items">
          {sorted.map((item) => (
            <li key={item.initiativeId} className="inbox-list__item">
              <InboxRow item={item} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
