import type { SnapshotEvent, DrillInDetail } from "./types.js";

// Endpoint path constants.
export const API_PATHS = {
  snapshot: "/api/snapshot",
  events: "/api/events",
  initiative: (id: string) => `/api/initiatives/${encodeURIComponent(id)}`,
  logs: (id: string, sessionId: string) =>
    `/api/initiatives/${encodeURIComponent(id)}/logs?session=${encodeURIComponent(sessionId)}`,
  attach: (id: string) => `/api/initiatives/${encodeURIComponent(id)}/attach`,
  mail: "/api/mail",
  mailSend: "/api/mail/send",
  mailClose: (id: string) => `/api/mail/${encodeURIComponent(id)}/close`,
  mailPurge: "/api/mail/purge",
} as const;

// GET /api/snapshot
export type SnapshotResponse = SnapshotEvent;

// GET /api/events  (text/event-stream)
// Each SSE event data field is a JSON-serialised SnapshotEvent.
export type EventsPayload = SnapshotEvent;

// GET /api/initiatives/:id
export type InitiativeDetailResponse = DrillInDetail;

// GET /api/initiatives/:id/logs?session=<sessionId>
// sessionId here is the SHORT claude session id (the `id` field from `claude agents --json`,
// 8 lowercase hex chars), NOT the full UUID. `claude logs <full-uuid>` silently fails.
// Returns raw `claude logs` bytes (terminal TUI output with ANSI escapes).
// Consume with xterm.js — do NOT strip ANSI.
export type LogsChunk = Uint8Array;

// POST /api/initiatives/:id/attach
export interface AttachRequest {
  // sessionId is the SHORT claude session id (the `id` field from `claude agents --json`,
  // 8 lowercase hex chars), NOT the full UUID. `claude attach <full-uuid>` fails silently.
  sessionId: string;
}
export interface AttachResponse {
  ok: true;
}

// SSE event union — the `type` field on the EventSource message.
export type DashboardSSEEvent =
  | { type: "snapshot"; data: SnapshotEvent };

// GET /api/mail -> 200 MailListResponse ({ messages }); on `ateam mail list`
// CLI failure -> 502 { error }. See MailMessage/MailListResponse in ./types.ts
// for the full contract (mirrors `ateam mail list --json`).
// NON-DESTRUCTIVE: the server MUST implement this via `ateam mail list`, never `ateam mail inbox`.

// POST /api/mail/send, body MailSendRequest ({ to, body, sender? }):
//   • 200 MailSendResponse ({ ok:true, messageId, recipient }) on success.
//   • 400 { error } for malformed/missing to|body or invalid initiative id.
//   • 502 { error } on `ateam mail send` failure.
// See MailSendRequest/MailSendResponse in ./types.ts.

// POST /api/mail/:id/close -> shells `ateam mail close <id>`. 200 { ok:true };
// 400 { error } for invalid/malformed id; 502 { error } on CLI failure.

// POST /api/mail/purge, body MailPurgeRequest ({ olderThan?, dryRun? }) ->
// shells `ateam mail purge`. 200 MailPurgeResponse ({ ok:true, output? });
// 502 { error } on CLI failure. See MailPurgeRequest/MailPurgeResponse in ./types.ts.
