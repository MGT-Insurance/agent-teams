import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { InitiativeNode } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
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

// Closed states — hidden unless "Show closed" is on. Status comes from the
// registry as free TEXT, so compare case-insensitively.
const CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosed(node: InitiativeNode): boolean {
  return CLOSED_STATUSES.has(node.initiative.status.toLowerCase());
}

// Signal 3 — "session" — three states:
//   "on"     = LIVE: working (busy/state=working) or waiting (status=waiting/
//              state=blocked, e.g. a parked agent on a human gate).
//   "muted"  = DORMANT: a session process is matched but finished/idle
//              (idle/done/stopped) — still alive, but not actively working.
//   "off"    = NONE: no matched session process at all.
function sessionLevel(node: InitiativeNode): ChipLevel {
  const s = node.session;
  if (s === null) return "off";
  if (
    s.status === "busy" ||
    s.status === "waiting" ||
    s.state === "working" ||
    s.state === "blocked"
  ) {
    return "on";
  }
  return "muted";
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

// Chip intensity: "on" = present/lit, "muted" = present-but-dormant (session
// only), "off" = absent. See initiatives.css.
type ChipLevel = "on" | "muted" | "off";

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

function InitiativeRow({ node }: { node: InitiativeNode }) {
  const navigate = useNavigate();
  const { initiative } = node;

  const onMachine = node.worktreeExists;
  const hasPr = node.delivery === "pr-open";
  const sLevel = sessionLevel(node);
  const sessionIcon = sLevel === "on" ? "●" : sLevel === "muted" ? "◐" : "○";
  const sessionValue = sLevel === "on" ? "live" : sLevel === "muted" ? "dormant" : "none";

  function handleRowClick() {
    navigate(`/initiative/${initiative.id}`);
  }

  function handlePrLinkClick(e: React.MouseEvent<HTMLAnchorElement>) {
    // Don't let the PR link bubble up to the row's drill-in navigation.
    e.stopPropagation();
  }

  const sessionDetail = node.session
    ? `${node.session.status}${node.session.state ? ` (${node.session.state})` : ""}`
    : "";
  const sessionTitle =
    sLevel === "on"
      ? `Live session: ${sessionDetail}`
      : sLevel === "muted"
        ? `Dormant session (process alive): ${sessionDetail}`
        : "No session";

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
          level={sLevel}
          tone="session"
          icon={sessionIcon}
          label="session"
          value={sessionValue}
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
  const [showClosed, setShowClosed] = usePersistedBool("initiatives.showClosed", false);
  const [onlyOnMachine, setOnlyOnMachine] = usePersistedBool("initiatives.onlyOnMachine", false);

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
