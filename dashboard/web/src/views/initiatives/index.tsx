import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { InitiativeNode } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import "./initiatives.css";

// Closed states — hidden unless "Show closed" is on. Status comes from the
// registry as free TEXT, so compare case-insensitively.
const CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosed(node: InitiativeNode): boolean {
  return CLOSED_STATUSES.has(node.initiative.status.toLowerCase());
}

// Signal 3 — "has an existing session": a live session is present. Per the
// SessionSignal model (shared/types.ts), "live" = working (busy/state=working)
// OR waiting (status=waiting/state=blocked — e.g. a parked agent on a human
// gate). Idle->ended and done/stopped sessions are NOT live and do not light.
function hasLiveSession(node: InitiativeNode): boolean {
  const s = node.session;
  if (s === null) return false;
  return (
    s.status === "busy" ||
    s.status === "waiting" ||
    s.state === "working" ||
    s.state === "blocked"
  );
}

// Per-signal hue so the three chips are distinguishable when lit:
// machine=blue, pr=violet, session=green (see initiatives.css).
type ChipTone = "machine" | "pr" | "session";

interface SignalChipProps {
  active: boolean;
  tone: ChipTone;
  icon: string;
  label: string;
  title: string;
}

function SignalChip({ active, tone, icon, label, title }: SignalChipProps) {
  return (
    <span
      className={`init-chip init-chip--${active ? "on" : "off"} init-chip--${tone}`}
      title={title}
      aria-label={`${label}: ${active ? "yes" : "no"}`}
    >
      <span className="init-chip__icon" aria-hidden="true">{icon}</span>
      <span className="init-chip__label">{label}</span>
    </span>
  );
}

function InitiativeRow({ node }: { node: InitiativeNode }) {
  const navigate = useNavigate();
  const { initiative } = node;

  const onMachine = node.worktreeExists;
  const hasPr = node.delivery === "pr-open";
  const running = hasLiveSession(node);

  function handleRowClick() {
    navigate(`/initiative/${initiative.id}`);
  }

  function handlePrLinkClick(e: React.MouseEvent<HTMLAnchorElement>) {
    // Don't let the PR link bubble up to the row's drill-in navigation.
    e.stopPropagation();
  }

  const sessionTitle = node.session
    ? `Running session: ${node.session.status}${node.session.state ? ` (${node.session.state})` : ""}`
    : "No running session";

  return (
    <div
      className="init-row"
      data-initiative-id={initiative.id}
      data-closed={isClosed(node) ? "true" : "false"}
      onClick={handleRowClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") handleRowClick(); }}
      aria-label={`Open initiative: ${initiative.title}`}
    >
      <div className="init-row__main">
        <span className="init-row__title">{initiative.title}</span>
        <span className="init-row__id">{initiative.id}</span>
        <span className="init-row__phase">{node.phase}</span>
      </div>
      <div className="init-row__signals">
        <SignalChip
          active={onMachine}
          tone="machine"
          icon="▣"
          label="on machine"
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
            active={hasPr}
            tone="pr"
            icon="⎘"
            label="PR"
            title={hasPr ? "Has an open PR" : "No open PR"}
          />
        )}
        <SignalChip
          active={running}
          tone="session"
          icon="●"
          label="session"
          title={sessionTitle}
        />
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
  const [showClosed, setShowClosed] = useState(false);
  const [onlyOnMachine, setOnlyOnMachine] = useState(false);

  const q = query.trim().toLowerCase();
  const filtered = initiatives.filter((node) => {
    if (!showClosed && isClosed(node)) return false;
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
            checked={showClosed}
            onChange={(e) => setShowClosed(e.target.checked)}
          />
          Show closed
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
