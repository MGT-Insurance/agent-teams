import { useMemo, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useSnapshotContext } from "../../SnapshotContext.js";
import type { InitiativeNode } from "@agent-teams/shared";
import type { ActivityStatus } from "@agent-teams/shared";
import "./constellation.css";

// Stable deterministic polar layout: hash initiative id to a fixed angle,
// then assign radii in concentric rings so nodes don't collide.
function polarLayout(
  nodes: InitiativeNode[],
  cx: number,
  cy: number,
): Array<{ node: InitiativeNode; x: number; y: number }> {
  if (nodes.length === 0) return [];

  // Cheap stable hash: sum char codes modulo 360 → degrees.
  function stableAngle(id: string): number {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    return (h % 360) * (Math.PI / 180);
  }

  const minR = 80;
  const ringGap = 90;
  // Pack into rings: ring 0 fits 6, ring 1 fits 12, etc.
  const ringCapacity = (ring: number) => Math.max(6, (ring + 1) * 6);

  const rings: InitiativeNode[][] = [];
  let remaining = [...nodes];
  let ring = 0;
  while (remaining.length > 0) {
    const cap = ringCapacity(ring);
    rings.push(remaining.slice(0, cap));
    remaining = remaining.slice(cap);
    ring++;
  }

  const result: Array<{ node: InitiativeNode; x: number; y: number }> = [];
  for (let r = 0; r < rings.length; r++) {
    const radius = minR + r * ringGap;
    const ringNodes = rings[r];
    if (!ringNodes || ringNodes.length === 0) continue;
    // Evenly space nodes on the ring, but offset by each node's stable hash
    // so a node keeps the same rough quadrant even as count changes.
    const baseAngle = (2 * Math.PI) / ringNodes.length;
    ringNodes.forEach((n, i) => {
      const hashOffset = stableAngle(n.initiative.id);
      const angle = i * baseAngle + (hashOffset % baseAngle);
      result.push({
        node: n,
        x: cx + radius * Math.cos(angle),
        y: cy + radius * Math.sin(angle),
      });
    });
  }
  return result;
}

// Visual parameters keyed on activity.
const ACTIVITY_META: Record<
  ActivityStatus,
  { color: string; glowColor: string; r: number; cssClass: string; label: string }
> = {
  "needs-human": {
    color: "#f5a623",
    glowColor: "rgba(245,166,35,0.55)",
    r: 14,
    cssClass: "node--needs-human",
    label: "needs you",
  },
  busy: {
    color: "#4a9eff",
    glowColor: "rgba(74,158,255,0.45)",
    r: 12,
    cssClass: "node--busy",
    label: "busy",
  },
  delivered: {
    color: "#3ecf8e",
    glowColor: "rgba(62,207,142,0.35)",
    r: 11,
    cssClass: "node--delivered",
    label: "delivered",
  },
  idle: {
    color: "#4b5563",
    glowColor: "rgba(75,85,99,0.25)",
    r: 9,
    cssClass: "node--idle",
    label: "idle",
  },
  done: {
    color: "#2a2e35",
    glowColor: "rgba(42,46,53,0.15)",
    r: 8,
    cssClass: "node--done",
    label: "done",
  },
};

interface NodeProps {
  node: InitiativeNode;
  x: number;
  y: number;
  onNavigate: (id: string) => void;
}

function ConstellationNode({ node, x, y, onNavigate }: NodeProps) {
  const meta = ACTIVITY_META[node.activity];
  const isBusy = node.session?.status === "busy";
  const isBlocked = node.session?.state === "blocked";

  return (
    <g
      className={`constellation-node ${meta.cssClass}${isBusy ? " node--session-busy" : ""}${isBlocked ? " node--session-blocked" : ""}`}
      transform={`translate(${x},${y})`}
      role="button"
      tabIndex={0}
      aria-label={`${node.initiative.title} — ${meta.label}`}
      onClick={() => onNavigate(node.initiative.id)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onNavigate(node.initiative.id);
      }}
      style={{ cursor: "pointer" }}
    >
      {/* Outer glow ring for needs-human flare */}
      {node.activity === "needs-human" && (
        <circle
          className="node-flare"
          cx={0}
          cy={0}
          r={meta.r + 10}
          fill="none"
          stroke={meta.color}
          strokeWidth={1.5}
          opacity={0.6}
        />
      )}

      {/* Delivered ring */}
      {node.activity === "delivered" && (
        <circle
          cx={0}
          cy={0}
          r={meta.r + 6}
          fill="none"
          stroke={meta.color}
          strokeWidth={1}
          opacity={0.5}
          strokeDasharray="4 3"
        />
      )}

      {/* Glow filter applied via drop-shadow */}
      <circle
        className="node-core"
        cx={0}
        cy={0}
        r={meta.r}
        fill={meta.color}
        style={{ filter: `drop-shadow(0 0 ${meta.r * 0.8}px ${meta.glowColor})` }}
      />

      {/* Pulse ring for busy/needs-human */}
      {(node.activity === "busy" || node.activity === "needs-human") && (
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

      {/* Label below node */}
      <text
        className="node-label"
        x={0}
        y={meta.r + 14}
        textAnchor="middle"
        fill="var(--color-text-muted)"
        fontSize={10}
        fontFamily="var(--font-mono)"
      >
        {node.initiative.title.length > 20
          ? node.initiative.title.slice(0, 18) + "…"
          : node.initiative.title}
      </text>

      {/* Phase token */}
      <text
        className="node-phase"
        x={0}
        y={meta.r + 25}
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

export default function ConstellationView() {
  const { initiatives, connectionState, error, ts } = useSnapshotContext();
  const navigate = useNavigate();
  const svgRef = useRef<SVGSVGElement>(null);

  // Fixed viewport — nodes are stable across snapshots because layout is deterministic.
  const W = 800;
  const H = 600;
  const cx = W / 2;
  const cy = H / 2;

  const positioned = useMemo(
    () => polarLayout(initiatives, cx, cy),
    // Re-run only when initiative ids change, not on every tick (avoids jumpiness).
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [initiatives.map((n) => n.initiative.id).join(","), cx, cy],
  );

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

  if (initiatives.length === 0) {
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
    <div className="constellation-viewport">
      <svg
        ref={svgRef}
        className="constellation-svg"
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="xMidYMid meet"
        aria-label="Initiative constellation"
      >
        {/* Star field background dots */}
        <StarField seed={42} count={60} w={W} h={H} />

        {/* Central anchor */}
        <circle cx={cx} cy={cy} r={3} fill="var(--color-text-muted)" opacity={0.3} />
        <circle
          cx={cx}
          cy={cy}
          r={60}
          fill="none"
          stroke="var(--color-text-muted)"
          strokeWidth={1}
          opacity={0.08}
        />

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

      {connectionState === "reconnecting" && (
        <div className="constellation-overlay-banner">reconnecting…</div>
      )}
    </div>
  );
}

// Deterministic star field — same positions every render (seed-based PRNG).
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
      opacity: next() * 0.25 + 0.05,
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
