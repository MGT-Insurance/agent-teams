import type { SnapshotEvent, DrillInDetail } from "./types.js";

// Endpoint path constants.
export const API_PATHS = {
  snapshot: "/api/snapshot",
  events: "/api/events",
  initiative: (id: string) => `/api/initiatives/${encodeURIComponent(id)}`,
  logs: (id: string, sessionId: string) =>
    `/api/initiatives/${encodeURIComponent(id)}/logs?session=${encodeURIComponent(sessionId)}`,
  attach: (id: string) => `/api/initiatives/${encodeURIComponent(id)}/attach`,
} as const;

// GET /api/snapshot
export type SnapshotResponse = SnapshotEvent;

// GET /api/events  (text/event-stream)
// Each SSE event data field is a JSON-serialised SnapshotEvent.
export type EventsPayload = SnapshotEvent;

// GET /api/initiatives/:id
export type InitiativeDetailResponse = DrillInDetail;

// GET /api/initiatives/:id/logs?session=<sessionId>
// Returns raw `claude logs` bytes (terminal TUI output with ANSI escapes).
// Consume with xterm.js — do NOT strip ANSI.
export type LogsChunk = Uint8Array;

// POST /api/initiatives/:id/attach
export interface AttachRequest {
  sessionId: string;
}
export interface AttachResponse {
  ok: true;
}

// SSE event union — the `type` field on the EventSource message.
export type DashboardSSEEvent =
  | { type: "snapshot"; data: SnapshotEvent };
