// macOS terminal attach: open a real terminal window running `claude attach <id>`.
// Uses `open -a Terminal` with an osascript to run the command.
// Returns { ok: true } on success, throws on failure.

import { spawn } from "node:child_process";

export interface AttachResult {
  ok: true;
}

// claude agents --json exposes two id fields:
//   id        — short 8 lowercase-hex chars (e.g. "21bd9e92"), used by claude attach/logs/stop
//   sessionId — full UUID v4, informational only
// We validate the SHORT id here; passing the full UUID to `claude attach` silently fails.
const CLAUDE_ID_RE = /^[0-9a-f]{8}$/;

export function isValidSessionId(id: string): boolean {
  return CLAUDE_ID_RE.test(id);
}

// Escape a string for safe embedding inside an AppleScript double-quoted string.
// Replaces backslash then double-quote so the value cannot break out of `do script "..."`.
function escapeForAppleScript(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

// Launch `claude attach <id>` in a new macOS Terminal window.
// Prefers iTerm2 if running, falls back to Terminal.app.
// Caller MUST validate id with isValidSessionId before calling.
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
