// Core-path tests for launchTerminal in launch.ts.
// Mocks osascript spawn so no real terminal is opened during tests.

import { describe, it, expect, vi, afterEach } from "vitest";
import { EventEmitter } from "node:events";
import { existsSync } from "node:fs";

// Module-level mock: replace node:fs existsSync so isItermInstalled can be controlled.
vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return { ...actual, existsSync: vi.fn() };
});

interface ProcStub {
  stderr: EventEmitter;
  on: (event: string, listener: (...args: unknown[]) => void) => ProcStub;
  emit: (event: string, ...args: unknown[]) => boolean;
}

function makeProc(): ProcStub {
  return new EventEmitter() as EventEmitter & ProcStub;
}

let currentScript: string = "";
let currentProc: ProcStub;

vi.mock("node:child_process", () => ({
  spawn: vi.fn((_cmd: string, args: string[]) => {
    // Capture the AppleScript passed to osascript -e <script>.
    currentScript = args[1] ?? "";
    currentProc = Object.assign(makeProc(), { stderr: new EventEmitter() });
    return currentProc;
  }),
}));

// Dynamic import after mocks are registered so the module picks up the stubs.
const { launchTerminal } = await import("./launch.js");

const WORKTREE = "/Users/erlloyd/.agent-teams-worktrees/my-initiative";

describe("launchTerminal — core paths", () => {
  afterEach(() => {
    vi.mocked(existsSync).mockReset();
  });

  it("resolves { ok: true } when osascript exits 0", async () => {
    vi.mocked(existsSync).mockReturnValue(false); // Terminal.app path
    const resultP = launchTerminal(WORKTREE);
    currentProc.emit("close", 0);
    const result = await resultP;
    expect(result).toEqual({ ok: true });
  });

  it("rejects when osascript exits non-zero", async () => {
    vi.mocked(existsSync).mockReturnValue(false);
    const resultP = launchTerminal(WORKTREE);
    (currentProc.stderr as EventEmitter).emit("data", Buffer.from("execution error\n"));
    currentProc.emit("close", 1);
    await expect(resultP).rejects.toThrow(/osascript exited with code 1/);
  });

  it("rejects when spawn itself errors (e.g. ENOENT)", async () => {
    vi.mocked(existsSync).mockReturnValue(false);
    const resultP = launchTerminal(WORKTREE);
    currentProc.emit("error", Object.assign(new Error("spawn ENOENT"), { code: "ENOENT" }));
    await expect(resultP).rejects.toThrow(/failed to spawn osascript/);
  });

  it("AppleScript (Terminal.app) contains cd + worktree + claude", async () => {
    vi.mocked(existsSync).mockReturnValue(false); // no iTerm
    const resultP = launchTerminal(WORKTREE);
    currentProc.emit("close", 0);
    await resultP;
    expect(currentScript).toContain("cd");
    expect(currentScript).toContain(WORKTREE);
    expect(currentScript).toContain("claude");
    expect(currentScript).toContain("Terminal");
  });

  it("AppleScript (iTerm) contains cd + worktree + claude", async () => {
    // iTerm is present when existsSync returns true for the iTerm.app path.
    vi.mocked(existsSync).mockImplementation(
      (p) => p === "/Applications/iTerm.app"
    );
    const resultP = launchTerminal(WORKTREE);
    currentProc.emit("close", 0);
    await resultP;
    expect(currentScript).toContain("cd");
    expect(currentScript).toContain(WORKTREE);
    expect(currentScript).toContain("claude");
    expect(currentScript).toContain("iTerm");
  });

  it("includes stderr in rejection message on non-zero exit", async () => {
    vi.mocked(existsSync).mockReturnValue(false);
    const resultP = launchTerminal(WORKTREE);
    (currentProc.stderr as EventEmitter).emit("data", Buffer.from("AppleScript error -1708\n"));
    currentProc.emit("close", 2);
    await expect(resultP).rejects.toThrow(/AppleScript error -1708/);
  });
});
