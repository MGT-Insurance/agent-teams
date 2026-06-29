// macOS terminal launch: open a real terminal window running `cd <worktree> && claude`.
// Modeled on attach.ts launchAttach — same osascript approach, same return shape.
// Returns { ok: true } on success, throws on failure.

import { spawn } from "node:child_process";
import { escapeForAppleScript, isItermInstalled } from "./attach.js";

export interface LaunchResult {
  ok: true;
}

// Open a new terminal window running `cd <worktreePath> && claude`.
// Prefers iTerm2 if installed, falls back to Terminal.app.
export function launchTerminal(worktreePath: string): Promise<LaunchResult> {
  const safe = escapeForAppleScript(worktreePath);
  const script = isItermInstalled()
    ? `tell application "iTerm"\n  set w to (create window with default profile)\n  tell current session of w\n    write text "cd ${safe} && claude"\n  end tell\n  activate\nend tell`
    : `tell application "Terminal"\n  do script "cd ${safe} && claude"\n  activate\nend tell`;

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
