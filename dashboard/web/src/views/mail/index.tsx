import { Fragment, useCallback, useEffect, useState } from "react";
import type { InitiativeNode, MailMessage } from "@agent-teams/shared";
import { useSnapshotContext } from "../../SnapshotContext.js";
import { fetchMail, sendMail } from "../../lib/api.js";
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
}: {
  message: MailMessage;
  expanded: boolean;
  onToggle: () => void;
}) {
  const unread = message.status === "pending";
  return (
    <Fragment>
      <tr
        className={`mail-row mail-row--${unread ? "unread" : "read"}`}
        onClick={onToggle}
        data-testid={`mail-row-${message.id}`}
      >
        <td>{message.to}</td>
        <td>{message.from}</td>
        <td>{message.subject}</td>
        <td>
          <span className={`mail-status mail-status--${message.status}`}>{message.status}</span>
        </td>
        <td>{formatDateTime(message.createdAt)}</td>
        <td>{message.readAt ? formatDateTime(message.readAt) : "—"}</td>
        <td>{message.readBy ?? "—"}</td>
      </tr>
      {expanded && (
        <tr className="mail-row__detail">
          <td colSpan={7}>
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

  const recipients = initiatives.filter((node) => !isClosed(node));
  const sorted = [...messages].sort((a, b) => b.createdAt.localeCompare(a.createdAt));

  return (
    <div className="mail-view">
      <header className="mail-header">
        <h1 className="mail-header__title">Mail</h1>
        {messages.length > 0 && (
          <span className="mail-header__count" data-testid="mail-count">{messages.length}</span>
        )}
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

      <MailSendForm recipients={recipients} onSent={() => { void load(); }} />

      <section className="mail-section">
        <h2 className="section-title">Messages</h2>
        {loading && messages.length === 0 ? (
          <p className="mail-status-text">Loading mail…</p>
        ) : sorted.length === 0 ? (
          <p className="mail-status-text">No messages.</p>
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
              </tr>
            </thead>
            <tbody>
              {sorted.map((m) => (
                <MailRow
                  key={m.id}
                  message={m}
                  expanded={expandedId === m.id}
                  onToggle={() => setExpandedId((cur) => (cur === m.id ? null : m.id))}
                />
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
