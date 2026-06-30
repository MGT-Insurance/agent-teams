import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { InboxItem } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { attachToInitiative } from "../../lib/api.js";
import { StopButton } from "../../components/StopButton.js";
import "./inbox.css";

// Row label per flavor — matches the spec's specificity-follows-signal principle.
// reap:    zombie — closed initiative, worktree gone, session still alive. Stop it.
// review:  AUTHORITATIVE gate:review label; "review the PR".
// waiting: explicit gate:question/human; agent declared a blocking question.
// generic: delivered + no explicit gate; graceful degrade, no specific action asserted.
// check:   session waiting/blocked but no declared gate; softer "check on it" tier.
function rowBadgeLabel(kind: InboxItem["kind"]): string {
  if (kind === "reap") return "reap";
  if (kind === "review") return "review the PR";
  if (kind === "waiting") return "agent waiting";
  if (kind === "check") return "check on it";
  return "needs you";
}

function InboxAttachButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
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
    <button
      className="inbox-attach-btn"
      onClick={(e) => { void handleClick(e); }}
      disabled={state === "pending"}
      title="attach"
      aria-label="Attach to session"
    >
      {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "↗"}
    </button>
  );
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
        {actionSlot && (
          <div className="inbox-row__action-slot">{actionSlot}</div>
        )}
      </div>
      <p className="inbox-row__next-action">{item.nextAction}</p>
      {item.kind === "waiting" && (item.recommendation || item.alternative) && (
        <div className="inbox-row__suggestion">
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
  const filtered = thisMachineOnly ? inbox.filter((item) => item.onThisMachine || item.kind === "reap") : inbox;

  // Tiered sort: review first, then reap zombies (just-below review), then waiting/generic/check; recency desc within tier.
  const tierRank: Record<InboxItem["kind"], number> = { review: 0, reap: 1, waiting: 2, generic: 3, check: 4 };
  const sorted = [...filtered].sort((a, b) => {
    const tierDiff = tierRank[a.kind] - tierRank[b.kind];
    if (tierDiff !== 0) return tierDiff;
    return b.updatedAt.localeCompare(a.updatedAt);
  });

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
                  item.kind === "reap" && item.sessionId ? (
                    <StopButton initiativeId={item.initiativeId} sessionId={item.sessionId} />
                  ) : item.sessionId ? (
                    <InboxAttachButton initiativeId={item.initiativeId} sessionId={item.sessionId} />
                  ) : undefined
                }
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
