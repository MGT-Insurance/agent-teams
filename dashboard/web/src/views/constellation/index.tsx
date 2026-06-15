import { useMemo, useRef, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSnapshotContext } from "../../SnapshotContext.js";
import type { InitiativeNode, SessionState, DeliveryStatus, NeedsHumanFlavor } from "@agent-teams/shared";
import "./constellation.css";

// ---------------------------------------------------------------------------
// Derived urgency — the single dimension that drives orbital radius + motion.
// Computed from the two-dimension model (delivery x activity x needsHuman).
// ---------------------------------------------------------------------------

type UrgencyTier = "needs-human" | "working" | "idle" | "done";

function deriveUrgency(node: InitiativeNode): UrgencyTier {
  if (node.needsHuman !== false) return "needs-human";
  // Closed/merged → done
  if (node.delivery === "merged") return "done";
  const s = node.initiative.status.toLowerCase();
  if (s === "closed" || s === "done") return "done";
  // Live working session → working
  const isWorking =
    node.session !== null &&
    (node.session.status === "busy" || node.session.state === "working");
  if (isWorking) return "working";
  return "idle";
}

// ---------------------------------------------------------------------------
// Urgency-orbital layout
// ---------------------------------------------------------------------------
// Position encodes urgency: needs-human innermost, done outermost.
// Angle is a stable per-id hash so nodes never jump on refresh.
// Only radius changes as urgency changes.
// ---------------------------------------------------------------------------

const URGENCY_RADIUS_FRACTION: Record<UrgencyTier, number> = {
  "needs-human": 0.20, // innermost — pulled toward you
  working: 0.35,
  idle: 0.55,
  done: 0.70, // outer dim rim
};

// Orphans live at the rim with done nodes but visually distinct.
const ORPHAN_RADIUS_FRACTION = 0.73;

// Stable deterministic hash → [0, 1) used for sort-order and jitter.
function stableHash(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return h / 0xffffffff;
}

// Lay out a list of items evenly around a circle at the given radius.
// Items are sorted by their hash (stable order across refreshes), then
// assigned angle = i*(2π/n) + small per-id jitter so the spacing is
// guaranteed (no two nodes < 2π/n apart) but the exact positions feel
// organic rather than mechanical.
function evenTierAngles(ids: string[]): Map<string, number> {
  const n = ids.length;
  if (n === 0) return new Map();

  // Sort by hash for a stable, deterministic order.
  const sorted = [...ids].sort((a, b) => stableHash(a) - stableHash(b));

  const result = new Map<string, number>();
  const slice = (2 * Math.PI) / n;
  // Max jitter: ±20% of the slice, so nodes can't get closer than 60% of a slice.
  const maxJitter = slice * 0.20;

  sorted.forEach((id, i) => {
    const base = i * slice;
    // Per-id jitter in [-maxJitter, +maxJitter]
    const jitter = (stableHash(id + "jitter") - 0.5) * 2 * maxJitter;
    result.set(id, base + jitter);
  });

  return result;
}

function urgencyOrbitalLayout(
  nodes: InitiativeNode[],
  orphans: SessionState[],
  cx: number,
  cy: number,
  shortSide: number,
): {
  initiatives: Array<{ node: InitiativeNode; urgency: UrgencyTier; x: number; y: number }>;
  orphans: Array<{ session: SessionState; x: number; y: number }>;
} {
  // Group node ids by urgency tier so we can spread each tier evenly.
  const tierGroups = new Map<UrgencyTier, string[]>();
  const nodeUrgency = new Map<string, UrgencyTier>();
  for (const node of nodes) {
    const urgency = deriveUrgency(node);
    nodeUrgency.set(node.initiative.id, urgency);
    const g = tierGroups.get(urgency) ?? [];
    g.push(node.initiative.id);
    tierGroups.set(urgency, g);
  }

  // Compute evenly-distributed angles per tier.
  const angleMap = new Map<string, number>();
  for (const [, ids] of tierGroups) {
    const angles = evenTierAngles(ids);
    for (const [id, angle] of angles) {
      angleMap.set(id, angle);
    }
  }

  const initiatives = nodes.map((node) => {
    const urgency = nodeUrgency.get(node.initiative.id) ?? deriveUrgency(node);
    const fraction = URGENCY_RADIUS_FRACTION[urgency];
    const radius = shortSide * fraction;
    const angle = angleMap.get(node.initiative.id) ?? stableHash(node.initiative.id) * 2 * Math.PI;
    return {
      node,
      urgency,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
    };
  });

  // Orphans: also even-distribute around the orphan radius.
  const orphanAngles = evenTierAngles(orphans.map((s) => s.sessionId));
  const orphanNodes = orphans.map((session) => {
    const radius = shortSide * ORPHAN_RADIUS_FRACTION;
    const angle = orphanAngles.get(session.sessionId) ?? stableHash(session.sessionId) * 2 * Math.PI;
    return {
      session,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
    };
  });

  return { initiatives, orphans: orphanNodes };
}

