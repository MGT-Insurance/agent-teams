// macOS terminal attach: open a real terminal window running `claude attach <id>`.
// Uses `open -a Terminal` with an osascript to run the command.
// Returns { ok: true } on success, throws on failure.

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { isValidSessionId } from "@agent-teams/shared";

export interface AttachResult {
  ok: true;
}

// Re-exported so existing consumers (server/src/index.ts, stop.ts, tests) keep
// importing from "./attach.js" — the validation itself lives in @agent-teams/shared
// (agent-teams-rybk.5.4), the single source shared with parse.ts and the web view.
export { isValidSessionId };

// Escape a string for safe embedding inside an AppleScript double-quoted string.
// Replaces backslash then double-quote so the value cannot break out of `do script "..."`.
export function escapeForAppleScript(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

// Returns true when iTerm2 is installed (system-wide or user-local Applications folder).
export function isItermInstalled(): boolean {
  return (
    existsSync("/Applications/iTerm.app") ||
    existsSync(`${homedir()}/Applications/iTerm.app`)
  );
}

// Launch `claude attach <id>` in a new macOS Terminal window.
// Prefers iTerm2 if installed, falls back to Terminal.app.
// Caller MUST validate id with isValidSessionId before calling.
export function launchAttach(sessionId: string): Promise<AttachResult> {
  const safe = escapeForAppleScript(sessionId);
  // iTerm2: open a window with default profile, then write the command into the
  // interactive shell via `write text`. Using `command "..."` in the profile spec
  // bypasses shell profile loading so claude is not on PATH — write text avoids that.
  const script = isItermInstalled()
    ? `tell application "iTerm"\n  set w to (create window with default profile)\n  tell current session of w\n    write text "claude attach ${safe}"\n  end tell\n  activate\nend tell`
    : `tell application "Terminal"\n  do script "claude attach ${safe}"\n  activate\nend tell`;

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
