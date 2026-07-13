import { Fragment, useCallback, useEffect, useState } from "react";
import type { InitiativeNode, MailMessage } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { fetchMail, sendMail, closeMail, purgeMail } from "../../lib/api.js";
import "./mail.css";

// Closed states — mirrors initiatives/index.tsx's isClosed: the send picker must
// exclude closed/done initiatives (status comes from the registry as free TEXT).
const CLOSED_STATUSES = new Set(["closed", "done"]);

function isClosed(node: InitiativeNode): boolean {
  return CLOSED_STATUSES.has(node.initiative.status.toLowerCase());
}

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

// Client-side filter predicate (agent-teams-hw71.6.1). Exported as a pure
// function so it can be unit-tested directly, independent of the component's
// state wiring.
export interface MailFilters {
  // "unread" = status === "pending"; "read" = status !== "pending" (covers
  // both "read" and "acked" — both are read from the user's perspective).
  // "closed" = closed === true (bead lifecycle axis, orthogonal to status).
  readState: "all" | "unread" | "read" | "closed";
  initiativeId: string; // "" = all
  text: string; // matched case-insensitively against subject/from/to/body
}

export function filterMessages(messages: MailMessage[], filters: MailFilters): MailMessage[] {
  const text = filters.text.trim().toLowerCase();
  return messages.filter((m) => {
    if (filters.readState === "unread" && m.status !== "pending") return false;
    if (filters.readState === "read" && m.status === "pending") return false;
    if (filters.readState === "closed" && !m.closed) return false;
    if (filters.initiativeId && m.to !== filters.initiativeId) return false;
    if (text) {
      const haystack = `${m.subject} ${m.from} ${m.to} ${m.body}`.toLowerCase();
      if (!haystack.includes(text)) return false;
    }
    return true;
  });
}

// Mail is an on-demand fetch, NOT part of the SSE snapshot — light poll as a
// fallback to a manual Refresh (see router.tsx's per-track view contract).
const POLL_MS = 20_000;

function MailSendForm({
  recipients,
  onSent,
}: {
  recipients: InitiativeNode[];
  onSent: () => void;
}) {
  const [to, setTo] = useState("");
  const [body, setBody] = useState("");
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<string | null>(null);
  const [sendError, setSendError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSending(true);
    setSendError(null);
    setResult(null);
    try {
      const res = await sendMail({ to, body });
      setResult(`Sent — message ${res.messageId}`);
      setBody("");
      onSent();
    } catch (err) {
      setSendError(err instanceof Error ? err.message : String(err));
    } finally {
      setSending(false);
    }
  }

  return (
    <form className="mail-send-form" onSubmit={(e) => { void handleSubmit(e); }}>
      <h2 className="section-title">Send Mail</h2>
      <label className="mail-send-field">
        <span className="mail-send-label">To</span>
        <select
          className="mail-send-select"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          required
          aria-label="Recipient initiative"
        >
          <option value="" disabled>Select an initiative…</option>
          {recipients.map((node) => (
            <option key={node.initiative.id} value={node.initiative.id}>
              {node.initiative.title} ({node.initiative.id})
            </option>
          ))}
        </select>
      </label>
      <label className="mail-send-field">
        <span className="mail-send-label">Message</span>
        <textarea
          className="mail-send-textarea"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          required
          rows={4}
          aria-label="Message body"
        />
      </label>
      <div className="mail-send-actions">
        <button type="submit" className="mail-send-submit" disabled={sending || !to || !body.trim()}>
          {sending ? "Sending…" : "Send"}
        </button>
        {result && <span className="mail-send-result">{result}</span>}
        {sendError && <span className="mail-send-error">{sendError}</span>}
      </div>
    </form>
  );
}

function MailRow({
  message,
  expanded,
  onToggle,
  onClose,
  closing,
}: {
  message: MailMessage;
  expanded: boolean;
  onToggle: () => void;
  onClose: () => void;
  closing: boolean;
}) {
  const unread = message.status === "pending";
  // Closed rows render distinct regardless of delivery status (agent-teams-790o.4).
  const stateClass = message.closed ? "closed" : unread ? "unread" : "read";
  return (
    <Fragment>
      <tr
        className={`mail-row mail-row--${stateClass}`}
        onClick={onToggle}
        data-testid={`mail-row-${message.id}`}
      >
        <td>{message.to}</td>
        <td>{message.from}</td>
        <td>{message.subject}</td>
        <td>
          <span className={`mail-status mail-status--${message.status}`}>{message.status}</span>
          {message.closed && <span className="mail-closed-badge">closed</span>}
        </td>
        <td>{formatDateTime(message.createdAt)}</td>
        <td>{message.readAt ? formatDateTime(message.readAt) : "—"}</td>
        <td>{message.readBy ?? "—"}</td>
        <td>
          {!message.closed && (
            <button
              type="button"
              className="mail-row-close-btn"
              onClick={(e) => {
                e.stopPropagation();
                onClose();
              }}
              disabled={closing}
            >
              {closing ? "Closing…" : "Close"}
            </button>
          )}
        </td>
      </tr>
      {expanded && (
        <tr className="mail-row__detail">
          <td colSpan={8}>
            <pre className="mail-body">{message.body}</pre>
          </td>
        </tr>
      )}
    </Fragment>
  );
}

