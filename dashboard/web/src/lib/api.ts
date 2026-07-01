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

// POST /api/initiatives/:id/launch-session — open a terminal in the initiative's worktree.
// Returns { ok: true } on success (a terminal window opened). Throws with the server's
// error message on failure so the caller can surface the reason.
export async function launchSession(initiativeId: string): Promise<{ ok: true }> {
  const res = await fetch(`/api/initiatives/${encodeURIComponent(initiativeId)}/launch-session`, {
    method: "POST",
  });
  const body = (await res.json()) as { ok?: boolean; error?: string };
  if (!res.ok) {
    throw new Error(body.error ?? `launch-session failed: ${res.status}`);
  }
  return { ok: true };
}

// POST /api/initiatives/:id/stop-session — stop a running background session (reap zombie).
export async function stopSession(initiativeId: string, sessionId: string): Promise<void> {
  const res = await fetch(`/api/initiatives/${encodeURIComponent(initiativeId)}/stop-session`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sessionId }),
  });
  if (!res.ok) throw new Error(`stop-session failed: ${res.status}`);
}

// Returns the URL for log streaming (piped into xterm.js by the drill-in view).
// Do NOT fetch this with fetchJSON — it is a chunked byte stream.
export function logsUrl(id: string, sessionId: string): string {
  return API_PATHS.logs(id, sessionId);
}
