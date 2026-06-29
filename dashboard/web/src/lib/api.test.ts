// Edge-case tests for the launchSession function in api.ts.
// Focuses on how it constructs the thrown error message from the server's
// JSON body (error / detail / log fields).

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
  it("returns { ok: true, log } on a 200 response", async () => {
    stubFetch(200, { ok: true, log: "/logs/launch-at-ok.log" });
    const result = await launchSession("at-ok");
    expect(result).toEqual({ ok: true, log: "/logs/launch-at-ok.log" });
  });

  it("returns { ok: true, log: '' } when 200 body omits the log field", async () => {
    stubFetch(200, { ok: true });
    const result = await launchSession("at-nolog");
    expect(result).toEqual({ ok: true, log: "" });
  });
});

describe("launchSession api.ts — error message construction", () => {
  it("throws with just the error message when only error is present", async () => {
    stubFetch(502, { ok: false, error: "ateam resume exited with code 1" });
    await expect(launchSession("at-fail")).rejects.toThrow(
      "ateam resume exited with code 1",
    );
  });

  it("throws with all three lines when error + detail + log are present", async () => {
    stubFetch(502, {
      ok: false,
      error: "ateam resume exited with code 1",
      detail: "initiative at-ggz is closed — use ateam reopen first\n",
      log: "/logs/launch-at-ggz.log",
    });
    let caught: Error | null = null;
    try {
      await launchSession("at-ggz");
    } catch (e) {
      caught = e as Error;
    }
    expect(caught).not.toBeNull();
    const lines = caught!.message.split("\n");
    // First line: the error text.
    expect(lines[0]).toBe("ateam resume exited with code 1");
    // Second line: detail trimmed.
    expect(lines[1]).toBe(
      "initiative at-ggz is closed — use ateam reopen first",
    );
    // Third line: log path prefixed.
    expect(lines[2]).toBe("Log: /logs/launch-at-ggz.log");
    expect(lines).toHaveLength(3);
  });

  it("trims whitespace from detail before joining", async () => {
    stubFetch(502, {
      ok: false,
      error: "cmd failed",
      detail: "  output with spaces  \n",
    });
    let caught: Error | null = null;
    try {
      await launchSession("at-trim");
    } catch (e) {
      caught = e as Error;
    }
    expect(caught).not.toBeNull();
    const lines = caught!.message.split("\n");
    expect(lines[1]).toBe("output with spaces");
  });

  it("falls back to status-based message when body.error is absent", async () => {
    stubFetch(502, {});
    await expect(launchSession("at-noerr")).rejects.toThrow(
      "launch-session failed: 502",
    );
  });

  it("omits Log line when log field is absent in the failure body", async () => {
    stubFetch(502, {
      ok: false,
      error: "cmd failed",
      detail: "some detail",
    });
    let caught: Error | null = null;
    try {
      await launchSession("at-nolog");
    } catch (e) {
      caught = e as Error;
    }
    expect(caught).not.toBeNull();
    expect(caught!.message).not.toMatch(/Log:/);
    expect(caught!.message.split("\n")).toHaveLength(2);
  });
});
