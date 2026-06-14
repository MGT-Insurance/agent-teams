import { useMemo, useRef, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSnapshotContext } from "../../SnapshotContext.js";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";
import type { ActivityStatus } from "@agent-teams/shared";
import "./constellation.css";

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

// Stable deterministic polar layout scaled to the given viewport.
// minRFraction and ringGapFraction are fractions of the shorter viewport side.
function polarLayout(
  nodes: InitiativeNode[],
  orphans: SessionState[],
  cx: number,
  cy: number,
  shortSide: number,
): {
  initiatives: Array<{ node: InitiativeNode; x: number; y: number }>;
  orphans: Array<{ session: SessionState; x: number; y: number }>;
} {
  // Cheap stable hash → angle in radians
  function stableAngle(id: string): number {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    return (h % 360) * (Math.PI / 180);
  }

  // Scale rings relative to viewport — fill the space with breathing room.
  const minR = shortSide * 0.18;
  const ringGap = shortSide * 0.22;

  // Pack into rings: ring 0 fits 6, ring 1 fits 12, etc.
  const ringCapacity = (ring: number) => Math.max(6, (ring + 1) * 6);

  function layOutNodes<T>(
    items: T[],
    getId: (item: T) => string,
    ringOffset = 0,
  ): Array<{ item: T; x: number; y: number }> {
    if (items.length === 0) return [];
    const rings: T[][] = [];
    let remaining = [...items];
    let ring = ringOffset;
    while (remaining.length > 0) {
      const cap = ringCapacity(ring - ringOffset);
      rings.push(remaining.slice(0, cap));
      remaining = remaining.slice(cap);
      ring++;
    }
    const result: Array<{ item: T; x: number; y: number }> = [];
    for (let r = 0; r < rings.length; r++) {
      const radius = minR + r * ringGap;
      const ringNodes = rings[r];
      if (!ringNodes || ringNodes.length === 0) continue;
      const baseAngle = (2 * Math.PI) / ringNodes.length;
      ringNodes.forEach((n, i) => {
        const hashOffset = stableAngle(getId(n));
        const angle = i * baseAngle + (hashOffset % baseAngle);
        result.push({ item: n, x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle) });
      });
    }
    return result;
  }

  const initiativesRings = Math.ceil(nodes.length / 6);

  const initiativePositions = layOutNodes(
    nodes,
    (n) => n.initiative.id,
    0,
  );

  // Orphans go in the next ring out so they form an outer halo.
  const orphanRingOffset = initiativesRings;
  const orphanPositions = layOutNodes(
    orphans,
    (s) => s.sessionId,
    orphanRingOffset,
  );

  return {
    initiatives: initiativePositions.map(({ item, x, y }) => ({ node: item, x, y })),
    orphans: orphanPositions.map(({ item, x, y }) => ({ session: item, x, y })),
  };
}

// ---------------------------------------------------------------------------
// Visual metadata for activity states
// ---------------------------------------------------------------------------

const ACTIVITY_META: Record<
  ActivityStatus,
  { color: string; glowColor: string; r: number; cssClass: string; label: string }
> = {
  "needs-human": {
    color: "#ff6b35",
    glowColor: "rgba(255,107,53,0.6)",
    r: 16,
    cssClass: "node--needs-human",
    label: "needs you",
  },
  busy: {
    color: "#4a9eff",
    glowColor: "rgba(74,158,255,0.45)",
    r: 13,
    cssClass: "node--busy",
    label: "busy",
  },
  delivered: {
    color: "#3ecf8e",
    glowColor: "rgba(62,207,142,0.35)",
    r: 13,
    cssClass: "node--delivered",
    label: "delivered",
  },
  idle: {
    color: "#4b5563",
    glowColor: "rgba(75,85,99,0.25)",
    r: 10,
    cssClass: "node--idle",
    label: "idle",
  },
  done: {
    color: "#2a2e35",
    glowColor: "rgba(42,46,53,0.15)",
    r: 9,
    cssClass: "node--done",
    label: "done",
  },
};

// ---------------------------------------------------------------------------
// Initiative node
// ---------------------------------------------------------------------------

