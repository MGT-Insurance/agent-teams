import { useEffect, useRef, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import type { DrillInDetail, SessionState } from "@agent-teams/shared";
import { fetchInitiative, logsUrl, attachToInitiative } from "../../lib/api.js";
import "./drillin.css";

// Pick a default background session for log streaming: first bg session with a short id.
// Only background sessions have id != null; interactive sessions have id: null.
function defaultBgSession(sessions: SessionState[]): SessionState | undefined {
  return sessions.find((s) => s.kind === "background" && s.id != null);
}

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === "busy" || status === "working"
      ? "badge badge--busy"
      : status === "idle"
        ? "badge badge--idle"
        : status === "done" || status === "stopped"
          ? "badge badge--done"
          : "badge badge--default";
  return <span className={cls}>{status}</span>;
}

// Strip only the ANSI sequences that wipe xterm's scrollback buffer.
// All other sequences (cursor movement, colors) pass through unchanged so
// content stays correctly positioned within the wide fixed-col terminal.
function stripScrollbackClears(bytes: Uint8Array): Uint8Array {
  const text = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
  const cleaned = text
    // ESC[2J / ESC[3J — erase entire screen (and scrollback for 3J)
    .replace(/\x1b\[[23]J/g, "")
    // Alt-screen buffer switches (ESC[?1049h/l, ESC[?47h/l, ESC[?1047h/l)
    // — switching to alt-screen discards the main screen's scrollback
    .replace(/\x1b\[\?(?:1049|47|1047)[hl]/g, "");
  return new TextEncoder().encode(cleaned);
}

// Streams raw chunked bytes from the logs endpoint into an xterm Terminal instance.
// The endpoint is NOT SSE — it is a plain chunked HTTP response (binary).
// We use a wide fixed column count (300) so ANSI cursor-positioning sequences
// from the source terminal never overflow and cause text overlap ("mingling").
function LogPane({
  initiativeId,
  session,
}: {
  initiativeId: string;
  session: SessionState;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    // 300 cols: must be >= the source session's terminal width so ANSI
    // cursor-absolute-column sequences don't overflow and mingle text.
    // We can't query the source terminal's width, so we use a value wide
    // enough to cover all realistic terminals; the container scrolls horizontally.
    const term = new Terminal({
      cols: 300,
      theme: {
        background: "#0d0f12",
        foreground: "#c8cdd5",
        cursor: "#4a9eff",
      },
      fontFamily: '"Berkeley Mono", "Fira Code", "JetBrains Mono", ui-monospace, monospace',
      fontSize: 13,
      scrollback: 10_000,
    });
    termRef.current = term;
    term.open(container);

    const ac = new AbortController();
    abortRef.current = ac;
    // session.id is the short claude session id (8 lowercase hex); the full UUID fails.
    const url = logsUrl(initiativeId, session.id!);

    (async () => {
      try {
        const res = await fetch(url, { signal: ac.signal });
        if (!res.ok) {
          term.writeln(`\r\n\x1b[31mLog stream error: ${res.status} ${res.statusText}\x1b[0m`);
          return;
        }
        if (!res.body) {
          term.writeln("\r\n\x1b[33mNo response body — log stream unavailable.\x1b[0m");
          return;
        }
        const reader = res.body.getReader();
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          term.write(stripScrollbackClears(value));
        }
      } catch (err) {
        if (err instanceof Error && err.name === "AbortError") return;
        const msg = err instanceof Error ? err.message : String(err);
        term.writeln(`\r\n\x1b[31mLog stream failed: ${msg}\x1b[0m`);
      }
    })();

    return () => {
      ac.abort();
      term.dispose();
      termRef.current = null;
    };
    // Re-mount when the session changes (keyed on short id, not uuid).
  }, [initiativeId, session.id]);

  return (
    <div className="log-pane-scroll">
      <div className="log-pane" ref={containerRef} />
    </div>
  );
}