// ---------------------------------------------------------------------------
// Visual metadata per urgency tier — CHANNEL 1: core color + motion
// ---------------------------------------------------------------------------

const URGENCY_META: Record<
  UrgencyTier,
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
  working: {
    color: "#4a9eff",
    glowColor: "rgba(74,158,255,0.5)",
    r: 14,
    cssClass: "node--working",
    label: "working",
    opacity: 1,
  },
  idle: {
    color: "#4b5563",
    glowColor: "rgba(75,85,99,0.2)",
    r: 11,
    cssClass: "node--idle",
    label: "idle",
    opacity: 0.65,
  },
  done: {
    color: "#2e3540",
    glowColor: "rgba(46,53,64,0.1)",
    r: 9,
    cssClass: "node--done",
    label: "done",
    opacity: 0.25,
  },
};

// Tether line alpha tracks urgency: needs-human brightest, done faintest.
const TETHER_OPACITY: Record<UrgencyTier, number> = {
  "needs-human": 0.55,
  working: 0.38,
  idle: 0.13,
  done: 0.06,
};

// PR delivery ring color — CHANNEL 2: calm green, no motion.
const DELIVERY_RING_COLOR = "#3ecf8e";
const DELIVERY_RING_GLOW = "rgba(62,207,142,0.35)";

// ---------------------------------------------------------------------------
// Hover tooltip
// ---------------------------------------------------------------------------

interface TooltipData {
  x: number;
  y: number;
  title: string;
  phase: string;
  delivery: DeliveryStatus;
  urgency: UrgencyTier;
  needsHuman: false | NeedsHumanFlavor;
}

function NodeTooltip({ data, svgW, svgH }: { data: TooltipData; svgW: number; svgH: number }) {
  // Delivery label
  const deliveryLabel =
    data.delivery === "pr-open"
      ? "PR open"
      : data.delivery === "merged"
        ? "merged"
        : "no PR";

  // Clamp tooltip to stay inside the SVG viewport.
  const TW = 200;
  const TH = 76;
  const PAD = 12;
  const rawX = data.x + 20;
  const rawY = data.y - 10;
  const clampedX = Math.min(Math.max(rawX, PAD), svgW - TW - PAD);
  const clampedY = Math.min(Math.max(rawY, PAD), svgH - TH - PAD);

  return (
    <g className="node-tooltip" style={{ pointerEvents: "none" }}>
      <rect
        x={clampedX}
        y={clampedY}
        width={TW}
        height={TH}
        rx={5}
        fill="rgba(8,10,16,0.94)"
        stroke="rgba(255,255,255,0.10)"
        strokeWidth={1}
      />
      {/* Full title — wrap at 28 chars per line, two lines max */}
      {wrapText(data.title, 28).map((line, i) => (
        <text
          key={i}
          x={clampedX + 10}
          y={clampedY + 18 + i * 14}
          fill="rgba(226,232,240,0.95)"
          fontSize={11}
          fontFamily="var(--font-mono)"
          fontWeight="500"
        >
          {line}
        </text>
      ))}
      <text
        x={clampedX + 10}
        y={clampedY + 52}
        fill="rgba(160,170,185,0.75)"
        fontSize={9.5}
        fontFamily="var(--font-mono)"
      >
        {data.phase} · {deliveryLabel} · {data.urgency}
      </text>
      {data.needsHuman !== false && (
        <text
          x={clampedX + 10}
          y={clampedY + 66}
          fill="#ff6b35"
          fontSize={9}
          fontFamily="var(--font-mono)"
          fontWeight="600"
        >
          {data.needsHuman === "answer" ? "needs answer" : "awaiting review"}
        </text>
      )}
    </g>
  );
}

// Wrap text at approx charLimit chars per line, returning up to 2 lines.
function wrapText(text: string, charLimit: number): string[] {
  if (text.length <= charLimit) return [text];
  const cut = text.lastIndexOf(" ", charLimit);
  const breakAt = cut > 0 ? cut : charLimit;
  const line1 = text.slice(0, breakAt);
  const rest = text.slice(breakAt + (cut > 0 ? 1 : 0));
  const line2 = rest.length > charLimit ? rest.slice(0, charLimit - 1) + "…" : rest;
  return [line1, line2];
}

// ---------------------------------------------------------------------------
// Tether line (center → node)
// ---------------------------------------------------------------------------

interface TetherProps {
  cx: number;
  cy: number;
  x: number;
  y: number;
  urgency: UrgencyTier;
  color: string;
  r: number;
}

