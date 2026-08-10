import { useState } from "react";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";
import { sessionKind as canonicalSessionKind, isValidSessionId } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { useRowClickNav } from "../../hooks/useRowClickNav.js";
import { RowActions } from "../../components/RowActions.js";
import { AlertInfoIcon } from "../../components/AlertInfoIcon.js";
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
// Thin node-level wrapper — the actual classification is the canonical
// sessionKind() from @agent-teams/shared (agent-teams-rybk.5.2), shared with
// the server's deriveAlert (server/src/parse.ts). Do not re-implement the
// status!=null check here.
function sessionKind(node: InitiativeNode): "alive" | "dead" | "none" {
  return canonicalSessionKind(node.session);
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
  return typeof id === "string" && isValidSessionId(id) ? id : undefined;
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

function InitiativeRow({ node }: { node: InitiativeNode }) {
  const { initiative } = node;
  const rowNav = useRowClickNav(initiative.id, initiative.title);

  const onMachine = node.worktreeExists;
  const hasPr = node.delivery === "pr-open";
  const sess = sessionChip(node);
  const alert = node.alert;
  const attachId = sessionAttachId(node.session);

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
      className="row-card init-row"
      data-initiative-id={initiative.id}
      data-closed={isClosed(node) ? "true" : "false"}
      data-alert={alert?.level}
      {...rowNav}
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
        {/* One chip per PR — an initiative can have more than one open at once
            (agent-teams-ssib.9). */}
        {hasPr && initiative.prs.length > 0 ? (
          initiative.prs.map((url) => (
            <a
              key={url}
              href={url}
              target="_blank"
              rel="noreferrer"
              className="init-chip init-chip--on init-chip--pr init-chip--link"
              onClick={handlePrLinkClick}
              title={`Open PR: ${url}`}
              aria-label="open PR: yes"
            >
              <span className="init-chip__icon" aria-hidden="true">⎘</span>
              <span className="init-chip__label">PR ↗</span>
            </a>
          ))
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
      <div className="row-action-slot">
        <RowActions
          input={{
            initiativeId: initiative.id,
            isReap: node.needsHuman === "reap",
            isClosed: isClosed(node),
            worktreeExists: node.worktreeExists,
            rawSessionId: node.session?.id,
            attachId,
          }}
        />
      </div>
      <AlertInfoIcon alert={alert} />
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
    // Reap zombies are always visible — bypass BOTH the showCompleted and onlyOnMachine filters.
    const forceShow = node.needsHuman === "reap";
    if (!forceShow && !showCompleted && isCompleted(node)) return false;
    if (!forceShow && onlyOnMachine && !node.worktreeExists) return false;
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