export default function MailView() {
  const { initiatives } = useSnapshotContext();
  const [messages, setMessages] = useState<MailMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [sendOpen, setSendOpen] = useState(false);
  const [readState, setReadState] = useState<MailFilters["readState"]>("all");
  const [initiativeFilter, setInitiativeFilter] = useState("");
  const [textFilter, setTextFilter] = useState("");
  const [closingId, setClosingId] = useState<string | null>(null);
  const [purging, setPurging] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const load = useCallback(() => {
    return fetchMail()
      .then((res) => {
        setMessages(res.messages);
        setFetchError(null);
      })
      .catch((err: unknown) => {
        setFetchError(err instanceof Error ? err.message : String(err));
      });
  }, []);

  useEffect(() => {
    setLoading(true);
    void load().finally(() => setLoading(false));
    const interval = setInterval(() => { void load(); }, POLL_MS);
    return () => clearInterval(interval);
  }, [load]);

  async function handleClose(id: string) {
    setClosingId(id);
    setActionError(null);
    try {
      await closeMail(id);
      await load();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err));
    } finally {
      setClosingId(null);
    }
  }

  async function handlePurge() {
    if (!window.confirm("Purge closed mail older than 7 days? This permanently deletes it.")) {
      return;
    }
    setPurging(true);
    setActionError(null);
    try {
      await purgeMail({});
      await load();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err));
    } finally {
      setPurging(false);
    }
  }

  const recipients = initiatives.filter((node) => !isClosed(node));
  // Full id -> title lookup (NOT `recipients`, which excludes closed initiatives) —
  // past mail may reference a since-closed initiative and should still show its name.
  const initiativeTitleById = new Map(initiatives.map((node) => [node.initiative.id, node.initiative.title]));
  const sorted = [...messages].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  const filtered = filterMessages(sorted, { readState, initiativeId: initiativeFilter, text: textFilter });

  // Distinct recipient ids present in the mailbox, for the initiative filter dropdown.
  const toOptions = Array.from(new Set(messages.map((m) => m.to))).sort();

  const filtersActive = readState !== "all" || initiativeFilter !== "" || textFilter.trim() !== "";

  return (
    <div className="mail-view">
      <header className="mail-header">
        <h1 className="mail-header__title">Mail</h1>
        {messages.length > 0 && (
          <span className="mail-header__count" data-testid="mail-count">{messages.length}</span>
        )}
        <button
          type="button"
          className="mail-purge-btn"
          onClick={() => { void handlePurge(); }}
          disabled={purging}
        >
          {purging ? "Purging…" : "Purge closed"}
        </button>
        <button
          type="button"
          className="mail-refresh-btn"
          onClick={() => { void load(); }}
          disabled={loading}
        >
          {loading ? "Refreshing…" : "Refresh"}
        </button>
      </header>

      {fetchError && (
        <div className="mail-banner mail-banner--error">Failed to load mail: {fetchError}</div>
      )}
      {actionError && (
        <div className="mail-banner mail-banner--error">{actionError}</div>
      )}

      <section className="mail-section">
        <h2 className="section-title">Messages</h2>
        {messages.length > 0 && (
          <div className="mail-filters">
            <select
              className="mail-filter-select"
              aria-label="Filter by read status"
              value={readState}
              onChange={(e) => setReadState(e.target.value as MailFilters["readState"])}
            >
              <option value="all">All</option>
              <option value="unread">Unread</option>
              <option value="read">Read</option>
              <option value="closed">Closed</option>
            </select>
            <select
              className="mail-filter-select"
              aria-label="Filter by initiative"
              value={initiativeFilter}
              onChange={(e) => setInitiativeFilter(e.target.value)}
            >
              <option value="">All initiatives</option>
              {toOptions.map((id) => {
                const title = initiativeTitleById.get(id);
                return (
                  <option key={id} value={id}>{title ? `${title} (${id})` : id}</option>
                );
              })}
            </select>
            <input
              type="text"
              className="mail-filter-text"
              placeholder="Search subject, from, to, body…"
              aria-label="Search messages"
              value={textFilter}
              onChange={(e) => setTextFilter(e.target.value)}
            />
            {filtersActive && (
              <button
                type="button"
                className="mail-filter-clear"
                onClick={() => {
                  setReadState("all");
                  setInitiativeFilter("");
                  setTextFilter("");
                }}
              >
                Clear filters
              </button>
            )}
          </div>
        )}
        {loading && messages.length === 0 ? (
          <p className="mail-status-text">Loading mail…</p>
        ) : messages.length === 0 ? (
          <p className="mail-status-text">No messages.</p>
        ) : filtered.length === 0 ? (
          <p className="mail-status-text">No messages match the current filters.</p>
        ) : (
          <table className="mail-table" aria-label="Mail messages">
            <thead>
              <tr>
                <th>To</th>
                <th>From</th>
                <th>Subject</th>
                <th>Status</th>
                <th>Created</th>
                <th>Read At</th>
                <th>Read By</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((m) => (
                <MailRow
                  key={m.id}
                  message={m}
                  expanded={expandedId === m.id}
                  onToggle={() => setExpandedId((cur) => (cur === m.id ? null : m.id))}
                  onClose={() => { void handleClose(m.id); }}
                  closing={closingId === m.id}
                />
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="mail-send-section">
        <button
          type="button"
          className="mail-send-toggle"
          aria-expanded={sendOpen}
          onClick={() => setSendOpen((cur) => !cur)}
        >
          {sendOpen ? "Hide send form" : "Send a message"}
        </button>
        {sendOpen && <MailSendForm recipients={recipients} onSent={() => { void load(); }} />}
      </section>
    </div>
  );
}
