// macOS terminal launch: open a real terminal window running `ateam resume <id>`.
// ateam resume builds the full `claude --bg -n <name> --permission-mode bypassPermissions /dri <id>`
// with Dir set to the initiative's worktree. Running inside a real terminal (with a TTY) is
// required — spawning claude --bg headlessly from the node server fork-bombs (see mfpz.6).
// Modeled on attach.ts launchAttach — same osascript approach, same return shape.

import { spawn } from "node:child_process";
import { escapeForAppleScript, isItermInstalled } from "./attach.js";

export interface LaunchResult {
  ok: true;
}

export function launchTerminal(initiativeId: string): Promise<LaunchResult> {
  const safe = escapeForAppleScript(initiativeId);
  const cmd = `ateam resume ${safe}`;
  const script = isItermInstalled()
    ? `tell application "iTerm"\n  set w to (create window with default profile)\n  tell current session of w\n    write text "${cmd}"\n  end tell\n  activate\nend tell`
    : `tell application "Terminal"\n  do script "${cmd}"\n  activate\nend tell`;

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