interface NodeProps {
  node: InitiativeNode;
  x: number;
  y: number;
  onNavigate: (id: string) => void;
}

function ConstellationNode({ node, x, y, onNavigate }: NodeProps) {
  const meta = ACTIVITY_META[node.activity];
  const isBusy = node.session?.status === "busy";
  const hasPr = node.initiative.prUrl !== null;
  const needsHuman = node.activity === "needs-human";

  return (
    <g
      className={`constellation-node ${meta.cssClass}${isBusy ? " node--session-busy" : ""}`}
      transform={`translate(${x},${y})`}
      role="button"
      tabIndex={0}
      aria-label={`${node.initiative.title} — ${meta.label}`}
      data-initiative-id={node.initiative.id}
      data-activity={node.activity}
      data-has-pr={hasPr ? "true" : "false"}
      onClick={() => onNavigate(node.initiative.id)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onNavigate(node.initiative.id);
      }}
      style={{ cursor: "pointer" }}
    >
      {/* Outer glow ring for needs-human flare */}
      {needsHuman && (
        <circle
          className="node-flare"
          cx={0}
          cy={0}
          r={meta.r + 12}
          fill="none"
          stroke={meta.color}
          strokeWidth={2}
          opacity={0.6}
        />
      )}

      {/* Delivered outer ring */}
      {node.activity === "delivered" && (
        <circle
          cx={0}
          cy={0}
          r={meta.r + 7}
          fill="none"
          stroke={meta.color}
          strokeWidth={1}
          opacity={0.5}
          strokeDasharray="4 3"
        />
      )}

      {/* Core circle */}
      <circle
        className="node-core"
        cx={0}
        cy={0}
        r={meta.r}
        fill={meta.color}
        style={{ filter: `drop-shadow(0 0 ${meta.r * 0.8}px ${meta.glowColor})` }}
      />

      {/* Pulse ring for busy/needs-human */}
      {(node.activity === "busy" || needsHuman) && (
        <circle
          className="node-pulse"
          cx={0}
          cy={0}
          r={meta.r}
          fill="none"
          stroke={meta.color}
          strokeWidth={2}
        />
      )}

      {/* needs-human badge: "!" exclamation */}
      {needsHuman && (
        <g className="node-badge node-badge--needs-human" data-badge="needs-human">
          <circle cx={meta.r - 2} cy={-(meta.r - 2)} r={7} fill="#ff6b35" />
          <text
            x={meta.r - 2}
            y={-(meta.r - 2) + 4}
            textAnchor="middle"
            fill="white"
            fontSize={9}
            fontWeight="bold"
            fontFamily="var(--font-mono)"
            aria-hidden="true"
          >
            !
          </text>
        </g>
      )}

      {/* PR badge */}
      {hasPr && !needsHuman && (
        <g className="node-badge node-badge--pr" data-badge="pr">
          <rect
            x={meta.r - 4}
            y={-(meta.r + 6)}
            width={16}
            height={10}
            rx={3}
            fill="#a78bfa"
          />
          <text
            x={meta.r + 4}
            y={-(meta.r + 6) + 8}
            textAnchor="middle"
            fill="white"
            fontSize={7}
            fontWeight="bold"
            fontFamily="var(--font-mono)"
            aria-hidden="true"
          >
            PR
          </text>
        </g>
      )}

      {/* Label below node */}
      <text
        className="node-label"
        x={0}
        y={meta.r + 16}
        textAnchor="middle"
        fill="var(--color-text-muted)"
        fontSize={10}
        fontFamily="var(--font-mono)"
      >
        {node.initiative.title.length > 22
          ? node.initiative.title.slice(0, 20) + "…"
          : node.initiative.title}
      </text>

      {/* Phase token */}
      <text
        className="node-phase"
        x={0}
        y={meta.r + 28}
        textAnchor="middle"
        fill={meta.color}
        fontSize={8.5}
        fontFamily="var(--font-mono)"
        opacity={0.7}
      >
        {node.phase}
      </text>
    </g>
  );
}

// ---------------------------------------------------------------------------
// Orphan (unregistered background session) node
// ---------------------------------------------------------------------------

