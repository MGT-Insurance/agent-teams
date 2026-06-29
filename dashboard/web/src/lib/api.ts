import type { SnapshotEvent, DrillInDetail, AttachRequest, AttachResponse } from "@agent-teams/shared";
import { API_PATHS } from "@agent-teams/shared";

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url} failed: ${res.status} ${res.statusText}`);
  return res.json() as Promise<T>;
}

// GET /api/snapshot — one-shot fetch of the current snapshot (use SSE for live updates).
export function fetchSnapshot(): Promise<SnapshotEvent> {
  return fetchJSON<SnapshotEvent>(API_PATHS.snapshot);
}

// GET /api/initiatives/:id — full drill-in detail for one initiative.
export function fetchInitiative(id: string): Promise<DrillInDetail> {
  return fetchJSON<DrillInDetail>(API_PATHS.initiative(id));
}

// POST /api/initiatives/:id/attach — open a terminal attach session.
// The backend shells out via macOS open/osascript; the response is just { ok: true }.
export async function attachToInitiative(id: string, sessionId: string): Promise<AttachResponse> {
  const body: AttachRequest = { sessionId };
  const res = await fetch(API_PATHS.attach(id), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`POST ${API_PATHS.attach(id)} failed: ${res.status} ${res.statusText}`);
  return res.json() as Promise<AttachResponse>;
}

// POST /api/initiatives/:id/launch-session — spawn a new bg DRI session for the initiative.
// Returns { ok: true, log } on success. Throws with a message carrying the server's
// error/detail (and log path if present) on failure, so callers can show a real reason.
export async function launchSession(initiativeId: string): Promise<{ ok: true; log: string }> {
  const res = await fetch(`/api/initiatives/${encodeURIComponent(initiativeId)}/launch-session`, {
    method: "POST",
  });
  const body = (await res.json()) as {
    ok?: boolean;
    log?: string;
    error?: string;
    detail?: string;
  };
  if (!res.ok) {
    const msg = body.error ?? `launch-session failed: ${res.status}`;
    const parts: string[] = [msg];
    if (body.detail) parts.push(body.detail.trim());
    if (body.log) parts.push(`Log: ${body.log}`);
    throw new Error(parts.join("\n"));
  }
  return { ok: true, log: body.log ?? "" };
}

// Returns the URL for log streaming (piped into xterm.js by the drill-in view).
// Do NOT fetch this with fetchJSON — it is a chunked byte stream.
export function logsUrl(id: string, sessionId: string): string {
  return API_PATHS.logs(id, sessionId);
}
