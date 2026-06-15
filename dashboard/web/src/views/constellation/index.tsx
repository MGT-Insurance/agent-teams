import { useMemo, useRef, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSnapshotContext } from "../../SnapshotContext.js";
import type { InitiativeNode, SessionState, DeliveryStatus, NeedsHumanFlavor } from "@agent-teams/shared";
import "./constellation.css";

// ---------------------------------------------------------------------------
// Derived urgency — the single dimension that drives orbital radius + motion.
// Computed from the two-dimension model (delivery x activity x needsHuman).
// ---------------------------------------------------------------------------

// "waiting" is a sub-tier of needs-human — most urgent (agent blocked on you).
// Both "waiting" and "needs-human" orbit in the inner ring; "waiting" is visually
// stronger (brighter color, double flare ring).
type UrgencyTier = "waiting" | "needs-human" | "working" | "idle" | "done";

function deriveUrgency(node: InitiativeNode): UrgencyTier {
  if (node.needsHuman === "waiting") return "waiting";
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
// Angles are globally-even across ALL nodes so no two nodes ever share an
// angle — even nodes at the same radius can never stack.
// ---------------------------------------------------------------------------

// Radii are fractions of shortSide (= min(W,H)). Coordinates are centered at
// (W/2, H/2), so usable radius = shortSide/2. These fractions use the full
// shortSide, so the actual fraction of available canvas = value / 2.
//   E.g. shortSide=782 → inner=0.28*782=219px (56% of 391px half-canvas)
//                        done=0.44*782=344px (88% of half-canvas)
const URGENCY_RADIUS_FRACTION: Record<UrgencyTier, number> = {
  waiting: 0.28,      // innermost — agent waiting on you (most urgent)
  "needs-human": 0.28, // same inner orbit — any needs-you flavor
  working: 0.34,
  idle: 0.40,
  done: 0.44, // outer dim rim
};

// Orphans live at the rim, slightly outside done nodes.
const ORPHAN_RADIUS_FRACTION = 0.46;

// Urgency tier sort order — lower number = more urgent = placed first.
const URGENCY_SORT_ORDER: Record<UrgencyTier, number> = {
  waiting: 0,
  "needs-human": 1,
  working: 2,
  idle: 3,
  done: 4,
};

// Stable deterministic hash → [0, 1) used for sort tie-breaking and placement.
function stableHash(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return h / 0xffffffff;
}

// Globally-even angular distribution across ALL nodes (initiatives + orphans).
//
// KEY INVARIANT: every node gets angle = index * (2π / totalCount) + fixedPhase,
// so same-radius nodes are ALWAYS angularly separated. No two nodes can stack.
//
// Sort order: urgency tier (most urgent first), then stable id-hash tie-break.
// This keeps the layout stable across refreshes (no jumps) while guaranteeing
// minimum angular separation of 360°/N between any two adjacent nodes.
export function globalEvenAngles(
  allIds: Array<{ id: string; urgencyOrder: number }>,
): Map<string, number> {
  const n = allIds.length;
  if (n === 0) return new Map();

  // Sort deterministically: urgency tier first, then stable hash tie-break.
  const sorted = [...allIds].sort((a, b) => {
    if (a.urgencyOrder !== b.urgencyOrder) return a.urgencyOrder - b.urgencyOrder;
    return stableHash(a.id) - stableHash(b.id);
  });

  const result = new Map<string, number>();
  const slice = (2 * Math.PI) / n;
  // Fixed phase offset: start at top (-π/2) so first node lands at 12 o'clock.
  const phase = -Math.PI / 2;

  sorted.forEach(({ id }, i) => {
    result.set(id, phase + i * slice);
  });

  return result;
}

// Minimum center-to-center distance between two placed nodes to prevent overlap.
// If the globally-even angles still result in nodes too close (e.g. very dense),
// nudge the radius outward. With N >= 2 and even angles this should rarely fire.
const MIN_NODE_SEPARATION = 55; // px — covers r=20 node + glow rings + margin

function nudgeForSeparation(
  placed: Array<{ id: string; x: number; y: number; angle: number; radius: number }>,
  cx: number,
  cy: number,
): Array<{ id: string; x: number; y: number; angle: number; radius: number }> {
  // Simple O(n^2) pass — node counts are small (< ~20 typically).
  const result = placed.map((p) => ({ ...p }));
  let changed = true;
  let passes = 0;
  while (changed && passes < 10) {
    changed = false;
    for (let i = 0; i < result.length; i++) {
      for (let j = i + 1; j < result.length; j++) {
        const pi = result[i];
        const pj = result[j];
        if (!pi || !pj) continue;
        const dx = pi.x - pj.x;
        const dy = pi.y - pj.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < MIN_NODE_SEPARATION) {
          // Nudge the outer node (larger radius) outward along its angle.
          const outer = pi.radius >= pj.radius ? pi : pj;
          const need = MIN_NODE_SEPARATION - dist;
          outer.radius += need / 2 + 1;
          outer.x = cx + outer.radius * Math.cos(outer.angle);
          outer.y = cy + outer.radius * Math.sin(outer.angle);
          changed = true;
        }
      }
    }
    passes++;
  }
  return result;
}

