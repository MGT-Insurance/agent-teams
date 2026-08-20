import { useState } from "react";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";
import { sessionKind as canonicalSessionKind, isValidSessionId } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { useRowClickNav } from "../../hooks/useRowClickNav.js";
import { RowActions } from "../../components/RowActions.js";
import { AlertInfoIcon } from "../../components/AlertInfoIcon.js";
import {
  buildInitiativeBoard,
  type BoardInitiativeNode,
  type BoardPullRequest,
  type InitiativeKanbanCard,
  type InitiativeKanbanLane,
} from "./kanban-model.js";
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

const CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosed(node: InitiativeNode): boolean {
  return CLOSED_STATUSES.has(node.initiative.status.toLowerCase());
}

function sessionKind(node: InitiativeNode): "alive" | "dead" | "none" {
  return canonicalSessionKind(node.session);
}

// A closed initiative with any lingering session stays visible until reaped.
function isCompleted(node: InitiativeNode): boolean {
  return isClosed(node) && sessionKind(node) === "none";
}

function sessionAttachId(session: SessionState | null | undefined): string | undefined {
  const id = session?.id;
  return typeof id === "string" && isValidSessionId(id) ? id : undefined;
}

type ChipTone = "machine" | "pr" | "session";
type ChipLevel = "on" | "good" | "warn" | "muted" | "off";

function sessionChip(node: InitiativeNode): { glyph: string; level: ChipLevel; value: string } {
  const kind = sessionKind(node);
  if (kind === "none") return { glyph: "○", level: "off", value: "none" };
  if (kind === "alive") {
    return isClosed(node)
      ? { glyph: "●", level: "warn", value: "running (close it)" }
      : { glyph: "●", level: "good", value: "running" };
  }
  const actionable = isClosed(node) || node.worktreeExists;
  return { glyph: "◐", level: actionable ? "warn" : "muted", value: "dead" };
}

function phaseClass(phase: string): string {
  return `init-row__phase--${phase.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
}

interface SignalChipProps {
  level: ChipLevel;
  tone: ChipTone;
  icon: string;
  label: string;
  value: string;
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

/**
 * Keep the model boundary explicit: the browser model receives only its
 * sanctioned initiative/workstream rails, while the original node remains on
 * the adapted object for identity signals, actions, and alerts.
 */
type InitiativeBoardNode = InitiativeNode & BoardInitiativeNode;

function toBoardNode(node: InitiativeNode): InitiativeBoardNode {
  return {
    ...node,
    initiative: {
      ...node.initiative,
      prs: node.initiative.prs,
      prReviews: node.initiative.prReviews,
      prWorkstreams: node.initiative.prWorkstreams,
    },
    workstreams: node.workstreams,
    workstreamDiagnostics: node.workstreamDiagnostics,
  };
}

function stopIdentityNavigation(
  event: React.MouseEvent<HTMLAnchorElement> | React.KeyboardEvent<HTMLAnchorElement>,
) {
  event.stopPropagation();
}

function pullRequestLabel(href: string): string {
  try {
    const url = new URL(href);
    const parts = url.pathname.split("/").filter(Boolean);
    if (parts.length >= 4 && parts[parts.length - 2] === "pull") {
      return `${parts[0]}/${parts[1]} #${parts[parts.length - 1]}`;
    }
    return `${url.hostname}${url.pathname}`;
  } catch {
    return href;
  }
}

function PullRequestLink({ pr, unassigned = false }: { pr: BoardPullRequest; unassigned?: boolean }) {
  const gate = pr.rawGate === "" ? "no gate" : pr.rawGate;
  return (
    <a
      href={pr.href}
      target="_blank"
      rel="noopener noreferrer"
      className="initiative-pr-link"
      onClick={stopIdentityNavigation}
      onKeyDown={stopIdentityNavigation}
      aria-label={`Open PR ${pullRequestLabel(pr.href)}; gate ${gate}${unassigned ? "; unassigned" : ""}`}
      title={pr.href}
    >
      <span className="initiative-pr-link__name">{pullRequestLabel(pr.href)} ↗</span>
      <span className="initiative-pr-link__gate">Gate: {gate}</span>
    </a>
  );
}

