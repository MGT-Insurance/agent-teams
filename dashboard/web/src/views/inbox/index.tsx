import { useNavigate } from "react-router-dom";
import type { InboxItem } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import "./inbox.css";

// Row label per flavor — matches the spec's specificity-follows-signal principle.
// waiting: agent is paused, waiting for input.
// review: delivered + session ended; we know the ask is verify & merge.
// generic: delivered + no session; graceful degrade, no specific action asserted.
function rowBadgeLabel(kind: InboxItem["kind"]): string {
  if (kind === "waiting") return "agent waiting";
  if (kind === "review") return "verify & merge";
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
        {/* PR chip on waiting items — makes the orthogonal delivery visible at a glance. */}
        {item.kind === "waiting" && item.prUrl && (
          <span className="inbox-row__pr-chip" aria-label="open PR">PR</span>
        )}
        {actionSlot && (
          <div className="inbox-row__action-slot">{actionSlot}</div>
        )}
      </div>
      <p className="inbox-row__question">{item.question}</p>
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

interface SectionProps {
  title: string;
  items: InboxItem[];
  dataSection: string;
}

function InboxSection({ title, items, dataSection }: SectionProps) {
  if (items.length === 0) return null;
  return (
    <section className="inbox-section" data-section={dataSection} aria-label={title}>
      <h2 className="inbox-section__title">{title}</h2>
      <ul className="inbox-list" aria-label={`${title} items`}>
        {items.map((item) => (
          <li key={item.initiativeId} className="inbox-list__item">
            <InboxRow item={item} />
          </li>
        ))}
      </ul>
    </section>
  );
}

export default function InboxView() {
  const { inbox, connectionState, error } = useSnapshotContext();

  // "Waiting on you": agent is blocked/waiting on human input (most urgent).
  const waitingItems = inbox.filter((i) => i.kind === "waiting");
  // "Delivered — needs you": PR delivered (review = session ended; generic = no session).
  const deliveredItems = inbox.filter((i) => i.kind === "review" || i.kind === "generic");

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
        <div className="inbox-sections">
          <InboxSection
            title="Waiting on you"
            items={waitingItems}
            dataSection="waiting"
          />
          <InboxSection
            title="Delivered — needs you"
            items={deliveredItems}
            dataSection="delivered"
          />
        </div>
      )}
    </div>
  );
}
