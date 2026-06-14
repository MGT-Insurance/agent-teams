// macOS terminal attach: open a real terminal window running `claude attach <sessionId>`.
// Uses `open -a Terminal` with an osascript to run the command.
// Returns { ok: true } on success, throws on failure.

import { spawn } from "node:child_process";

export interface AttachResult {
  ok: true;
}

// UUID v4 format: 8-4-4-4-12 hex digits.
const UUID_V4_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isValidSessionId(sessionId: string): boolean {
  return UUID_V4_RE.test(sessionId);
}

// Escape a string for safe embedding inside an AppleScript double-quoted string.
// Replaces backslash then double-quote so the value cannot break out of `do script "..."`.
function escapeForAppleScript(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

// Launch `claude attach <sessionId>` in a new macOS Terminal window.
// Prefers iTerm2 if running, falls back to Terminal.app.
// Caller MUST validate sessionId with isValidSessionId before calling.
export function launchAttach(sessionId: string): Promise<AttachResult> {
  // Use osascript to open a new Terminal window and run the command.
  // The `do script` command opens a new tab/window in Terminal.app.
  const safe = escapeForAppleScript(sessionId);
  const script = `tell application "Terminal"
  do script "claude attach ${safe}"
  activate
end tell`;

  return new Promise((resolve, reject) => {
    const proc = spawn("osascript", ["-e", script], {
      stdio: ["ignore", "pipe", "pipe"],
    });

    const errChunks: Buffer[] = [];
    proc.stderr.on("data", (chunk: Buffer) => errChunks.push(chunk));

    proc.on("error", (err) => {
      reject(new Error(`failed to spawn osascript: ${err.message}`));
    });

    proc.on("close", (code) => {
      if (code !== 0) {
        const stderr = Buffer.concat(errChunks).toString("utf8");
        reject(new Error(`osascript exited with code ${code}: ${stderr.slice(0, 200)}`));
        return;
      }
      resolve({ ok: true });
    });
  });
}
