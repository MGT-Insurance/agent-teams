// Core-path tests for launchSession in launch.ts.
// Stubs spawn with a real EventEmitter so event dispatch is synchronous,
// matching the pattern established in cli.test.ts.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { writeFile } from "node:fs/promises";
import { EventEmitter } from "node:events";

// Build a minimal proc stub: real EventEmitter base so .on/.emit work,
// plus stub stdout/stderr channels.
interface ProcStub {
  pid: number;
  stdout: EventEmitter;
  stderr: EventEmitter;
  on: (event: string, listener: (...args: unknown[]) => void) => ProcStub;
  emit: (event: string, ...args: unknown[]) => boolean;
}

function makeProc(): ProcStub {
  const base = new EventEmitter() as EventEmitter & ProcStub;
  base.pid = 9999;
  base.stdout = new EventEmitter();
  base.stderr = new EventEmitter();
  return base;
}

let currentProc: ProcStub;

vi.mock("node:child_process", () => ({
  spawn: vi.fn(() => {
    currentProc = makeProc();
    return currentProc;
  }),
}));

vi.mock("node:fs/promises", () => ({
  mkdir: vi.fn().mockResolvedValue(undefined),
  writeFile: vi.fn().mockResolvedValue(undefined),
}));

// Dynamic import after mocks are registered so the module picks up the stubs.
const { launchSession } = await import("./launch.js");

describe("launchSession — core paths", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns { ok: false } when the spawn itself errors (e.g. ENOENT)", async () => {
    const resultP = launchSession("at-test1");
    // currentProc is set synchronously by the spawn mock. Emit the error event
    // before awaiting so settle() fires while the Promise is still pending.
    currentProc.emit("error", Object.assign(new Error("spawn ENOENT"), { code: "ENOENT" }));
    const result = await resultP;
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toMatch(/spawn ENOENT/);
      expect(result.log).toMatch(/launch-at-test1/);
    }
  });

  it("returns { ok: false } when the process exits non-zero (unknown initiative)", async () => {
    const resultP = launchSession("at-test2");
    currentProc.emit("close", 1);
    const result = await resultP;
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toMatch(/exited with code 1/);
    }
  });

  it("returns { ok: true } when the process exits 0 (successful launch)", async () => {
    const resultP = launchSession("at-test3");
    currentProc.emit("close", 0);
    const result = await resultP;
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.log).toMatch(/launch-at-test3/);
    }
  });
});

describe("launchSession — edge cases", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("captures stderr in detail on non-zero exit", async () => {
    const resultP = launchSession("at-detail");
    currentProc.stderr.emit("data", Buffer.from("ateam resume: closed initiative\n"));
    currentProc.emit("close", 1);
    const result = await resultP;
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.detail).toMatch(/ateam resume: closed initiative/);
    }
  });

  it("truncates detail to last 500 chars when combined output is long", async () => {
    const resultP = launchSession("at-long");
    // 1000-char output — detail should be the last 500.
    const longOutput = "a".repeat(500) + "b".repeat(500);
    currentProc.stdout.emit("data", Buffer.from(longOutput));
    currentProc.emit("close", 1);
    const result = await resultP;
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.detail).toBe("b".repeat(500));
    }
  });

  it("returns { ok: true } when child is still alive after the 3s fast-fail window", async () => {
    vi.useFakeTimers();
    const resultP = launchSession("at-timeout");
    // Don't emit close or error — child is still running.
    await vi.advanceTimersByTimeAsync(8100);
    const result = await resultP;
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.log).toMatch(/launch-at-timeout/);
    }
  });

  it("settle() is idempotent: close after timeout does not flip ok:true to ok:false", async () => {
    vi.useFakeTimers();
    const resultP = launchSession("at-race");
    // Timeout fires first, settling with ok:true.
    await vi.advanceTimersByTimeAsync(8100);
    // Late-arriving close event should be ignored.
    currentProc.emit("close", 1);
    const result = await resultP;
    expect(result.ok).toBe(true);
  });

  it("resolves normally even when writeFile rejects (log write failure swallowed)", async () => {
    vi.mocked(writeFile).mockRejectedValueOnce(new Error("ENOSPC: no space left on device"));
    const resultP = launchSession("at-logfail");
    currentProc.emit("close", 0);
    const result = await resultP;
    // Should still resolve ok:true — the log write failure must not propagate.
    expect(result.ok).toBe(true);
  });

  it("detail is undefined when non-zero exit has no output", async () => {
    const resultP = launchSession("at-noout");
    // No stdout/stderr emitted before close.
    currentProc.emit("close", 2);
    const result = await resultP;
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.detail).toBeUndefined();
    }
  });
});
