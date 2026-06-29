// Isolated handler for POST /api/initiatives/:id/launch-session.
// Extracted from index.ts so it can be unit-tested with a mocked spawn.
//
// Success shape (200): { ok: true; log: string }
// Failure shape (502): { ok: false; error: string; detail?: string; log?: string }

import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

export type LaunchResult =
  | { ok: true; log: string }
  | { ok: false; error: string; detail?: string; log?: string };

// Fast-fail window in ms. On the FIRST invocation in a fresh node process the
// ateam Go binary + embedded-Dolt init can take several seconds, so 3s was too
// short: the timeout fired with empty output even for a closed/bogus initiative,
// producing the exact silent-false-success we're fixing. Real failures always
// exit non-zero within this window; successful launches resolve via close(0) and
// never wait for it — so widening only affects the max latency of the rare
// "child still alive" fallback path.
const LAUNCH_TIMEOUT_MS = 8000;

export function launchSession(id: string): Promise<LaunchResult> {
  const home = process.env["HOME"] ?? "/tmp";
  const logsDir = join(home, ".agent-teams", "logs");
  const logFile = join(logsDir, `launch-${id}-${Date.now()}.log`);

  // Augment PATH so ateam is findable even when the server was launched by a
  // GUI or launchd agent that inherits a bare environment.
  const augmentedPath = [
    join(home, ".local", "bin"),
    "/usr/local/bin",
    "/opt/homebrew/bin",
    process.env["PATH"] ?? "",
  ].join(":");

  // Ensure the log dir exists before writeFile is called. This is
  // fire-and-forget: settle() uses writeFile(...).catch() so a missing dir
  // just drops the log write silently rather than crashing the handler.
  mkdir(logsDir, { recursive: true }).catch(() => {/* ignore */});

  const output: string[] = [];
  const child = spawn("ateam", ["resume", id], {
    stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, PATH: augmentedPath },
  });

  child.stdout.on("data", (chunk: Buffer) => output.push(chunk.toString("utf8")));
  child.stderr.on("data", (chunk: Buffer) => output.push(chunk.toString("utf8")));

  return new Promise<LaunchResult>((resolve) => {
    let settled = false;

    function settle(result: LaunchResult) {
      if (settled) return;
      settled = true;

      // Write combined output to the log file (failure-tolerant).
      const combined = output.join("");
      const header =
        `argv: ateam resume ${id}\n` +
        `pid: ${child.pid ?? "?"}\n` +
        `result: ${result.ok ? "ok" : result.error}\n` +
        `time: ${new Date().toISOString()}\n\n`;
      writeFile(logFile, header + combined).catch(() => {/* ignore log write failure */});

      resolve(result);
    }

    child.on("error", (err) => {
      settle({ ok: false, error: `failed to spawn ateam: ${err.message}`, log: logFile });
    });

    child.on("close", (code) => {
      if (code === 0) {
        settle({ ok: true, log: logFile });
      } else {
        const combined = output.join("");
        settle({
          ok: false,
          error: `ateam resume exited with code ${code ?? "null"}`,
          detail: combined.slice(-500) || undefined,
          log: logFile,
        });
      }
    });

    // Still alive after the window → treat as a successful background launch.
    // Do NOT kill — it's the DRI session we wanted to start.
    setTimeout(() => {
      settle({ ok: true, log: logFile });
    }, LAUNCH_TIMEOUT_MS);
  });
}
