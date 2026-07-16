import { useCallback, useEffect, useMemo, useState } from "react";
import type { MemoryEntry } from "@agent-teams/shared";
import { fetchMemories } from "../../lib/api.js";
import "./memories.css";

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

// Client-side filter predicate (agent-teams-hvje.6): scoped to the memories
// of a single already-selected role — role is a navigation choice now, not
// a filter dimension.
export interface MemoryFilters {
  tier: "all" | "hot" | "fresh" | "cold";
  text: string; // matched case-insensitively against slug/body
}

export function filterMemories(entries: MemoryEntry[], filters: MemoryFilters): MemoryEntry[] {
  const text = filters.text.trim().toLowerCase();
  return entries.filter((m) => {
    if (filters.tier !== "all" && m.tier !== filters.tier) return false;
    if (text) {
      const haystack = `${m.slug} ${m.body}`.toLowerCase();
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

function MemoryCard({ entry }: { entry: MemoryEntry }) {
  return (
    <li className="memories-card" data-testid={`memories-card-${entry.key}`}>
      <div className="memories-card__meta">
        <span className="memories-card__slug">{entry.slug}</span>
        <span className={`memories-tier memories-tier--${entry.tier}`}>{entry.tier}</span>
        <span className="memories-card__applied">Applied {entry.appliedCount}×</span>
        <span className="memories-card__last-applied">
          {entry.lastApplied ? `Last applied ${formatDateTime(entry.lastApplied)}` : "Never applied"}
        </span>
      </div>
      <pre className="memories-card__body">{entry.body}</pre>
    </li>
  );
}

export default function MemoriesView() {
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [selectedRoleRaw, setSelectedRoleRaw] = useState<string | null>(null);
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

  // Group by role — never interleaved in the rendered output.
  const memoriesByRole = useMemo(() => {
    const map = new Map<string, MemoryEntry[]>();
    for (const m of memories) {
      const list = map.get(m.role);
      if (list) list.push(m);
      else map.set(m.role, [m]);
    }
    return map;
  }, [memories]);

  const roleOptions = useMemo(() => Array.from(memoriesByRole.keys()).sort(), [memoriesByRole]);

  // Default: first role alphabetically. Falls back cleanly if the previously
  // selected role disappears from a refetch.
  const selectedRole = selectedRoleRaw !== null && roleOptions.includes(selectedRoleRaw)
    ? selectedRoleRaw
    : roleOptions[0] ?? null;

  const roleMemories = selectedRole ? memoriesByRole.get(selectedRole) ?? [] : [];
  const sorted = sortMemories(roleMemories);
  const filtered = filterMemories(sorted, { tier, text: textFilter });

  const filtersActive = tier !== "all" || textFilter.trim() !== "";

  return (
    <div className="memories-view">
      <header className="memories-header">
        <h1 className="memories-header__title">Memories</h1>
        {memories.length > 0 && (
          <span className="memories-header__count" data-testid="memories-count">
            {roleOptions.length} role{roleOptions.length === 1 ? "" : "s"}, {memories.length} memor{memories.length === 1 ? "y" : "ies"}
          </span>
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

      {loading && memories.length === 0 ? (
        <p className="memories-status-text">Loading memories…</p>
      ) : memories.length === 0 ? (
        <p className="memories-status-text">No memories.</p>
      ) : (
        <div className="memories-layout">
          <aside className="memories-sidebar">
            <ul className="memories-role-list">
              {roleOptions.map((r) => (
                <li key={r}>
                  <button
                    type="button"
                    className={`memories-role-item${r === selectedRole ? " memories-role-item--active" : ""}`}
                    onClick={() => {
                      setSelectedRoleRaw(r);
                      setTier("all");
                      setTextFilter("");
                    }}
                    data-testid={`memories-role-${r}`}
                  >
                    <span className="memories-role-name">{r}</span>
                    <span className="memories-role-count">{memoriesByRole.get(r)!.length}</span>
                  </button>
                </li>
              ))}
            </ul>
          </aside>

          <section className="memories-detail">
            <div className="memories-detail-header">
              <h2 className="memories-detail-header__title">{selectedRole}</h2>
              <span className="memories-detail-header__count">
                {roleMemories.length} memor{roleMemories.length === 1 ? "y" : "ies"}
              </span>
            </div>

            <div className="memories-filters">
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
                placeholder="Search this role's memories…"
                aria-label="Search memories"
                value={textFilter}
                onChange={(e) => setTextFilter(e.target.value)}
              />
              {filtersActive && (
                <button
                  type="button"
                  className="memories-filter-clear"
                  onClick={() => {
                    setTier("all");
                    setTextFilter("");
                  }}
                >
                  Clear filters
                </button>
              )}
            </div>

            {filtered.length === 0 ? (
              <p className="memories-status-text">No memories match the current filters.</p>
            ) : (
              <ul className="memories-list">
                {filtered.map((m) => (
                  <MemoryCard key={m.key} entry={m} />
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
