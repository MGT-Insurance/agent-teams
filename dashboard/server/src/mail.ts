// Pure parsing/normalization helpers for the mail tab (agent-teams-hw71).
// No process spawning here — see cli.ts for the `ateam mail list`/`ateam mail
// send` wrappers that call these.

import type { MailMessage } from "@agent-teams/shared";

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function nullableStr(v: unknown): string | null {
  return typeof v === "string" ? v : null;
}

function status(v: unknown): MailMessage["status"] {
  return v === "acked" || v === "read" || v === "pending" ? v : "pending";
}

// Coerces the raw JSON array from `ateam mail list --json` into MailMessage[].
// Defensive per-field: malformed/missing fields default rather than throw, but
// a non-array top level is a hard failure (the shape is fundamentally wrong).
export function normalizeMailJson(raw: string): MailMessage[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(`ateam mail list --json produced invalid JSON: ${String(err)}`);
  }
  if (!Array.isArray(parsed)) {
    throw new Error("ateam mail list --json did not return an array");
  }
  return parsed.map((item): MailMessage => {
    const r = (item ?? {}) as Record<string, unknown>;
    return {
      id: str(r.id),
      to: str(r.to),
      from: str(r.from),
      subject: str(r.subject),
      body: str(r.body),
      status: status(r.status),
      createdAt: str(r.createdAt),
      readAt: nullableStr(r.readAt),
      readBy: nullableStr(r.readBy),
      thread: nullableStr(r.thread),
      closed: typeof r.closed === "boolean" ? r.closed : false,
    };
  });
}

// Parses `ateam mail send`'s stdout for the `message_id:`/`recipient:` lines.
// Tolerates the additional liveness/note/respawn diagnostic lines messaging.go
// prints after them (e.g. a later line containing "recipient worktree=..." must
// NOT be mistaken for the `recipient:` field) — match anchored at line start.
export function parseSendOutput(stdout: string): { messageId: string; recipient: string } {
  const messageId = stdout.match(/^message_id:\s*(.+)$/m)?.[1]?.trim();
  const recipient = stdout.match(/^recipient:\s*(.+)$/m)?.[1]?.trim();
  if (!messageId || !recipient) {
    throw new Error(`ateam mail send output missing message_id/recipient: ${stdout.slice(0, 300)}`);
  }
  return { messageId, recipient };
}