function TetherLine({ cx, cy, x, y, urgency, color, r }: TetherProps) {
  const opacity = TETHER_OPACITY[urgency];
  const dx = x - cx;
  const dy = y - cy;
  const dist = Math.sqrt(dx * dx + dy * dy) || 1;
  const stopX = x - (dx / dist) * (r + 4);
  const stopY = y - (dy / dist) * (r + 4);

  return (
    <line
      x1={cx}
      y1={cy}
      x2={stopX}
      y2={stopY}
      stroke={color}
      strokeWidth={urgency === "needs-human" ? 1.5 : 1}
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
  const rings: Array<{ tier: UrgencyTier; opacity: number }> = [
    { tier: "needs-human", opacity: 0.12 },
    { tier: "working", opacity: 0.07 },
    { tier: "idle", opacity: 0.05 },
    { tier: "done", opacity: 0.04 },
  ];

  return (
    <>
      {rings.map(({ tier, opacity }) => (
        <circle
          key={tier}
          cx={cx}
          cy={cy}
          r={shortSide * URGENCY_RADIUS_FRACTION[tier]}
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
    <g className="constellation-center" style={{ pointerEvents: "none" }}>
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
  urgency: UrgencyTier;
  x: number;
  y: number;
  svgW: number;
  svgH: number;
  onNavigate: (id: string) => void;
}

function ConstellationNode({ node, urgency, x, y, svgW, svgH, onNavigate }: NodeProps) {
  const [hovered, setHovered] = useState(false);
  const meta = URGENCY_META[urgency];
  const needsHuman = node.needsHuman;
  const delivery = node.delivery;
  const hasPr = delivery === "pr-open";
  // PR ring radius: slightly outside core + delivery ring if any.
  const prRingR = meta.r + 8;

  // Title: two lines up to 22 chars each, no hard truncation in the node label.
  const titleLines = wrapText(node.initiative.title, 22);

  // Legacy data-activity: map urgency back to something meaningful for test hooks.
  // Tests check for "idle", "busy", "needs-human" etc — map urgency tiers to match.
  const dataActivity =
    needsHuman !== false ? "needs-human" : urgency === "working" ? "busy" : urgency;

  return (
    <g
      className={`constellation-node ${meta.cssClass}`}
      transform={`translate(${x},${y})`}
      role="button"
      tabIndex={0}
      aria-label={`${node.initiative.title} — ${meta.label}`}
      data-initiative-id={node.initiative.id}
      data-activity={dataActivity}
      data-has-pr={hasPr ? "true" : "false"}
      onClick={() => onNavigate(node.initiative.id)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onNavigate(node.initiative.id);
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setHovered(true)}
      onBlur={() => setHovered(false)}
      style={{ cursor: "pointer" }}
    >
      {/* CHANNEL 2: Delivery ring — calm green, NO motion, shown when PR exists */}
      {hasPr && (
        <circle
          className="node-delivery-ring"
          cx={0}
          cy={0}
          r={prRingR}
          fill="none"
          stroke={DELIVERY_RING_COLOR}
          strokeWidth={1.5}
          opacity={0.55}
          style={{ filter: `drop-shadow(0 0 5px ${DELIVERY_RING_GLOW})` }}
        />
      )}

      {/* CHANNEL 1 urgency animations */}

      {/* needs-human: outer flare ring (animated) */}
      {needsHuman !== false && (
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

      {/* needs-human: secondary flare ring (staggered, animated) */}
      {needsHuman !== false && (
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

      {/* Core circle — CHANNEL 1 color */}
      <circle
        className="node-core"
        cx={0}
        cy={0}
        r={meta.r}
        fill={meta.color}
        opacity={meta.opacity}
        style={{ filter: `drop-shadow(0 0 ${meta.r * 0.9}px ${meta.glowColor})` }}
      />

      {/* Pulse ring for working + needs-human (animated) */}
      {(urgency === "working" || needsHuman !== false) && (
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
      {needsHuman !== false && (
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

      {/* PR badge — shown when PR open and NOT needs-human (to avoid badge collision) */}
      {hasPr && needsHuman === false && (
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

      {/* Title — two lines (longer, higher contrast) */}
      {titleLines.map((line, i) => (
        <text
          key={i}
          className="node-label"
          x={0}
          y={meta.r + 18 + i * 13}
          textAnchor="middle"
          fill={needsHuman !== false ? "#ffcbb3" : "rgba(226,232,240,0.87)"}
          fontSize={11}
          fontFamily="var(--font-mono)"
          fontWeight={needsHuman !== false ? "600" : "400"}
        >
          {line}
        </text>
      ))}

      {/* Phase token */}
      <text
        className="node-phase"
        x={0}
        y={meta.r + 18 + titleLines.length * 13}
        textAnchor="middle"
        fill={meta.color}
        fontSize={9.5}
        fontFamily="var(--font-mono)"
        opacity={needsHuman !== false ? 0.9 : 0.6}
        letterSpacing="0.04em"
      >
        {node.phase}
      </text>

      {/* Hover tooltip — shows full title + state summary */}
      {hovered && (
        <NodeTooltip
          data={{
            x: 0,
            y: 0,
            title: node.initiative.title,
            phase: node.phase,
            delivery: node.delivery,
            urgency,
            needsHuman,
          }}
          svgW={svgW}
          svgH={svgH}
        />
      )}
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
  return (
    <div
      className="constellation-legend"
      aria-label="Constellation legend — inner orbit needs you, outer orbit done"
      data-testid="constellation-legend"
    >
      <div className="constellation-legend__title">orbit key · inner = urgent</div>

      {/* CHANNEL 1: urgency (core color + motion) */}
      <div className="constellation-legend__section">urgency</div>

      <div className="constellation-legend__entry legend-entry--needs-human">
        <LegendDot color="#ff6b35" badge="!" />
        <span>needs your input</span>
      </div>

      <div className="constellation-legend__entry legend-entry--working">
        <LegendDot color="#4a9eff" pulse />
        <span>working</span>
      </div>

      <div className="constellation-legend__entry legend-entry--idle legend-entry--dim">
        <LegendDot color="#4b5563" />
        <span>idle</span>
      </div>

      <div className="constellation-legend__entry legend-entry--done legend-entry--dim">
        <LegendDot color="#2e3540" faint />
        <span>done / merged</span>
      </div>

      {/* CHANNEL 2: delivery ring */}
      <div className="constellation-legend__section">delivery</div>

      <div className="constellation-legend__entry legend-entry--pr">
        <LegendDeliveryRing badge="PR" />
        <span>open PR — awaiting review</span>
      </div>

      <div className="constellation-legend__entry legend-entry--orphan legend-entry--dim">
        <LegendDot color="#6b7280" dashed />
        <span>unregistered session</span>
      </div>
    </div>
  );
}

function LegendDot({
  color,
  badge,
  pulse,
  faint,
  dashed,
}: {
  color: string;
  badge?: string;
  pulse?: boolean;
  faint?: boolean;
  dashed?: boolean;
}) {
  return (
    <svg width={22} height={22} className="constellation-legend__icon" aria-hidden="true">
      {dashed ? (
        <circle cx={11} cy={11} r={7} fill="none" stroke={color} strokeWidth={1.5} strokeDasharray="3 2" opacity={0.7} />
      ) : (
        <circle cx={11} cy={11} r={7} fill={color} opacity={faint ? 0.3 : 1} />
      )}
      {badge === "!" && (
        <>
          <circle cx={18} cy={5} r={4.5} fill="#ff6b35" />
          <text x={18} y={8.5} textAnchor="middle" fill="white" fontSize={6.5} fontWeight="bold" fontFamily="monospace">!</text>
        </>
      )}
      {pulse && (
        <circle cx={11} cy={11} r={7} fill="none" stroke={color} strokeWidth={1} className="legend-pulse" />
      )}
    </svg>
  );
}

function LegendDeliveryRing({ badge }: { badge: string }) {
  return (
    <svg width={22} height={22} className="constellation-legend__icon" aria-hidden="true">
      {/* core (blue = working example) */}
      <circle cx={11} cy={11} r={5} fill="#4a9eff" opacity={0.8} />
      {/* delivery ring — green, no motion */}
      <circle cx={11} cy={11} r={9} fill="none" stroke="#3ecf8e" strokeWidth={1.5} opacity={0.6} />
      {/* PR badge */}
      <rect x={14} y={2} width={8} height={6} rx={1.5} fill="#a78bfa" />
      <text x={18} y={7} textAnchor="middle" fill="white" fontSize={4} fontWeight="bold" fontFamily="monospace">{badge}</text>
    </svg>
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

  // Layout cache key: re-run when ids, delivery, needsHuman, or session state change.
  const layoutKey =
    initiatives
      .map((n) => `${n.initiative.id}:${n.delivery}:${String(n.needsHuman)}:${n.activity}`)
      .join(",") +
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
        {positioned.map(({ node, urgency, x, y }) => (
          <TetherLine
            key={`tether-${node.initiative.id}`}
            cx={cx}
            cy={cy}
            x={x}
            y={y}
            urgency={urgency}
            color={URGENCY_META[urgency].color}
            r={URGENCY_META[urgency].r}
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
        {positioned.map(({ node, urgency, x, y }) => (
          <ConstellationNode
            key={node.initiative.id}
            node={node}
            urgency={urgency}
            x={x}
            y={y}
            svgW={W}
            svgH={H}
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
      {stars.map((st) => (
        <circle key={st.key} cx={st.x} cy={st.y} r={st.r} fill="white" opacity={st.opacity} />
      ))}
    </>
  );
}