interface OrphanNodeProps {
  session: SessionState;
  x: number;
  y: number;
}

function OrphanNode({ session, x, y }: OrphanNodeProps) {
  const label = session.name ?? session.sessionId.slice(0, 8);
  const r = 8;
  const color = "#6b7280";

  return (
    <g
      className="constellation-node node--orphan"
      transform={`translate(${x},${y})`}
      aria-label={`Unregistered session: ${label}`}
      data-orphan-session-id={session.sessionId}
      data-activity="orphan"
    >
      {/* Dashed hollow circle — distinct from initiative nodes */}
      <circle
        cx={0}
        cy={0}
        r={r}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeDasharray="3 2"
        opacity={0.7}
      />

      <text
        className="node-label"
        x={0}
        y={r + 14}
        textAnchor="middle"
        fill={color}
        fontSize={9}
        fontFamily="var(--font-mono)"
        opacity={0.6}
      >
        {label.length > 18 ? label.slice(0, 16) + "…" : label}
      </text>

      <text
        x={0}
        y={r + 24}
        textAnchor="middle"
        fill={color}
        fontSize={7.5}
        fontFamily="var(--font-mono)"
        opacity={0.4}
      >
        unregistered
      </text>
    </g>
  );
}

// ---------------------------------------------------------------------------
// Legend
// ---------------------------------------------------------------------------

