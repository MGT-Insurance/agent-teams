// Tests for cli.ts wrappers: bdLabeledBeads, spawnClaudeLogs, and ateamLearnings.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { EventEmitter } from "node:events";
import { spawn } from "node:child_process";

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

const { spawnClaudeLogs, bdLabeledBeads, ateamLearnings } = await import("./cli.js");

describe("runCli timeout (via bdLabeledBeads)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("kills the child and rejects with CliError if it hangs past the timeout", async () => {
    const promise = bdLabeledBeads("/repo/path", "at-abc");

    // Simulate a hung child: timer fires, we kill() it, then the OS reports
    // the process closed (as a real killed process eventually would).
    await vi.advanceTimersByTimeAsync(10_000);
    currentProc.emit("close", null);

    await expect(promise).rejects.toMatchObject({ name: "CliError" });
    expect(currentProc.kill).toHaveBeenCalledTimes(1);
  });

  it("does not kill the child if it closes before the timeout", async () => {
    const promise = bdLabeledBeads("/repo/path", "at-abc");

    currentProc.stdout.emit("data", Buffer.from("[]"));
    currentProc.emit("close", 0);
    await vi.advanceTimersByTimeAsync(10_000);

    await expect(promise).resolves.toBe("[]");
    expect(currentProc.kill).not.toHaveBeenCalled();
  });
});

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

// agent-teams-orb7.1: the "user" role's context injection mechanism is `ateam
// prime` (filtered/capped/truncated), NOT the raw `ateam learnings user` dump
// — verify the CLI wrapper picks the right subcommand per role.
describe("ateamLearnings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shells `ateam learnings <role>` for a non-user role", async () => {
    const promise = ateamLearnings("dri");
    currentProc.stdout.emit("data", Buffer.from("dri:hot:foo\nsome body\n"));
    currentProc.emit("close", 0);

    await expect(promise).resolves.toBe("dri:hot:foo\nsome body\n");
    expect(spawn).toHaveBeenCalledWith("ateam", ["learnings", "dri"], expect.anything());
  });

  it("shells `ateam prime` (NOT `ateam learnings user`) for the user role", async () => {
    const promise = ateamLearnings("user");
    currentProc.stdout.emit("data", Buffer.from("primed context text"));
    currentProc.emit("close", 0);

    await expect(promise).resolves.toBe("primed context text");
    expect(spawn).toHaveBeenCalledWith("ateam", ["prime"], expect.anything());
  });

  it("rejects with CliError on non-zero exit", async () => {
    const promise = ateamLearnings("planner");

    currentProc.stderr.emit("data", Buffer.from("boom"));
    currentProc.emit("close", 1);

    await expect(promise).rejects.toMatchObject({ name: "CliError", exitCode: 1 });
  });
});
