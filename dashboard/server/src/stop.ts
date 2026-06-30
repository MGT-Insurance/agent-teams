// Shell `claude stop <sessionId>` then remove the job directory so the entry
// disappears from `claude agents --json --all` on the next snapshot tick.
// This mirrors what the `claude agents` TUI does on Ctrl-X (delete ~/.claude/jobs/<id>/).
// Caller MUST validate id with isValidSessionId (from attach.ts) before calling.

import { spawn } from "node:child_process";
import { rm } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

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
      const jobDir = join(homedir(), ".claude", "jobs", sessionId);
      rm(jobDir, { recursive: true, force: true }).then(
        () => resolve({ ok: true }),
        () => resolve({ ok: true }),
      );
    });
  });
}