function WorkstreamCard({ card, columnLabel }: { card: InitiativeKanbanCard; columnLabel: string }) {
  const identity = card.kind === "fallback" ? `Fallback ${card.key}` : `Bead ${card.workstreamId}`;
  const issueType = card.issueType || "unspecified";
  const priority = card.priority === null || card.priority === "" ? "unspecified" : String(card.priority);

  return (
    <article
      className="initiative-card"
      data-column={card.columnId}
      aria-label={`${card.title}, ${columnLabel}`}
    >
      <div className="initiative-card__identity">{identity}</div>
      <h4 className="initiative-card__title">{card.title}</h4>
      <dl className="initiative-card__facts">
        <div><dt>Board state</dt><dd>{columnLabel}</dd></div>
        <div><dt>Raw state</dt><dd>{card.rawStatus || "unknown"}</dd></div>
        <div><dt>Type</dt><dd>{issueType}</dd></div>
        <div><dt>Priority</dt><dd>{priority}</dd></div>
        <div><dt>Descendants</dt><dd>{card.progress.closed} / {card.progress.total} closed</dd></div>
      </dl>

      {card.labels.length > 0 && (
        <ul className="initiative-card__labels" aria-label="Workstream labels">
          {card.labels.map((label, index) => <li key={`${label}:${index}`}>{label}</li>)}
        </ul>
      )}

      {card.pullRequests.length > 0 && (
        <div className="initiative-card__prs">
          <h5>Pull requests ({card.pullRequests.length})</h5>
          <ul>
            {card.pullRequests.map((pr) => (
              <li key={pr.href}><PullRequestLink pr={pr} /></li>
            ))}
          </ul>
        </div>
      )}
    </article>
  );
}

