import { useMemo, useRef, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSnapshotContext } from "../../SnapshotContext.js";
import type { InitiativeNode, SessionState } from "@agent-teams/shared";
import type { ActivityStatus } from "@agent-teams/shared";
import "./constellation.css";

// ---------------------------------------------------------------------------
// Urgency-orbital layout
// ---------------------------------------------------------------------------
// Position encodes urgency: needs-human innermost, done outermost.
// Angle is a stable per-id hash so nodes never jump on refresh.
// Only radius changes as activity changes.
// ---------------------------------------------------------------------------

// Urgency levels: lower number = closer to center (more urgent).
const URGENCY_RADIUS_FRACTION: Record<ActivityStatus, number> = {
  "needs-human": 0.20, // innermost — pulled toward you
  busy: 0.35,
  delivered: 0.47,
  idle: 0.58,
  done: 0.70, // outer dim rim
};

// Orphans live at the rim with done nodes but visually distinct.
const ORPHAN_RADIUS_FRACTION = 0.73;

// Stable deterministic angle from an id string (0 to 2π).
function stableAngle(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return (h % 36000) * (Math.PI / 18000); // 0 to 2π with fine resolution
}

function urgencyOrbitalLayout(
  nodes: InitiativeNode[],
  orphans: SessionState[],
  cx: number,
  cy: number,
  shortSide: number,
): {
  initiatives: Array<{ node: InitiativeNode; x: number; y: number }>;
  orphans: Array<{ session: SessionState; x: number; y: number }>;
} {
  const initiatives = nodes.map((node) => {
    const fraction = URGENCY_RADIUS_FRACTION[node.activity];
    const radius = shortSide * fraction;
    const angle = stableAngle(node.initiative.id);
    return {
      node,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
    };
  });

  const orphanNodes = orphans.map((session) => {
    const radius = shortSide * ORPHAN_RADIUS_FRACTION;
    const angle = stableAngle(session.sessionId);
    return {
      session,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
    };
  });

  return { initiatives, orphans: orphanNodes };
}

// ---------------------------------------------------------------------------
// Visual metadata for activity states
// ---------------------------------------------------------------------------

const ACTIVITY_META: Record<
  ActivityStatus,
  { color: string; glowColor: string; r: number; cssClass: string; label: string; opacity: number }
> = {
  "needs-human": {
    color: "#ff6b35",
    glowColor: "rgba(255,107,53,0.7)",
    r: 18,
    cssClass: "node--needs-human",
    label: "needs you",
    opacity: 1,
  },
  busy: {
    color: "#4a9eff",
    glowColor: "rgba(74,158,255,0.5)",
    r: 14,
    cssClass: "node--busy",
    label: "busy",
    opacity: 1,
  },
  delivered: {
    color: "#3ecf8e",
    glowColor: "rgba(62,207,142,0.4)",
    r: 14,
    cssClass: "node--delivered",
    label: "delivered",
    opacity: 0.95,
  },
  idle: {
    color: "#4b5563",
    glowColor: "rgba(75,85,99,0.25)",
    r: 11,
    cssClass: "node--idle",
    label: "idle",
    opacity: 0.65,
  },
  done: {
    color: "#323840",
    glowColor: "rgba(50,56,64,0.15)",
    r: 9,
    cssClass: "node--done",
    label: "done",
    opacity: 0.3,
  },
};

// Tether line alpha tracks urgency: needs-human brightest, done faintest.
const TETHER_OPACITY: Record<ActivityStatus, number> = {
  "needs-human": 0.55,
  busy: 0.38,
  delivered: 0.28,
  idle: 0.14,
  done: 0.07,
};

// ---------------------------------------------------------------------------
// Tether line (center → node)
// ---------------------------------------------------------------------------

interface TetherProps {
  cx: number;
  cy: number;
  x: number;
  y: number;
  activity: ActivityStatus;
  color: string;
}

