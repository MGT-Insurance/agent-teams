// Entry point: bare node:http server.
// Route table:
//   GET  /api/snapshot                        -> SnapshotEvent (one-shot)
//   GET  /api/events                          -> SSE stream of SnapshotEvent
//   GET  /api/initiatives/:id                 -> DrillInDetail
//   GET  /api/initiatives/:id/logs?session=   -> raw claude logs bytes (chunked)
//   POST /api/initiatives/:id/attach          -> { ok: true } (macOS terminal)
//   GET  /*                                   -> static SPA (dist/web/) in production
//
// Dev wiring: run the Vite dev server separately (Track B) and configure its
// vite.config.ts proxy to forward /api/** to this server (default port 4823).
// The static-serve fallback only activates when dashboard/web/dist/ exists.

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { join, extname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import type { DrillInDetail, WorkBead } from "@agent-teams/shared";
import { CliError, claudeAgentsJson, bdLabeledBeads, spawnClaudeLogs } from "./cli.js";
import { parseClaudeAgents, parseBdList, parseInitiative } from "./parse.js";
import { SseRegistry } from "./sse.js";
import { SnapshotManager } from "./snapshot.js";
import { launchAttach, isValidSessionId } from "./attach.js";
import { buildSnapshot } from "./snapshot.js";

const PORT = parseInt(process.env["PORT"] ?? "4823", 10);
const __dirname = fileURLToPath(new URL(".", import.meta.url));
// The web build output lives at dashboard/web/dist relative to server/src/.
const WEB_DIST = join(__dirname, "..", "..", "web", "dist");

const sse = new SseRegistry();
const snapshots = new SnapshotManager();

snapshots.start((event) => {
  sse.broadcast(event);
});

// --- MIME types for static serving ---
const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript",
  ".mjs": "application/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
};

// --- Helpers ---

function json(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Access-Control-Allow-Origin": "*",
  });
  res.end(payload);
}

const BODY_LIMIT = 64 * 1024; // 64KB

function parseBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolveBody, rejectBody) => {
    const chunks: Buffer[] = [];
    let total = 0;
    req.on("data", (c: Buffer) => {
      total += c.byteLength;
      if (total > BODY_LIMIT) {
        rejectBody(Object.assign(new Error("request body too large"), { code: 413 }));
        return;
      }
      chunks.push(c);
    });
    req.on("end", () => resolveBody(Buffer.concat(chunks).toString("utf8")));
    req.on("error", rejectBody);
  });
}

// Attempt to serve a static file; returns false if not found.
async function serveStatic(
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  // Normalise the path; default to index.html for SPA routing.
  const rel = urlPath === "/" || !urlPath.includes(".") ? "index.html" : urlPath.slice(1);
  const filePath = join(WEB_DIST, rel);

  // Guard against path traversal: resolved path must stay inside WEB_DIST.
  if (!resolve(filePath).startsWith(resolve(WEB_DIST) + sep)) {
    res.writeHead(400, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "invalid path" }));
    return true;
  }

  try {
    await stat(filePath);
    const data = await readFile(filePath);
    const mime = MIME[extname(filePath)] ?? "application/octet-stream";
    res.writeHead(200, { "Content-Type": mime });
    res.end(data);
    return true;
  } catch {
    return false;
  }
}

// --- Route handler ---

