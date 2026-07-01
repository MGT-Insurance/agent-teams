// Tests for cli.ts wrappers: bdLabeledBeads and spawnClaudeLogs.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { EventEmitter } from "node:events";

// Build a minimal proc stub that satisfies the parts of ChildProcess we use.
interface ProcStub {
  stdout: EventEmitter;
  stderr: EventEmitter;
  kill: () => void;
  emit: (event: string, ...args: unknown[]) => boolean;
  on: (event: string, listener: (...args: unknown[]) => void) => ProcStub;
}

function makeProc(): ProcStub {
  const base = new EventEmitter();
  const stub = Object.assign(base, {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: vi.fn(),
  });
  return stub as unknown as ProcStub;
}

let currentProc: ProcStub;

vi.mock("node:child_process", () => ({
  spawn: vi.fn(() => {
    currentProc = makeProc();
    return currentProc;
  }),
}));

const { spawnClaudeLogs, bdLabeledBeads } = await import("./cli.js");

describe("spawnClaudeLogs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls onError (not onEnd) on non-zero exit", () => {
    const onData = vi.fn();
    const onEnd = vi.fn();
    const onError = vi.fn();

    spawnClaudeLogs("21bd9e92-ad92-4758-9a38-a236de7c6703", onData, onEnd, onError);
    currentProc.emit("close", 1);

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onEnd).not.toHaveBeenCalled();
  });

  it("fired flag prevents double-fire when error then close both emit", () => {
    const onData = vi.fn();
    const onEnd = vi.fn();
    const onError = vi.fn();

    spawnClaudeLogs("21bd9e92-ad92-4758-9a38-a236de7c6703", onData, onEnd, onError);
    currentProc.emit("error", new Error("spawn failed"));
    currentProc.emit("close", 1);

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onEnd).not.toHaveBeenCalled();
  });

  it("calls onEnd on clean exit (code 0)", () => {
    const onData = vi.fn();
    const onEnd = vi.fn();
    const onError = vi.fn();

    spawnClaudeLogs("21bd9e92-ad92-4758-9a38-a236de7c6703", onData, onEnd, onError);
    currentProc.emit("close", 0);

    expect(onEnd).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
  });

  it("calls onEnd on null exit code", () => {
    const onData = vi.fn();
    const onEnd = vi.fn();
    const onError = vi.fn();

    spawnClaudeLogs("21bd9e92-ad92-4758-9a38-a236de7c6703", onData, onEnd, onError);
    currentProc.emit("close", null);

    expect(onEnd).toHaveBeenCalledTimes(1);
    expect(onError).not.toHaveBeenCalled();
  });
});

describe("bdLabeledBeads", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("resolves with stdout on clean exit", async () => {
    const payload = JSON.stringify([{ id: "at-abc1", title: "test bead" }]);
    const promise = bdLabeledBeads("/repo/path", "at-abc");

    // Emit data then close 0.
    currentProc.stdout.emit("data", Buffer.from(payload));
    currentProc.emit("close", 0);

    const result = await promise;
    expect(result).toBe(payload);
  });

  it("rejects with CliError on non-zero exit", async () => {
    const promise = bdLabeledBeads("/repo/path", "at-abc");

    currentProc.stderr.emit("data", Buffer.from("some error"));
    currentProc.emit("close", 1);

    await expect(promise).rejects.toMatchObject({ name: "CliError", exitCode: 1 });
  });
});
