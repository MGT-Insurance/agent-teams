// Core-path tests for launchSession in launch.ts.
// Stubs spawn with a real EventEmitter so event dispatch is synchronous,
// matching the pattern established in cli.test.ts.

import { describe, it, expect, vi, beforeEach } from "vitest";
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
