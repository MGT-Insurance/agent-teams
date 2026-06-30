// Tests for the launchSession function in api.ts.
// Focuses on how it constructs the thrown error message from the server's JSON body.

import { describe, it, expect, vi, afterEach } from "vitest";
import { launchSession } from "./api.js";

function stubFetch(status: number, body: unknown): void {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      statusText: status === 200 ? "OK" : "Bad Gateway",
      json: () => Promise.resolve(body),
    } as Response),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("launchSession api.ts — success", () => {
  it("returns { ok: true } on a 200 response", async () => {
    stubFetch(200, { ok: true });
    const result = await launchSession("at-ok");
    expect(result).toEqual({ ok: true });
  });
});

describe("launchSession api.ts — error message construction", () => {
  it("throws with the server error message on a 502", async () => {
    stubFetch(502, { ok: false, error: "osascript exited with code 1: ..." });
    await expect(launchSession("at-fail")).rejects.toThrow(
      "osascript exited with code 1: ...",
    );
  });

  it("falls back to status-based message when body.error is absent", async () => {
    stubFetch(502, {});
    await expect(launchSession("at-noerr")).rejects.toThrow(
      "launch-session failed: 502",
    );
  });

  it("throws on 404 when initiative not found", async () => {
    stubFetch(404, { error: "initiative at-missing not found" });
    await expect(launchSession("at-missing")).rejects.toThrow(
      "initiative at-missing not found",
    );
  });

  it("throws on 400 when no worktree on this machine", async () => {
    stubFetch(400, { error: "initiative at-nowt has no worktree on this machine" });
    await expect(launchSession("at-nowt")).rejects.toThrow(
      "initiative at-nowt has no worktree on this machine",
    );
  });
});
