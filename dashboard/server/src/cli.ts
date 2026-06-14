// Exec wrappers for the agent-teams CLIs.
// All functions reject with a CliError (non-zero exit or spawn failure).
// Callers surface the error in the API response — do NOT swallow.

import { spawn } from "node:child_process";

export class CliError extends Error {
  constructor(
    public readonly command: string,
    public readonly exitCode: number | null,
    public readonly stderr: string,
    message: string,
  ) {
    super(message);
    this.name = "CliError";
  }
}

function runCli(cmd: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    const errChunks: Buffer[] = [];

    const proc = spawn(cmd, args, { stdio: ["ignore", "pipe", "pipe"] });

    proc.stdout.on("data", (chunk: Buffer) => chunks.push(chunk));
    proc.stderr.on("data", (chunk: Buffer) => errChunks.push(chunk));

    proc.on("error", (err) => {
      reject(
        new CliError(
          `${cmd} ${args.join(" ")}`,
          null,
          "",
          `failed to spawn ${cmd}: ${err.message}`,
        ),
      );
    });

    proc.on("close", (code) => {
      if (code !== 0) {
        const stderr = Buffer.concat(errChunks).toString("utf8");
        reject(
          new CliError(
            `${cmd} ${args.join(" ")}`,
            code,
            stderr,
            `${cmd} exited with code ${code}: ${stderr.slice(0, 200)}`,
          ),
        );
        return;
      }
      resolve(Buffer.concat(chunks).toString("utf8"));
    });
  });
}

// Returns raw JSON string from `ateam list-json`.
export function ateamListJson(): Promise<string> {
  return runCli("ateam", ["list-json"]);
}

// Returns the ateam workspace path (single line, trimmed).
export async function ateamWs(): Promise<string> {
  const out = await runCli("ateam", ["ws"]);
  return out.trim();
}

// Returns raw JSON string from `claude agents --json --all`.
export function claudeAgentsJson(): Promise<string> {
  return runCli("claude", ["agents", "--json", "--all"]);
}

// Returns raw JSON string from `bd -C <workspace> list --label human --json`.
export function bdHumanList(workspace: string): Promise<string> {
  return runCli("bd", ["-C", workspace, "list", "--label", "human", "--json"]);
}

// Returns raw JSON string from `bd -C <projectRepo> list --json`.
export function bdWorkBeads(projectRepo: string): Promise<string> {
  return runCli("bd", ["-C", projectRepo, "list", "--json"]);
}

// Spawns `claude logs <sessionId>` and pipes raw bytes to the provided callback.
// Calls onData for each chunk, onEnd when complete, onError on failure.
// Returns a teardown function that kills the process early (e.g. client disconnect).
export function spawnClaudeLogs(
  sessionId: string,
  onData: (chunk: Buffer) => void,
  onEnd: () => void,
  onError: (err: Error) => void,
): () => void {
  const proc = spawn("claude", ["logs", sessionId], {
    stdio: ["ignore", "pipe", "pipe"],
  });

  proc.stdout.on("data", onData);
  proc.stderr.on("data", onData); // logs may write TUI output to stderr too

  // Guard both error and close from double-firing.
  let fired = false;

  proc.on("error", (err) => {
    if (fired) return;
    fired = true;
    onError(
      new CliError("claude logs", null, "", `failed to spawn claude logs: ${err.message}`),
    );
  });

  proc.on("close", (code) => {
    if (fired) return;
    fired = true;
    if (code !== 0 && code !== null) {
      // Non-zero exit: surface the error but still end the response so the
      // caller's HTTP connection is not left hanging.
      onError(new CliError("claude logs", code, "", `claude logs exited with code ${code}`));
    } else {
      onEnd();
    }
  });

  return () => {
    proc.kill();
  };
}