function ConstellationLegend() {
  const entries: Array<{ color: string; shape: "circle" | "dashed"; badge?: string; label: string; cssClass?: string }> = [
    { color: "#ff6b35", shape: "circle", badge: "!", label: "needs your input", cssClass: "legend-entry--needs-human" },
    { color: "#a78bfa", shape: "circle", badge: "PR", label: "has open PR", cssClass: "legend-entry--pr" },
    { color: "#4a9eff", shape: "circle", label: "busy / working", cssClass: "legend-entry--busy" },
    { color: "#3ecf8e", shape: "circle", label: "delivered", cssClass: "legend-entry--delivered" },
    { color: "#4b5563", shape: "circle", label: "idle", cssClass: "legend-entry--idle" },
    { color: "#2a2e35", shape: "circle", label: "done", cssClass: "legend-entry--done" },
    { color: "#6b7280", shape: "dashed", label: "unregistered session", cssClass: "legend-entry--orphan" },
  ];

  return (
    <div className="constellation-legend" aria-label="Constellation legend" data-testid="constellation-legend">
      <div className="constellation-legend__title">legend</div>
      {entries.map((e) => (
        <div key={e.label} className={`constellation-legend__entry ${e.cssClass ?? ""}`}>
          <svg width={22} height={22} className="constellation-legend__icon" aria-hidden="true">
            {e.shape === "dashed" ? (
              <circle cx={11} cy={11} r={7} fill="none" stroke={e.color} strokeWidth={1.5} strokeDasharray="3 2" opacity={0.8} />
            ) : (
              <circle cx={11} cy={11} r={7} fill={e.color} />
            )}
            {e.badge === "!" && (
              <>
                <circle cx={18} cy={5} r={4} fill="#ff6b35" />
                <text x={18} y={8} textAnchor="middle" fill="white" fontSize={6} fontWeight="bold" fontFamily="monospace">!</text>
              </>
            )}
            {e.badge === "PR" && (
              <>
                <rect x={14} y={2} width={8} height={6} rx={1.5} fill="#a78bfa" />
                <text x={18} y={8} textAnchor="middle" fill="white" fontSize={4.5} fontWeight="bold" fontFamily="monospace">PR</text>
              </>
            )}
          </svg>
          <span>{e.label}</span>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main view
// ---------------------------------------------------------------------------

export default function ConstellationView() {
  const { initiatives, unmatchedSessions, connectionState, error, ts } = useSnapshotContext();
  const navigate = useNavigate();

  // Track actual SVG render size for responsive layout.
  const containerRef = useRef<HTMLDivElement>(null);
  const [viewSize, setViewSize] = useState({ w: 900, h: 700 });

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const obs = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const { width, height } = entry.contentRect;
      if (width > 50 && height > 50) {
        setViewSize({ w: Math.round(width), h: Math.round(height) });
      }
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  const W = viewSize.w;
  const H = viewSize.h;
  const cx = W / 2;
  const cy = H / 2;
  const shortSide = Math.min(W, H);

  // Layout cache: re-run only when initiative ids or orphan session ids change.
  const layoutCacheRef = useRef<{
    key: string;
    result: ReturnType<typeof polarLayout>;
  } | null>(null);

  const idKey =
    initiatives.map((n) => n.initiative.id).join(",") +
    "|" +
    unmatchedSessions.map((s) => s.sessionId).join(",");

  const { positioned, positionedOrphans } = useMemo(() => {
    const cache = layoutCacheRef.current;
    if (cache && cache.key === idKey) {
      return { positioned: cache.result.initiatives, positionedOrphans: cache.result.orphans };
    }
    const result = polarLayout(initiatives, unmatchedSessions, cx, cy, shortSide);
    layoutCacheRef.current = { key: idKey, result };
    return { positioned: result.initiatives, positionedOrphans: result.orphans };
  }, [initiatives, unmatchedSessions, idKey, cx, cy, shortSide]);

  const handleNavigate = (id: string) => {
    navigate(`/initiative/${id}`);
  };

  if (connectionState === "error") {
    return (
      <div className="constellation-status">
        <span className="constellation-status__icon">!</span>
        <span>{error ?? "Connection error"}</span>
      </div>
    );
  }

  if (connectionState === "connecting" && initiatives.length === 0) {
    return (
      <div className="constellation-status">
        <span className="constellation-status__spinner" />
        <span>Connecting…</span>
      </div>
    );
  }

  if (initiatives.length === 0 && unmatchedSessions.length === 0) {
    return (
      <div className="constellation-status">
        <span>No initiatives active</span>
        {ts !== null && (
          <span className="constellation-status__ts">
            Last update: {new Date(ts).toLocaleTimeString()}
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="constellation-viewport" ref={containerRef}>
      <svg
        className="constellation-svg"
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="xMidYMid meet"
        aria-label="Initiative constellation"
        data-node-count={positioned.length}
        data-orphan-count={positionedOrphans.length}
      >
        {/* Star field background */}
        <StarField seed={42} count={80} w={W} h={H} />

        {/* Central anchor */}
        <circle cx={cx} cy={cy} r={3} fill="var(--color-text-muted)" opacity={0.3} />
        <circle
          cx={cx}
          cy={cy}
          r={shortSide * 0.14}
          fill="none"
          stroke="var(--color-text-muted)"
          strokeWidth={1}
          opacity={0.06}
        />

        {/* Orphan nodes (outer halo) */}
        {positionedOrphans.map(({ session, x, y }) => (
          <OrphanNode
            key={session.sessionId}
            session={session}
            x={x}
            y={y}
          />
        ))}

        {/* Initiative nodes */}
        {positioned.map(({ node, x, y }) => (
          <ConstellationNode
            key={node.initiative.id}
            node={node}
            x={x}
            y={y}
            onNavigate={handleNavigate}
          />
        ))}
      </svg>

      <ConstellationLegend />

      {connectionState === "reconnecting" && (
        <div className="constellation-overlay-banner">reconnecting…</div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Star field
// ---------------------------------------------------------------------------

function StarField({ seed, count, w, h }: { seed: number; count: number; w: number; h: number }) {
  const stars = useMemo(() => {
    let s = seed;
    const next = () => {
      s = (s * 1664525 + 1013904223) >>> 0;
      return s / 0xffffffff;
    };
    return Array.from({ length: count }, (_, i) => ({
      key: i,
      x: next() * w,
      y: next() * h,
      r: next() * 1.2 + 0.3,
      opacity: next() * 0.22 + 0.05,
    }));
  }, [seed, count, w, h]);

  return (
    <>
      {stars.map((s) => (
        <circle key={s.key} cx={s.x} cy={s.y} r={s.r} fill="white" opacity={s.opacity} />
      ))}
    </>
  );
}
