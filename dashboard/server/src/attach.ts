// macOS terminal attach: open a real terminal window running `claude attach <sessionId>`.
// Uses `open -a Terminal` with an osascript to run the command.
// Returns { ok: true } on success, throws on failure.

import { spawn } from "node:child_process";

export interface AttachResult {
  ok: true;
}

// Launch `claude attach <sessionId>` in a new macOS Terminal window.
// Prefers iTerm2 if running, falls back to Terminal.app.
export function launchAttach(sessionId: string): Promise<AttachResult> {
  // Use osascript to open a new Terminal window and run the command.
  // The `do script` command opens a new tab/window in Terminal.app.
  const script = `tell application "Terminal"
  do script "claude attach ${sessionId}"
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
