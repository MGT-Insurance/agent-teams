// Shell `claude stop <sessionId>` to stop a running background session.
// Returns { ok: true } on success, throws on failure.
// Caller MUST validate id with isValidSessionId (from attach.ts) before calling.

import { spawn } from "node:child_process";

export interface StopResult {
  ok: true;
}

export function launchStop(sessionId: string): Promise<StopResult> {
  return new Promise((resolve, reject) => {
    const proc = spawn("claude", ["stop", sessionId], {
      stdio: ["ignore", "pipe", "pipe"],
    });

    const errChunks: Buffer[] = [];
    proc.stderr.on("data", (chunk: Buffer) => errChunks.push(chunk));

    proc.on("error", (err) => {
      reject(new Error(`failed to spawn claude stop: ${err.message}`));
    });

    proc.on("close", (code) => {
      if (code !== 0) {
        const stderr = Buffer.concat(errChunks).toString("utf8");
        reject(new Error(`claude stop exited with code ${code}: ${stderr.slice(0, 200)}`));
        return;
      }
      resolve({ ok: true });
    });
  });
}