function LaneDiagnostics({ lane }: { lane: InitiativeKanbanLane<InitiativeBoardNode> }) {
  const { diagnostics } = lane;
  const hasDiagnostics =
    diagnostics.unassignedPRs.length > 0 ||
    diagnostics.staleAssociations.length > 0 ||
    diagnostics.source.length > 0 ||
    diagnostics.duplicateWorkstreamIds.length > 0;

  if (!hasDiagnostics) return null;

  return (
    <aside className="initiative-lane__diagnostics" aria-label="Initiative diagnostics">
      {diagnostics.unassignedPRs.length > 0 && (
        <div className="initiative-diagnostic initiative-diagnostic--warn">
          <h4>Unassigned PRs ({diagnostics.unassignedPRs.length})</h4>
          <ul>
            {diagnostics.unassignedPRs.map((pr) => (
              <li key={pr.href}><PullRequestLink pr={pr} unassigned /></li>
            ))}
          </ul>
        </div>
      )}

      {diagnostics.staleAssociations.length > 0 && (
        <div className="initiative-diagnostic initiative-diagnostic--warn">
          <h4>Stale PR mappings ({diagnostics.staleAssociations.length})</h4>
          <ul>
            {diagnostics.staleAssociations.map(({ association, reason }, index) => (
              <li key={`${association.pr}:${association.workstream}:${index}`}>
                <span>{reason.replaceAll("-", " ")}: </span>
                <a
                  href={association.pr}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={stopIdentityNavigation}
                  onKeyDown={stopIdentityNavigation}
                  aria-label={`Open stale PR mapping ${pullRequestLabel(association.pr)}`}
                >
                  {pullRequestLabel(association.pr)} ↗
                </a>
                <span> → {association.workstream}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {diagnostics.source.length > 0 && (
        <div className="initiative-diagnostic">
          <h4>Workstream data ({diagnostics.source.length})</h4>
          <ul>
            {diagnostics.source.map((diagnostic, index) => (
              <li key={`${diagnostic.kind}:${diagnostic.beadId ?? ""}:${index}`}>
                <strong>{diagnostic.kind.replaceAll("-", " ")}:</strong> {diagnostic.message}
                {diagnostic.beadId ? ` (${diagnostic.beadId})` : ""}
              </li>
            ))}
          </ul>
        </div>
      )}

      {diagnostics.duplicateWorkstreamIds.length > 0 && (
        <div className="initiative-diagnostic initiative-diagnostic--warn">
          <h4>Duplicate workstreams</h4>
          <p>{diagnostics.duplicateWorkstreamIds.join(", ")}</p>
        </div>
      )}
    </aside>
  );
}

function InitiativeIdentity({ lane }: { lane: InitiativeKanbanLane<InitiativeBoardNode> }) {
  const node = lane.node;
  const { initiative } = node;
  const rowNav = useRowClickNav(initiative.id, initiative.title);
  const sess = sessionChip(node);
  const attachId = sessionAttachId(node.session);
  const sourcePrCount = lane.accounting.sourcePRCount;
  const hasPrSignal = node.delivery === "pr-open" || sourcePrCount > 0;
  const explicitWorkstreams = lane.accounting.sourceWorkstreamCount;

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
      className="row-card init-row initiative-lane__identity"
      data-initiative-id={initiative.id}
      data-closed={isClosed(node) ? "true" : "false"}
      data-alert={node.alert?.level}
      {...rowNav}
    >
      <div className="init-row__main">
        <h3 className="init-row__title">{initiative.title}</h3>
        <span className="init-row__id">{initiative.id}</span>
        <span className={`init-row__phase ${phaseClass(node.phase)}`}>{node.phase}</span>
        <p className="initiative-lane__summary">
          {explicitWorkstreams > 0
            ? `${explicitWorkstreams} workstream${explicitWorkstreams === 1 ? "" : "s"}`
            : "Initiative fallback"}
          {` · ${sourcePrCount} PR${sourcePrCount === 1 ? "" : "s"}`}
        </p>
      </div>

      <div className="init-row__signals" aria-label="Initiative signals">
        <SignalChip
          level={node.worktreeExists ? "on" : "off"}
          tone="machine"
          icon="▣"
          label="on machine"
          value={node.worktreeExists ? "yes" : "no"}
          title={node.worktreeExists ? "Worktree exists on this machine" : "Worktree not on this machine"}
        />
        <SignalChip
          level={hasPrSignal ? "on" : "off"}
          tone="pr"
          icon="⎘"
          label="PR"
          value={sourcePrCount > 0 ? `${sourcePrCount} open` : hasPrSignal ? "yes" : "no"}
          title={sourcePrCount > 0 ? `${sourcePrCount} open pull request${sourcePrCount === 1 ? "" : "s"}` : hasPrSignal ? "Has an open PR" : "No open PR"}
        />
        <SignalChip
          level={sess.level}
          tone="session"
          icon={sess.glyph}
          label="session"
          value={sess.value}
          title={sessionTitle}
        />
      </div>

      <LaneDiagnostics lane={lane} />

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
      <AlertInfoIcon alert={node.alert} />
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
    <div className={`initiatives-banner initiatives-banner--${isError ? "error" : "warn"}`} role="status">
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
    // Reap zombies are always visible — bypass both visibility toggles.
    const forceShow = node.needsHuman === "reap";
    if (!forceShow && !showCompleted && isCompleted(node)) return false;
    if (!forceShow && onlyOnMachine && !node.worktreeExists) return false;
    if (q === "") return true;
    const { id, title } = node.initiative;
    return id.toLowerCase().includes(q) || title.toLowerCase().includes(q);
  });
  const board = buildInitiativeBoard(filtered.map(toBoardNode));
  const showBanner = connectionState !== "connected";
  const emptyMessage = q !== "" ? "No initiatives match your search." : "No initiatives to show.";

  return (
    <div className="initiatives-view">
      <header className="initiatives-header">
        <h1 className="initiatives-header__title">Initiatives</h1>
        <span className="initiatives-header__count" data-testid="initiatives-count">
          {filtered.length}
        </span>
      </header>

      {showBanner && <DisconnectedBanner connectionState={connectionState} error={error} />}

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
        <section className="initiative-board" aria-labelledby="initiative-board-title">
          <h2 id="initiative-board-title" className="initiative-board__title">
            Initiative workflow board
          </h2>
          <p className="initiative-board__summary">
            {board.accounting.cardCount} workstream card{board.accounting.cardCount === 1 ? "" : "s"} across seven states
          </p>
          <div
            className="initiative-board__scroller"
            role="region"
            aria-label="Seven-column initiative swimlane board; scroll horizontally to see every state"
            tabIndex={0}
          >
            <div className="initiative-board__canvas">
              <div className="initiative-board__header" aria-hidden="true">
                <div className="initiative-board__corner">Initiative</div>
                {board.columns.map((column) => {
                  const count = board.lanes.reduce((total, lane) => total + lane.cells[column.id].count, 0);
                  return (
                    <div key={column.id} className="initiative-board__column-header" data-column={column.id}>
                      <span>{column.label}</span>
                      <span>{count}</span>
                    </div>
                  );
                })}
              </div>

              <ol className="initiative-board__lanes" aria-label="Initiative swimlanes">
                {board.lanes.map((lane) => (
                  <li key={lane.key} className="initiative-board__lane">
                    <InitiativeIdentity lane={lane} />
                    {board.columns.map((column) => {
                      const cell = lane.cells[column.id];
                      return (
                        <section
                          key={column.id}
                          className="initiative-board__cell"
                          data-column={column.id}
                          aria-label={`${column.label}: ${cell.count} workstream${cell.count === 1 ? "" : "s"} for ${lane.node.initiative.title}`}
                        >
                          <h3 className="initiative-board__cell-title">
                            <span>{column.label}</span>
                            <span>{cell.count}</span>
                          </h3>
                          {cell.cards.length === 0 ? (
                            <p className="initiative-board__empty">No workstreams</p>
                          ) : (
                            <ul className="initiative-board__cards">
                              {cell.cards.map((card) => (
                                <li key={card.key}>
                                  <WorkstreamCard card={card} columnLabel={column.label} />
                                </li>
                              ))}
                            </ul>
                          )}
                        </section>
                      );
                    })}
                  </li>
                ))}
              </ol>
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
