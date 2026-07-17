// Exec wrappers for the agent-teams CLIs.
// All functions reject with a CliError (non-zero exit or spawn failure).
// Callers surface the error in the API response — do NOT swallow.

import { spawn } from "node:child_process";
import { tmpdir } from "node:os";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { parseSendOutput } from "./mail.js";

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

// Kills a spawned child if it hangs past this long instead of blocking the
// caller forever (at-6nj — a hung bd/ateam/claude call previously blocked
// indefinitely with no way to recover).
const CHILD_TIMEOUT_MS = 10_000;

function runCli(cmd: string, args: string[], timeoutMs: number = CHILD_TIMEOUT_MS): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    const errChunks: Buffer[] = [];

    const proc = spawn(cmd, args, { stdio: ["ignore", "pipe", "pipe"] });

    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      proc.kill();
    }, timeoutMs);

    proc.stdout.on("data", (chunk: Buffer) => chunks.push(chunk));
    proc.stderr.on("data", (chunk: Buffer) => errChunks.push(chunk));

    proc.on("error", (err) => {
      clearTimeout(timer);
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
      clearTimeout(timer);
      if (timedOut) {
        reject(
          new CliError(
            `${cmd} ${args.join(" ")}`,
            code,
            "",
            `${cmd} timed out after ${timeoutMs}ms and was killed`,
          ),
        );
        return;
      }
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

// Cap on messages fetched via `ateam mail list --json`.
const MAIL_LIMIT = 1000;

// `ateam mail send` blocks on liveness/resume/respawn escalation (at-00o), well
// past the default child timeout — give it more room before we kill it.
const SEND_TIMEOUT_MS = 30_000;

// Returns raw JSON string from `ateam mail list --json --limit <MAIL_LIMIT>`.
// NON-DESTRUCTIVE: reads mail without marking it read. Never call `ateam mail
// inbox` here — that verb consumes/marks-read and would corrupt state for
// agents waiting on messages.
export function ateamDebugMailJson(): Promise<string> {
  return runCli("ateam", ["mail", "list", "--json", "--limit", String(MAIL_LIMIT)]);
}

// Returns raw JSON string from `ateam memories-json` (all role memories,
// tier + applied-signal joined in, sorted by key ascending).
export function ateamMemoriesJson(): Promise<string> {
  return runCli("ateam", ["memories-json"]);
}

// Returns the raw text `ateam learnings <role>` (or `ateam prime` for the
// "user" role — user memories are surfaced via prime's filtered/capped/
// truncated `user:` output, NOT the raw learnings dump, mirroring what's
// actually injected into a user-role session's context) would print.
export function ateamLearnings(role: string): Promise<string> {
  return runCli("ateam", role === "user" ? ["prime"] : ["learnings", role]);
}

// Sends a mail message via `ateam mail send <to> --file <tmp>`, writing the
// body to a temp file (ateam mail send has no inline-body flag). Returns the
// parsed message_id/recipient from stdout. Cleans up the temp file in all cases.
export async function ateamSend(
  to: string,
  body: string,
  sender?: string,
): Promise<{ messageId: string; recipient: string }> {
  const dir = await mkdtemp(join(tmpdir(), "ateam-send-"));
  const file = join(dir, "body.txt");
  try {
    await writeFile(file, body, "utf8");
    const args = ["mail", "send", to, "--file", file];
    if (sender) {
      args.push("--sender", sender);
    }
    const out = await runCli("ateam", args, SEND_TIMEOUT_MS);
    return parseSendOutput(out);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

// Closes a mail message's bead via `ateam mail close <id>`. Thin wrapper —
// `ateam` is the ONLY sanctioned write interface to the global workspace;
// never shell `bd -C` directly for mail.
export function ateamMailClose(id: string): Promise<string> {
  return runCli("ateam", ["mail", "close", id]);
}

// Purges closed mail via `ateam mail purge`. Without dryRun, permanently
// removes closed message beads older than olderThan (CLI defaults to "7d"
// when omitted); with dryRun, reports what would be removed without deleting.
export function ateamMailPurge(opts: { olderThan?: string; dryRun?: boolean }): Promise<string> {
  const args = ["mail", "purge"];
  if (opts.olderThan) {
    args.push("--older-than", opts.olderThan);
  }
  if (opts.dryRun) {
    args.push("--dry-run");
  }
  return runCli("ateam", args);
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

// Returns raw JSON string from `bd -C <workspace> list --status=closed --json`.
// Mirrors `ateam list-json` (which is `bd list --status=open --json`) but for the
// closed half — same RawInitiative shape, parsed via parseAteamListJson.
export function bdClosedInitiatives(workspace: string): Promise<string> {
  return runCli("bd", ["-C", workspace, "list", "--status=closed", "--json"]);
}

// Returns raw JSON string from `bd -C <repo> list --label <label> --json`.
export function bdLabeledBeads(repo: string, label: string): Promise<string> {
  return runCli("bd", ["-C", repo, "list", "--label", label, "--status=all", "--json"]);
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