async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const method = req.method ?? "GET";
  const url = new URL(req.url ?? "/", `http://localhost:${PORT}`);
  const path = url.pathname;

  // CORS preflight
  if (method === "OPTIONS") {
    res.writeHead(204, {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
      "Access-Control-Allow-Headers": "Content-Type",
    });
    res.end();
    return;
  }

  // GET /api/snapshot
  if (method === "GET" && path === "/api/snapshot") {
    try {
      const event = snapshots.getLatest() ?? (await buildSnapshot());
      json(res, 200, event);
    } catch (err) {
      json(res, 502, {
        error: err instanceof CliError ? err.message : String(err),
      });
    }
    return;
  }

  // GET /api/events  (SSE)
  if (method === "GET" && path === "/api/events") {
    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
      "Access-Control-Allow-Origin": "*",
    });
    res.write(": connected\n\n");

    sse.add(res);

    // Push the latest snapshot immediately so the client doesn't wait 2s.
    const latest = snapshots.getLatest();
    if (latest !== null) {
      res.write(`data: ${JSON.stringify(latest)}\n\n`);
    }
    return;
  }

  // /api/initiatives/:id routes
  const initiativeMatch = /^\/api\/initiatives\/([^/]+)(\/.*)?$/.exec(path);
  if (initiativeMatch) {
    const id = decodeURIComponent(initiativeMatch[1] ?? "");
    const sub = initiativeMatch[2] ?? "";

    // GET /api/initiatives/:id/logs?session=<sessionId>
    if (method === "GET" && sub === "/logs") {
      const sessionId = url.searchParams.get("session");
      if (!sessionId) {
        json(res, 400, { error: "missing ?session= parameter" });
        return;
      }
      if (!isValidSessionId(sessionId)) {
        json(res, 400, { error: "invalid session id" });
        return;
      }

      res.writeHead(200, {
        "Content-Type": "application/octet-stream",
        "Transfer-Encoding": "chunked",
        "Access-Control-Allow-Origin": "*",
      });

      let ended = false;
      const teardown = spawnClaudeLogs(
        sessionId,
        (chunk) => {
          if (!ended) res.write(chunk);
        },
        () => {
          ended = true;
          res.end();
        },
        (err) => {
          if (!ended) {
            ended = true;
            // Can't change headers; just end the stream.
            res.end();
            console.error(`[logs] error for session ${sessionId}: ${err.message}`);
          }
        },
      );

      res.on("close", () => {
        ended = true;
        teardown();
      });
      return;
    }

    // POST /api/initiatives/:id/attach
    if (method === "POST" && sub === "/attach") {
      let body: string;
      try {
        body = await parseBody(req);
      } catch (err) {
        const status = (err as { code?: number }).code === 413 ? 413 : 400;
        json(res, status, { error: status === 413 ? "request body too large" : "could not read request body" });
        return;
      }

      let sessionId: string;
      try {
        const parsed: unknown = JSON.parse(body);
        if (
          typeof parsed !== "object" ||
          parsed === null ||
          typeof (parsed as Record<string, unknown>)["sessionId"] !== "string"
        ) {
          throw new Error("missing sessionId");
        }
        sessionId = (parsed as { sessionId: string }).sessionId;
      } catch {
        json(res, 400, { error: "body must be { sessionId: string }" });
        return;
      }

      if (!isValidSessionId(sessionId)) {
        json(res, 400, { error: "invalid session id" });
        return;
      }

      try {
        const result = await launchAttach(sessionId);
        json(res, 200, result);
      } catch (err) {
        json(res, 502, { error: String(err) });
      }
      return;
    }

    // GET /api/initiatives/:id
    if (method === "GET" && sub === "") {
      try {
        const snapshot = snapshots.getLatest() ?? (await buildSnapshot());
        const node = snapshot.initiatives.find((n) => n.initiative.id === id);
        if (!node) {
          json(res, 404, { error: `initiative ${id} not found` });
          return;
        }

        const { initiative } = node;

        // Fetch all background sessions and work beads for this initiative.
        const agentsRaw = await claudeAgentsJson();
        const allSessions = parseClaudeAgents(agentsRaw);

        // Fetch all beads labeled with this initiative's id.
        let workBeads: WorkBead[] = [];
        if (initiative.repo) {
          try {
            workBeads = parseBdList(await bdLabeledBeads(initiative.repo, initiative.id));
          } catch (err) {
            console.error(`[drill-in] bd work beads error for ${id}: ${(err as Error).message}`);
          }
        }

        // Sessions whose cwd matches this initiative's worktree.
        const sessions = allSessions.filter(
          (s) => s.cwd === initiative.worktree,
        );

        // Split on the lookahead `\n(?=session )` so each "session N, …" line
        // starts a new entry while preserving multi-line content within an entry.
        const notesHistory = initiative.notes
          .split(/\n(?=session )/i)
          .map((s) => s.trim())
          .filter(Boolean);

        const detail: DrillInDetail = {
          ...initiative,
          notesHistory,
          sessions,
          workBeads,
        };

        json(res, 200, detail);
      } catch (err) {
        json(res, 502, {
          error: err instanceof CliError ? err.message : String(err),
        });
      }
      return;
    }
  }

  // Static SPA fallback (production only).
  if (method === "GET") {
    const served = await serveStatic(res, path);
    if (served) return;
  }

  json(res, 404, { error: `${method} ${path} not found` });
}

const server = createServer((req, res) => {
  handle(req, res).catch((err: unknown) => {
    console.error("[server] unhandled error:", err);
    if (!res.headersSent) {
      json(res, 500, { error: "internal server error" });
    }
  });
});

server.listen(PORT, () => {
  console.log(`[server] listening on http://localhost:${PORT}`);
  console.log(
    "[server] dev: configure Vite proxy /api -> http://localhost:" + PORT,
  );
});