function TetherLine({ cx, cy, x, y, activity, color }: TetherProps) {
  const opacity = TETHER_OPACITY[activity];
  // Shorten the line so it stops at the node edge, not node center.
  const meta = ACTIVITY_META[activity];
  const dx = x - cx;
  const dy = y - cy;
  const dist = Math.sqrt(dx * dx + dy * dy) || 1;
  const stopX = x - (dx / dist) * (meta.r + 4);
  const stopY = y - (dy / dist) * (meta.r + 4);

  return (
    <line
      x1={cx}
      y1={cy}
      x2={stopX}
      y2={stopY}
      stroke={color}
      strokeWidth={activity === "needs-human" ? 1.5 : 1}
      opacity={opacity}
      strokeLinecap="round"
    />
  );
}

// ---------------------------------------------------------------------------
// Urgency rings (decorative reference circles around center)
// ---------------------------------------------------------------------------

interface UrgencyRingsProps {
  cx: number;
  cy: number;
  shortSide: number;
}

function UrgencyRings({ cx, cy, shortSide }: UrgencyRingsProps) {
  const rings: Array<{ fraction: number; label: string; opacity: number }> = [
    { fraction: URGENCY_RADIUS_FRACTION["needs-human"], label: "needs you", opacity: 0.12 },
    { fraction: URGENCY_RADIUS_FRACTION.busy, label: "busy", opacity: 0.07 },
    { fraction: URGENCY_RADIUS_FRACTION.delivered, label: "delivered", opacity: 0.06 },
    { fraction: URGENCY_RADIUS_FRACTION.idle, label: "idle", opacity: 0.05 },
    { fraction: URGENCY_RADIUS_FRACTION.done, label: "done", opacity: 0.04 },
  ];

  return (
    <>
      {rings.map(({ fraction, label, opacity }) => (
        <circle
          key={label}
          cx={cx}
          cy={cy}
          r={shortSide * fraction}
          fill="none"
          stroke="rgba(255,255,255,0.55)"
          strokeWidth={1}
          opacity={opacity}
          strokeDasharray="3 6"
        />
      ))}
    </>
  );
}

// ---------------------------------------------------------------------------
// Center anchor — "YOU"
// ---------------------------------------------------------------------------

interface CenterAnchorProps {
  cx: number;
  cy: number;
}