function AttachButton({
  initiativeId,
  session,
  compact = false,
}: {
  initiativeId: string;
  session: SessionState;
  compact?: boolean;
}) {
  const [toast, setToast] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleAttach() {
    setPending(true);
    try {
      // session.id is the short claude session id; the full UUID (session.sessionId) fails.
      await attachToInitiative(initiativeId, session.id!);
      const label = session.name ?? session.id ?? session.sessionId;
      setToast(`Launched terminal for ${label}`);
      setTimeout(() => setToast(null), 4_000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setToast(`Attach failed: ${msg}`);
      setTimeout(() => setToast(null), 6_000);
    } finally {
      setPending(false);
    }
  }

  return (
    <span className="attach-slot">
      <button
        className={compact ? "attach-btn attach-btn--compact" : "attach-btn"}
        onClick={() => { void handleAttach(); }}
        disabled={pending}
      >
        {pending ? "Attaching…" : "Attach"}
      </button>
      {toast && <span className="attach-toast">{toast}</span>}
    </span>
  );
}

export default function DrillInView() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [detail, setDetail] = useState<DrillInDetail | null>(null);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Which bg session to stream logs for.
  const [logSession, setLogSession] = useState<SessionState | undefined>(undefined);

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    setFetchError(null);
    setDetail(null);

    fetchInitiative(id)
      .then((d) => {
        setDetail(d);
        setLogSession(defaultBgSession(d.sessions));
      })
      .catch((err: unknown) => {
        setFetchError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => setLoading(false));
  }, [id]);

  if (loading) {
    return <div className="drillin-status">Loading initiative…</div>;
  }

  if (fetchError) {
    return (
      <div className="drillin-status drillin-status--error">
        Failed to load initiative: {fetchError}
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="drillin-status drillin-status--error">
        Initiative not found.
      </div>
    );
  }

  const bgSessions = detail.sessions.filter((s) => s.kind === "background" && s.id != null);
  const reversedNotes = detail.notesHistory.slice().reverse();

  return (
    <div className="drillin">
      {/* Top toolbar: back + attach */}
      <div className="drillin-toolbar">
        <button className="back-btn" onClick={() => navigate(-1)}>
          ← Back
        </button>
        {bgSessions.length > 0 && (() => {
          const primary = bgSessions[0];
          if (!primary) return null;
          return (
          <div className="toolbar-attach">
            {bgSessions.length === 1 ? (
              <>
                <span className="toolbar-attach-label">
                  {primary.name ?? primary.id ?? primary.sessionId}
                </span>
                <AttachButton initiativeId={detail.id} session={primary} />
              </>
            ) : (
              bgSessions.map((s) => (
                <span key={s.id} className="toolbar-attach-item">
                  <span className="toolbar-attach-label">{s.name ?? s.id}</span>
                  <AttachButton initiativeId={detail.id} session={s} compact />
                </span>
              ))
            )}
          </div>
          );
        })()}
      </div>

      {/* Header */}
      <section className="drillin-header">
        <h1 className="drillin-title">{detail.title}</h1>
        <div className="drillin-meta">
          <span className="meta-item">
            <span className="meta-label">status</span>
            <StatusBadge status={detail.status} />
          </span>
          <span className="meta-item">
            <span className="meta-label">branch</span>
            <span className="meta-value meta-value--mono">{detail.branch || "—"}</span>
          </span>
          <span className="meta-item">
            <span className="meta-label">repo</span>
            <span className="meta-value meta-value--mono">{detail.repo || "—"}</span>
          </span>
          {detail.prUrl && (
            <span className="meta-item">
              <span className="meta-label">PR</span>
              <a
                className="meta-link"
                href={detail.prUrl}
                target="_blank"
                rel="noreferrer"
              >
                {detail.prUrl}
              </a>
            </span>
          )}
        </div>
        {detail.goal && (
          <p className="drillin-goal">{detail.goal}</p>
        )}
      </section>

      {/* Notes / history — most recent first */}
      <section className="drillin-section">
        <h2 className="section-title">History</h2>
        {reversedNotes.length === 0 ? (
          <p className="section-empty">No notes yet.</p>
        ) : (
          <ol className="notes-list">
            {reversedNotes.map((note, i) => (
              <li key={i} className="notes-item">
                <span className="notes-index">{reversedNotes.length - i}</span>
                <pre className="notes-text">{note}</pre>
              </li>
            ))}
          </ol>
        )}
      </section>

      {/* Sessions */}
      <section className="drillin-section">
        <h2 className="section-title">Sessions</h2>
        {detail.sessions.length === 0 ? (
          <p className="section-empty">No live sessions.</p>
        ) : (
          <table className="sessions-table">
            <thead>
              <tr>
                <th>name</th>
                <th>kind</th>
                <th>status</th>
                <th>state</th>
              </tr>
            </thead>
            <tbody>
              {detail.sessions.map((s) => (
                <tr key={s.sessionId} className="sessions-row">
                  <td className="mono">{s.name ?? s.id ?? "—"}</td>
                  <td>{s.kind}</td>
                  <td>
                    <StatusBadge status={s.status ?? "—"} />
                  </td>
                  <td>{s.state ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* Work beads — epics with nested children, then bare labeled beads */}
      <section className="drillin-section">
        <h2 className="section-title">Work Beads</h2>
        {detail.workBeads.length === 0 ? (
          <p className="section-empty">No beads for this initiative.</p>
        ) : (() => {
          const epics = detail.workBeads.filter(b => b.issue_type === "epic");
          const childrenByEpic = new Map<string, typeof detail.workBeads>();
          for (const b of detail.workBeads) {
            if (b.parent) {
              const list = childrenByEpic.get(b.parent) ?? [];
              list.push(b);
              childrenByEpic.set(b.parent, list);
            }
          }
          const epicIds = new Set(epics.map(e => e.id));
          const bareBeads = detail.workBeads.filter(
            b => b.issue_type !== "epic" && !b.parent
          );
          return (
            <table className="beads-table">
              <thead>
                <tr>
                  <th>id</th>
                  <th>title</th>
                  <th>status</th>
                  <th>priority</th>
                  <th>type</th>
                </tr>
              </thead>
              <tbody>
                {epics.map(epic => (
                  <>
                    <tr key={epic.id} className="beads-row beads-row--epic">
                      <td className="mono">{epic.id}</td>
                      <td className="beads-epic-title">{epic.title}</td>
                      <td><StatusBadge status={epic.status} /></td>
                      <td>{epic.priority}</td>
                      <td>{epic.issue_type}</td>
                    </tr>
                    {(childrenByEpic.get(epic.id) ?? []).map(child => (
                      <tr key={child.id} className="beads-row beads-row--child">
                        <td className="mono beads-child-id"><span className="beads-tree-connector">└</span>{child.id}</td>
                        <td className="beads-child-title">{child.title}</td>
                        <td><StatusBadge status={child.status} /></td>
                        <td>{child.priority}</td>
                        <td>{child.issue_type}</td>
                      </tr>
                    ))}
                  </>
                ))}
                {bareBeads.map(b => (
                  <tr key={b.id} className="beads-row">
                    <td className="mono">{b.id}</td>
                    <td>{b.title}</td>
                    <td><StatusBadge status={b.status} /></td>
                    <td>{b.priority}</td>
                    <td>{b.issue_type}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          );
        })()}
      </section>

      {/* Logs pane */}
      <section className="drillin-section drillin-section--logs">
        <h2 className="section-title">
          Logs
          {bgSessions.length > 1 && (
            <select
              className="log-session-select"
              value={logSession?.id ?? ""}
              onChange={(e) => {
                const s = bgSessions.find((b) => b.id === e.target.value);
                setLogSession(s);
              }}
            >
              {bgSessions.map((s) => (
                <option key={s.id} value={s.id!}>
                  {s.name ?? s.id ?? s.sessionId}
                </option>
              ))}
            </select>
          )}
        </h2>

        {bgSessions.length === 0 ? (
          <p className="section-empty">No background sessions — logs unavailable.</p>
        ) : logSession == null ? (
          <p className="section-empty">Select a session to view logs.</p>
        ) : (
          <LogPane
            key={logSession.id}
            initiativeId={detail.id}
            session={logSession}
          />
        )}
      </section>

    </div>
  );
}