function urgencyOrbitalLayout(
  nodes: InitiativeNode[],
  orphans: SessionState[],
  cx: number,
  cy: number,
  shortSide: number,
): {
  initiatives: Array<{ node: InitiativeNode; urgency: UrgencyTier; angle: number; x: number; y: number }>;
  orphans: Array<{ session: SessionState; x: number; y: number }>;
} {
  // Build the global node list for angle assignment.
  const nodeUrgency = new Map<string, UrgencyTier>();
  const allIds: Array<{ id: string; urgencyOrder: number }> = [];

  for (const node of nodes) {
    const urgency = deriveUrgency(node);
    nodeUrgency.set(node.initiative.id, urgency);
    allIds.push({ id: node.initiative.id, urgencyOrder: URGENCY_SORT_ORDER[urgency] });
  }
  // Orphans get sorted after all initiative tiers (urgencyOrder=5).
  for (const session of orphans) {
    allIds.push({ id: session.sessionId, urgencyOrder: 5 });
  }

  // Assign globally-even angles — no two nodes share an angle.
  const angleMap = globalEvenAngles(allIds);

  // Place initiative nodes.
  const rawInitiatives = nodes.map((node) => {
    const urgency = nodeUrgency.get(node.initiative.id) ?? deriveUrgency(node);
    const fraction = URGENCY_RADIUS_FRACTION[urgency];
    const radius = shortSide * fraction;
    const angle = angleMap.get(node.initiative.id) ?? stableHash(node.initiative.id) * 2 * Math.PI;
    return {
      id: node.initiative.id,
      angle,
      radius,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
      node,
      urgency,
    };
  });

  // Place orphan nodes.
  const rawOrphans = orphans.map((session) => {
    const radius = shortSide * ORPHAN_RADIUS_FRACTION;
    const angle = angleMap.get(session.sessionId) ?? stableHash(session.sessionId) * 2 * Math.PI;
    return {
      id: session.sessionId,
      angle,
      radius,
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
      session,
    };
  });

  // Combine all placed items, apply separation nudge, then split back out.
  const allPlaced = [
    ...rawInitiatives.map((p) => ({ id: p.id, x: p.x, y: p.y, angle: p.angle, radius: p.radius })),
    ...rawOrphans.map((p) => ({ id: p.id, x: p.x, y: p.y, angle: p.angle, radius: p.radius })),
  ];
  const nudged = nudgeForSeparation(allPlaced, cx, cy);
  const nudgedMap = new Map(nudged.map((p) => [p.id, p]));

  const initiatives = rawInitiatives.map((p) => {
    const n = nudgedMap.get(p.id) ?? p;
    return { node: p.node, urgency: p.urgency, angle: n.angle, x: n.x, y: n.y };
  });

  const orphanNodes = rawOrphans.map((p) => {
    const n = nudgedMap.get(p.id) ?? p;
    return { session: p.session, x: n.x, y: n.y };
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
  // "waiting" — agent paused on human input. Shared orange family; stronger via pulse/size.
  waiting: {
    color: "#ff6b35",
    glowColor: "rgba(255,107,53,0.8)",
    r: 20,
    cssClass: "node--waiting",
    label: "needs you",
    opacity: 1,
  },
  // "needs-human" — review/generic flavor. Same inner orbit, slightly softer orange.
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

// Tether line alpha tracks urgency: waiting brightest, done faintest.
const TETHER_OPACITY: Record<UrgencyTier, number> = {
  waiting: 0.70,
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
          {data.needsHuman === "waiting"
            ? "agent waiting on you"
            : data.needsHuman === "review"
              ? "verify & merge"
              : "needs your attention"}
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
  // "waiting" and "needs-human" share the same radius (0.20), so only draw one inner ring.
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
  angle: number;
  x: number;
  y: number;
  svgW: number;
  svgH: number;
  onNavigate: (id: string) => void;
}

// Compute radial label offset: place the label past the node's full visual
// radius (core + glow + flare rings + gap) so it never sits on the node.
// For needs-human nodes the outermost ring is at r+26 (secondary flare).
// For working nodes the pulse ring is at r. Add a generous gap beyond the edge.
function labelOffset(meta: typeof URGENCY_META[UrgencyTier], hasFlare: boolean): number {
  if (hasFlare) return meta.r + 26 + 20; // past secondary flare ring + 20px gap
  return meta.r + 10 + 12;              // past glow + 12px gap
}

function ConstellationNode({ node, urgency, angle, x, y, svgW, svgH, onNavigate }: NodeProps) {
  const [hovered, setHovered] = useState(false);
  const meta = URGENCY_META[urgency];
  const needsHuman = node.needsHuman;
  const delivery = node.delivery;
  const hasPr = delivery === "pr-open";
  const hasFlare = needsHuman !== false;
  // PR ring radius: slightly outside core + delivery ring if any.
  const prRingR = meta.r + 8;

  // Title: two lines up to 22 chars each, no hard truncation in the node label.
  const titleLines = wrapText(node.initiative.title, 22);

  // Legacy data-activity: map urgency back to something meaningful for test hooks.
  // Tests check for "idle", "busy", "needs-human" etc — map urgency tiers to match.
  const dataActivity =
    needsHuman !== false ? "needs-human" : urgency === "working" ? "busy" : urgency;

  // Radial label placement — labels radiate OUTWARD from the node center
  // (away from canvas center), never sitting on the node glow/ring.
  //
  // Strategy: the node <g> is already translated to (x, y). Labels are
  // positioned relative to the node origin (0,0). We compute an outward
  // direction from the angle, then offset the label along that direction.
  //
  // textAnchor controls which direction text grows from the anchor point:
  //   right half (cosA > 0.2): "start" — text extends rightward (outward)
  //   left half  (cosA < -0.2): "end"   — text extends leftward (outward)
  //   top/bottom (|cosA| <= 0.2): "middle" — text is horizontally centered
  // This ensures text always radiates AWAY from the node, never over it.
  const cosA = Math.cos(angle);
  const sinA = Math.sin(angle);
  const LABEL_ANCHOR_THRESHOLD = 0.20;
  const textAnchor =
    cosA > LABEL_ANCHOR_THRESHOLD
      ? "start"
      : cosA < -LABEL_ANCHOR_THRESHOLD
        ? "end"
        : "middle";

  const lOffset = labelOffset(meta, hasFlare);
  // Base label position: outward along angle from node origin
  const lx = cosA * lOffset;
  const ly = sinA * lOffset;
  // Line height for multi-line labels
  const LINE_H = 13;

  // Phase token sits below the last title line
  const phaseY = ly + titleLines.length * LINE_H + 2;

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
      {hasFlare && (
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
      {hasFlare && (
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
      {(urgency === "working" || hasFlare) && (
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
      {hasFlare && (
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

      {/* Title — radially outward from node, never on top of glow/ring */}
      {titleLines.map((line, i) => (
        <text
          key={i}
          className="node-label"
          x={lx}
          y={ly + i * LINE_H}
          textAnchor={textAnchor}
          dominantBaseline="middle"
          fill={needsHuman !== false ? "#ffcbb3" : "rgba(226,232,240,0.87)"}
          fontSize={11}
          fontFamily="var(--font-mono)"
          fontWeight={needsHuman !== false ? "600" : "400"}
        >
          {line}
        </text>
      ))}

      {/* Phase token — below label lines */}
      <text
        className="node-phase"
        x={lx}
        y={phaseY}
        textAnchor={textAnchor}
        dominantBaseline="middle"
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

      <div className="constellation-legend__entry legend-entry--waiting">
        <LegendDot color="#ff6b35" badge="!" />
        <span>agent waiting on you</span>
      </div>

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

  // Re-run when the component transitions from an early-return state (no ref)
  // to the full SVG render (ref attached). Without these deps the effect only
  // runs on the initial mount — if that mount shows "connecting" the div isn't
  // rendered yet and containerRef.current is null, so the observer never starts.
  const hasContent =
    connectionState !== "error" &&
    !(connectionState === "connecting" && initiatives.length === 0) &&
    (initiatives.length > 0 || unmatchedSessions.length > 0);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    // Sync immediately so the first layout uses the real size, not the 900×700 default.
    const rect = el.getBoundingClientRect();
    if (rect.width > 50 && rect.height > 50) {
      setViewSize({ w: Math.round(rect.width), h: Math.round(rect.height) });
    }
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
  }, [hasContent]);

  const W = viewSize.w;
  const H = viewSize.h;
  const cx = W / 2;
  const cy = H / 2;
  const shortSide = Math.min(W, H);

  // useMemo recomputes whenever any dep changes — cx, cy, shortSide included.
  // The manual layoutCacheRef was removed: it keyed only on initiative/orphan ids,
  // so it returned a stale layout (computed at the 900×700 default) on tab-switch
  // remounts after the ResizeObserver had already updated the real container size.
  const { positioned, positionedOrphans } = useMemo(() => {
    const result = urgencyOrbitalLayout(initiatives, unmatchedSessions, cx, cy, shortSide);
    return { positioned: result.initiatives, positionedOrphans: result.orphans };
  }, [initiatives, unmatchedSessions, cx, cy, shortSide]);

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
        {positioned.map(({ node, urgency, angle, x, y }) => (
          <ConstellationNode
            key={node.initiative.id}
            node={node}
            urgency={urgency}
            angle={angle}
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
