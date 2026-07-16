import { Fragment, useCallback, useEffect, useState } from "react";
import type { MemoryEntry } from "@agent-teams/shared";
import { fetchMemories } from "../../lib/api.js";
import "./memories.css";

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

// Client-side filter predicate (agent-teams-hvje.4), mirrors mail's filterMessages.
export interface MemoryFilters {
  role: string; // "" = all
  tier: "all" | "hot" | "fresh" | "cold";
  text: string; // matched case-insensitively against role/slug/body
}

export function filterMemories(entries: MemoryEntry[], filters: MemoryFilters): MemoryEntry[] {
  const text = filters.text.trim().toLowerCase();
  return entries.filter((m) => {
    if (filters.role && m.role !== filters.role) return false;
    if (filters.tier !== "all" && m.tier !== filters.tier) return false;
    if (text) {
      const haystack = `${m.role} ${m.slug} ${m.body}`.toLowerCase();
      if (!haystack.includes(text)) return false;
    }
    return true;
  });
}

// Default sort: most-applied first (the "how they've been applied" signal Eric
// led with), tiebreak by raw key ascending for a deterministic order.
export function sortMemories(entries: MemoryEntry[]): MemoryEntry[] {
  return [...entries].sort((a, b) => b.appliedCount - a.appliedCount || a.key.localeCompare(b.key));
}

// Memories are an on-demand fetch, NOT part of the SSE snapshot — light poll,
// mirrors mail's POLL_MS pattern.
const POLL_MS = 20_000;

function MemoryRow({
  entry,
  expanded,
  onToggle,
}: {
  entry: MemoryEntry;
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <Fragment>
      <tr
        className="memories-row"
        onClick={onToggle}
        data-testid={`memories-row-${entry.key}`}
      >
        <td>{entry.role}</td>
        <td>{entry.slug}</td>
        <td>
          <span className={`memories-tier memories-tier--${entry.tier}`}>{entry.tier}</span>
        </td>
        <td>{entry.appliedCount}</td>
        <td>{entry.lastApplied ? formatDateTime(entry.lastApplied) : "—"}</td>
      </tr>
      {expanded && (
        <tr className="memories-row__detail">
          <td colSpan={5}>
            <pre className="memories-body">{entry.body}</pre>
          </td>
        </tr>
      )}
    </Fragment>
  );
}

export default function MemoriesView() {
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [role, setRole] = useState("");
  const [tier, setTier] = useState<MemoryFilters["tier"]>("all");
  const [textFilter, setTextFilter] = useState("");

  const load = useCallback(() => {
    return fetchMemories()
      .then((res) => {
        setMemories(res.memories);
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

  const sorted = sortMemories(memories);
  const filtered = filterMemories(sorted, { role, tier, text: textFilter });

  // Distinct roles present in the data, for the role filter dropdown.
  const roleOptions = Array.from(new Set(memories.map((m) => m.role))).sort();

  const filtersActive = role !== "" || tier !== "all" || textFilter.trim() !== "";

  return (
    <div className="memories-view">
      <header className="memories-header">
        <h1 className="memories-header__title">Memories</h1>
        {memories.length > 0 && (
          <span className="memories-header__count" data-testid="memories-count">{memories.length}</span>
        )}
        <button
          type="button"
          className="memories-refresh-btn"
          onClick={() => { void load(); }}
          disabled={loading}
        >
          {loading ? "Refreshing…" : "Refresh"}
        </button>
      </header>

      {fetchError && (
        <div className="memories-banner memories-banner--error">Failed to load memories: {fetchError}</div>
      )}

      <section className="memories-section">
        <h2 className="section-title">Role Memories</h2>
        {memories.length > 0 && (
          <div className="memories-filters">
            <select
              className="memories-filter-select"
              aria-label="Filter by role"
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <option value="">All roles</option>
              {roleOptions.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
            <select
              className="memories-filter-select"
              aria-label="Filter by tier"
              value={tier}
              onChange={(e) => setTier(e.target.value as MemoryFilters["tier"])}
            >
              <option value="all">All tiers</option>
              <option value="hot">Hot</option>
              <option value="fresh">Fresh</option>
              <option value="cold">Cold</option>
            </select>
            <input
              type="text"
              className="memories-filter-text"
              placeholder="Search role, slug, body…"
              aria-label="Search memories"
              value={textFilter}
              onChange={(e) => setTextFilter(e.target.value)}
            />
            {filtersActive && (
              <button
                type="button"
                className="memories-filter-clear"
                onClick={() => {
                  setRole("");
                  setTier("all");
                  setTextFilter("");
                }}
              >
                Clear filters
              </button>
            )}
          </div>
        )}
        {loading && memories.length === 0 ? (
          <p className="memories-status-text">Loading memories…</p>
        ) : memories.length === 0 ? (
          <p className="memories-status-text">No memories.</p>
        ) : filtered.length === 0 ? (
          <p className="memories-status-text">No memories match the current filters.</p>
        ) : (
          <table className="memories-table" aria-label="Role memories">
            <thead>
              <tr>
                <th>Role</th>
                <th>Slug</th>
                <th>Tier</th>
                <th>Applied</th>
                <th>Last Applied</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((m) => (
                <MemoryRow
                  key={m.key}
                  entry={m}
                  expanded={expandedKey === m.key}
                  onToggle={() => setExpandedKey((cur) => (cur === m.key ? null : m.key))}
                />
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
