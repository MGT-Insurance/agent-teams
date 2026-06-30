// Tests for launchStop: valid id resolves, non-zero exit rejects, spawn error rejects.
// isValidSessionId is imported from attach.ts — the 8-hex regex is NOT duplicated here.

import { describe, it, expect, vi, afterEach } from "vitest";
import { EventEmitter } from "node:events";
import { spawn } from "node:child_process";
import { isValidSessionId } from "./attach.js";
import { launchStop } from "./stop.js";

// Module-level mock: replace node:child_process spawn with a vi.fn() so tests
// can control process lifecycle per-test without hitting a real child process.
vi.mock("node:child_process", async () => {
  const actual = await vi.importActual<typeof import("node:child_process")>("node:child_process");
  return { ...actual, spawn: vi.fn() };
});

type FakeProc = EventEmitter & { stderr: EventEmitter };

function makeProc(opts: {
  exitCode?: number;
  spawnError?: Error;
  stderrData?: string;
} = {}): FakeProc {
  const proc = new EventEmitter() as FakeProc;
  proc.stderr = new EventEmitter();

  process.nextTick(() => {
    if (opts.spawnError) {
      proc.emit("error", opts.spawnError);
    } else {
      if (opts.stderrData) {
        proc.stderr.emit("data", Buffer.from(opts.stderrData));
      }
      proc.emit("close", opts.exitCode ?? 0);
    }
  });

  return proc;
}

afterEach(() => {
  vi.mocked(spawn).mockReset();
});

describe("isValidSessionId (imported from attach.ts)", () => {
  it("accepts a well-formed short claude session id", () => {
    expect(isValidSessionId("21bd9e92")).toBe(true);
  });

  it("rejects an id that does not match the 8-hex pattern", () => {
    expect(isValidSessionId("not-valid")).toBe(false);
  });
});

describe("launchStop", () => {
  it("resolves { ok: true } when claude stop exits 0", async () => {
    vi.mocked(spawn).mockReturnValue(makeProc({ exitCode: 0 }) as unknown as ReturnType<typeof spawn>);
    await expect(launchStop("21bd9e92")).resolves.toEqual({ ok: true });
    expect(vi.mocked(spawn)).toHaveBeenCalledWith(
      "claude",
      ["stop", "21bd9e92"],
      expect.objectContaining({ stdio: ["ignore", "pipe", "pipe"] }),
    );
  });

  it("rejects with stderr slice when claude stop exits non-zero", async () => {
    vi.mocked(spawn).mockReturnValue(
      makeProc({ exitCode: 1, stderrData: "session not found" }) as unknown as ReturnType<typeof spawn>,
    );
    await expect(launchStop("21bd9e92")).rejects.toThrow(/claude stop exited with code 1.*session not found/);
  });

  it("rejects when spawn emits an error event", async () => {
    vi.mocked(spawn).mockReturnValue(
      makeProc({ spawnError: new Error("ENOENT: claude not found") }) as unknown as ReturnType<typeof spawn>,
    );
    await expect(launchStop("21bd9e92")).rejects.toThrow(/failed to spawn claude stop.*ENOENT/);
  });
});
