// Tests for the UUID validation and AppleScript injection guard in attach.ts.

import { describe, it, expect } from "vitest";
import { isValidSessionId } from "./attach.js";

describe("isValidSessionId", () => {
  it("accepts a well-formed UUID v4", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38-a236de7c6703")).toBe(true);
  });

  it("accepts uppercase UUID v4", () => {
    expect(isValidSessionId("63216460-3CF0-4A08-88EE-818CB460F5A4")).toBe(true);
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

  it("rejects a plain string that is not a UUID", () => {
    expect(isValidSessionId("not-a-uuid")).toBe(false);
  });

  it("rejects UUID with wrong segment lengths", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38")).toBe(false);
  });

  it("rejects UUID with non-hex characters", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38-a236de7c670z")).toBe(false);
  });
});