function CenterAnchor({ cx, cy }: CenterAnchorProps) {
  return (
    <g className="constellation-center">
      {/* Subtle radiant glow */}
      <circle
        cx={cx}
        cy={cy}
        r={22}
        fill="radial-gradient()"
        className="center-glow"
      />
      {/* Core dot */}
      <circle
        cx={cx}
        cy={cy}
        r={6}
        fill="#e2e8f0"
        opacity={0.9}
        style={{ filter: "drop-shadow(0 0 8px rgba(226,232,240,0.6))" }}
      />
      {/* Crosshair lines */}
      <line x1={cx - 14} y1={cy} x2={cx - 8} y2={cy} stroke="#e2e8f0" strokeWidth={1} opacity={0.4} />
      <line x1={cx + 8} y1={cy} x2={cx + 14} y2={cy} stroke="#e2e8f0" strokeWidth={1} opacity={0.4} />
      <line x1={cx} y1={cy - 14} x2={cx} y2={cy - 8} stroke="#e2e8f0" strokeWidth={1} opacity={0.4} />
      <line x1={cx} y1={cy + 8} x2={cx} y2={cy + 14} stroke="#e2e8f0" strokeWidth={1} opacity={0.4} />
      {/* "YOU" label */}
      <text
        x={cx}
        y={cy + 26}
        textAnchor="middle"
        fill="#e2e8f0"
        fontSize={9}
        fontFamily="var(--font-mono)"
        letterSpacing="0.12em"
        opacity={0.55}
      >
        YOU
      </text>
    </g>
  );
}

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

  // Title: truncate to fit, with more room than before (legibility).
  const title =
    node.initiative.title.length > 26
      ? node.initiative.title.slice(0, 24) + "…"
      : node.initiative.title;

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
      {/* Outer flare ring for needs-human — most prominent animation */}
      {needsHuman && (
        <circle
          className="node-flare"
          cx={0}
          cy={0}
          r={meta.r + 14}
          fill="none"
          stroke={meta.color}
          strokeWidth={2}
          opacity={0.7}
        />
      )}

      {/* Secondary flare ring — needs-human only */}
      {needsHuman && (
        <circle
          className="node-flare-2"
          cx={0}
          cy={0}
          r={meta.r + 26}
          fill="none"
          stroke={meta.color}
          strokeWidth={1}
          opacity={0.35}
        />
      )}

      {/* Delivered outer ring — dashed, subtle */}
      {node.activity === "delivered" && (
        <circle
          cx={0}
          cy={0}
          r={meta.r + 8}
          fill="none"
          stroke={meta.color}
          strokeWidth={1}
          opacity={0.4}
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
        opacity={meta.opacity}
        style={{ filter: `drop-shadow(0 0 ${meta.r * 0.9}px ${meta.glowColor})` }}
      />

      {/* Pulse ring for busy / needs-human */}
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

      {/* needs-human badge: "!" */}
      {needsHuman && (
        <g className="node-badge node-badge--needs-human" data-badge="needs-human">
          <circle cx={meta.r} cy={-(meta.r)} r={8} fill="#ff6b35" stroke="rgba(0,0,0,0.3)" strokeWidth={1} />
          <text
            x={meta.r}
            y={-(meta.r) + 4}
            textAnchor="middle"
            fill="white"
            fontSize={10}
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
            x={meta.r - 3}
            y={-(meta.r + 8)}
            width={18}
            height={11}
            rx={3}
            fill="#a78bfa"
          />
          <text
            x={meta.r + 6}
            y={-(meta.r + 8) + 8}
            textAnchor="middle"
            fill="white"
            fontSize={7.5}
            fontWeight="bold"
            fontFamily="var(--font-mono)"
            aria-hidden="true"
          >
            PR
          </text>
        </g>
      )}

      {/* Title label — larger, higher contrast */}
      <text
        className="node-label"
        x={0}
        y={meta.r + 18}
        textAnchor="middle"
        fill={needsHuman ? "#ffcbb3" : "rgba(226,232,240,0.85)"}
        fontSize={11}
        fontFamily="var(--font-mono)"
        fontWeight={needsHuman ? "600" : "400"}
      >
        {title}
      </text>

      {/* Phase token — readable, slightly below label */}
      <text
        className="node-phase"
        x={0}
        y={meta.r + 31}
        textAnchor="middle"
        fill={meta.color}
        fontSize={9.5}
        fontFamily="var(--font-mono)"
        opacity={needsHuman ? 0.9 : 0.65}
        letterSpacing="0.04em"
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
        opacity={0.6}
      />

      <text
        className="node-label"
        x={0}
        y={r + 15}
        textAnchor="middle"
        fill="rgba(156,163,175,0.7)"
        fontSize={9.5}
        fontFamily="var(--font-mono)"
      >
        {label.length > 18 ? label.slice(0, 16) + "…" : label}
      </text>

      <text
        x={0}
        y={r + 25}
        textAnchor="middle"
        fill={color}
        fontSize={7.5}
        fontFamily="var(--font-mono)"
        opacity={0.35}
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
  const entries: Array<{
    color: string;
    shape: "circle" | "dashed";
    badge?: string;
    label: string;
    cssClass?: string;
    dim?: boolean;
  }> = [
    { color: "#ff6b35", shape: "circle", badge: "!", label: "needs your input", cssClass: "legend-entry--needs-human" },
    { color: "#a78bfa", shape: "circle", badge: "PR", label: "has open PR", cssClass: "legend-entry--pr" },
    { color: "#4a9eff", shape: "circle", label: "busy / working", cssClass: "legend-entry--busy" },
    { color: "#3ecf8e", shape: "circle", label: "delivered", cssClass: "legend-entry--delivered" },
    { color: "#4b5563", shape: "circle", label: "idle", cssClass: "legend-entry--idle", dim: true },
    { color: "#323840", shape: "circle", label: "done", cssClass: "legend-entry--done", dim: true },
    { color: "#6b7280", shape: "dashed", label: "unregistered session", cssClass: "legend-entry--orphan", dim: true },
  ];

  return (
    <div
      className="constellation-legend"
      aria-label="Constellation legend — inner orbit needs you, outer orbit done"
      data-testid="constellation-legend"
    >
      <div className="constellation-legend__title">orbit key · inner = urgent</div>
      {entries.map((e) => (
        <div
          key={e.label}
          className={`constellation-legend__entry ${e.cssClass ?? ""}${e.dim ? " legend-entry--dim" : ""}`}
        >
          <svg width={22} height={22} className="constellation-legend__icon" aria-hidden="true">
            {e.shape === "dashed" ? (
              <circle cx={11} cy={11} r={7} fill="none" stroke={e.color} strokeWidth={1.5} strokeDasharray="3 2" opacity={0.7} />
            ) : (
              <circle cx={11} cy={11} r={7} fill={e.color} opacity={e.dim ? 0.4 : 1} />
            )}
            {e.badge === "!" && (
              <>
                <circle cx={18} cy={5} r={4.5} fill="#ff6b35" />
                <text x={18} y={8.5} textAnchor="middle" fill="white" fontSize={6.5} fontWeight="bold" fontFamily="monospace">!</text>
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

  // Layout: re-run when initiative ids, activity, or orphan session ids change.
  // Activity changes affect radius, so we include it in the key.
  const layoutKey =
    initiatives.map((n) => `${n.initiative.id}:${n.activity}`).join(",") +
    "|" +
    unmatchedSessions.map((s) => s.sessionId).join(",");

  const layoutCacheRef = useRef<{
    key: string;
    result: ReturnType<typeof urgencyOrbitalLayout>;
  } | null>(null);

  const { positioned, positionedOrphans } = useMemo(() => {
    const cache = layoutCacheRef.current;
    if (cache && cache.key === layoutKey) {
      return { positioned: cache.result.initiatives, positionedOrphans: cache.result.orphans };
    }
    const result = urgencyOrbitalLayout(initiatives, unmatchedSessions, cx, cy, shortSide);
    layoutCacheRef.current = { key: layoutKey, result };
    return { positioned: result.initiatives, positionedOrphans: result.orphans };
  }, [initiatives, unmatchedSessions, layoutKey, cx, cy, shortSide]);

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
        <StarField seed={42} count={90} w={W} h={H} />

        {/* Urgency reference rings (dashed orbital guides) */}
        <UrgencyRings cx={cx} cy={cy} shortSide={shortSide} />

        {/* Tether lines from center to each initiative — drawn before nodes */}
        {positioned.map(({ node, x, y }) => (
          <TetherLine
            key={`tether-${node.initiative.id}`}
            cx={cx}
            cy={cy}
            x={x}
            y={y}
            activity={node.activity}
            color={ACTIVITY_META[node.activity].color}
          />
        ))}

        {/* Tether lines for orphan sessions (dashed, very faint) */}
        {positionedOrphans.map(({ session, x, y }) => (
          <line
            key={`tether-orphan-${session.sessionId}`}
            x1={cx}
            y1={cy}
            x2={x}
            y2={y}
            stroke="#6b7280"
            strokeWidth={0.75}
            opacity={0.1}
            strokeDasharray="3 4"
          />
        ))}

        {/* Center anchor — "YOU" */}
        <CenterAnchor cx={cx} cy={cy} />

        {/* Orphan nodes (rim) */}
        {positionedOrphans.map(({ session, x, y }) => (
          <OrphanNode
            key={session.sessionId}
            session={session}
            x={x}
            y={y}
          />
        ))}

        {/* Initiative nodes — drawn last so they sit on top of tethers */}
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
      opacity: next() * 0.18 + 0.04,
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
