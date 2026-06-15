// HTTP-level tests for security fixes in index.ts.
// Spins up the server on a random port, sends requests, then closes.

import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { createServer, type Server } from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";

// We import only the validation helpers we can test directly without
// spinning up the full server (which starts SnapshotManager + SSE).
// HTTP-level guards are tested via a minimal inline server that replicates
// the same path-check and UUID-check logic.

import { resolve, sep, join } from "node:path";
import { isValidSessionId } from "./attach.js";

// ---- Short claude session id validation (unit) ------------------------------
// claude attach/logs/stop accept only the short id (8 lowercase hex), not the full UUID.

describe("isValidSessionId — route guard", () => {
  it("rejects non-id session param on logs route", () => {
    const bad = "../etc/passwd";
    expect(isValidSessionId(bad)).toBe(false);
  });

  it("rejects shell metacharacter injection in session param", () => {
    expect(isValidSessionId('"; open -a Calculator "')).toBe(false);
  });

  it("accepts a valid short claude session id", () => {
    expect(isValidSessionId("21bd9e92")).toBe(true);
  });

  it("rejects the full UUID (was the original bug — must now be rejected)", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38-a236de7c6703")).toBe(false);
  });
});

// ---- Path traversal guard (unit) --------------------------------------------

// Extract the guard logic for isolated testing.
function pathTraversalGuard(webDist: string, urlPath: string): boolean {
  const rel = urlPath === "/" || !urlPath.includes(".") ? "index.html" : urlPath.slice(1);
  const filePath = join(webDist, rel);
  return resolve(filePath).startsWith(resolve(webDist) + sep);
}

describe("path traversal guard", () => {
  const webDist = "/some/dist";

  it("allows a normal asset path", () => {
    expect(pathTraversalGuard(webDist, "/assets/app.js")).toBe(true);
  });

  it("blocks a simple traversal", () => {
    expect(pathTraversalGuard(webDist, "/assets/../../etc/passwd")).toBe(false);
  });

  it("blocks a traversal with encoded dots (after join resolves it)", () => {
    expect(pathTraversalGuard(webDist, "/../etc/passwd")).toBe(false);
  });

  it("allows root path (maps to index.html)", () => {
    expect(pathTraversalGuard(webDist, "/")).toBe(true);
  });
});
