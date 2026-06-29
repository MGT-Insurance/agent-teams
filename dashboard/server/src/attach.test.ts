// Tests for the short claude session id validation and AppleScript injection guard in attach.ts.
// `claude attach`/`claude logs` use the SHORT id (8 lowercase hex), not the full UUID.

import { describe, it, expect, vi, afterEach } from "vitest";
import { existsSync } from "node:fs";
import { isValidSessionId, isItermInstalled } from "./attach.js";

// Module-level mock: replace node:fs existsSync with a vi.fn() so tests can
// control its return value per-test without the "Cannot redefine property" error.
vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return { ...actual, existsSync: vi.fn() };
});

describe("isValidSessionId", () => {
  it("accepts a well-formed short claude session id", () => {
    expect(isValidSessionId("21bd9e92")).toBe(true);
  });

  it("accepts another valid short id", () => {
    expect(isValidSessionId("a1b2c3d4")).toBe(true);
  });

  it("rejects the full UUID (documents the original bug — was accepted, now rejected)", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38-a236de7c6703")).toBe(false);
  });

  it("rejects empty string", () => {
    expect(isValidSessionId("")).toBe(false);
  });

  it("rejects injection attempt with embedded double-quote", () => {
    expect(isValidSessionId('"; rm -rf /; echo "')).toBe(false);
  });

  it("rejects injection attempt with semicolon", () => {
    expect(isValidSessionId("abc; rm -rf /")).toBe(false);
  });

  it("rejects a plain string that is not a short id", () => {
    expect(isValidSessionId("not-a-uuid")).toBe(false);
  });

  it("rejects 7-char hex (too short)", () => {
    expect(isValidSessionId("21bd9e9")).toBe(false);
  });

  it("rejects 9-char hex (too long)", () => {
    expect(isValidSessionId("21bd9e921")).toBe(false);
  });

  it("rejects uppercase hex (must be lowercase)", () => {
    expect(isValidSessionId("21BD9E92")).toBe(false);
  });

  it("rejects non-hex characters", () => {
    expect(isValidSessionId("21bd9e9z")).toBe(false);
  });
});

describe("isItermInstalled", () => {
  afterEach(() => {
    vi.mocked(existsSync).mockReset();
  });

  it("returns true when /Applications/iTerm.app exists", () => {
    vi.mocked(existsSync).mockImplementation((p) => p === "/Applications/iTerm.app");
    expect(isItermInstalled()).toBe(true);
  });

  it("returns false when neither iTerm.app path exists", () => {
    vi.mocked(existsSync).mockReturnValue(false);
    expect(isItermInstalled()).toBe(false);
  });
});
