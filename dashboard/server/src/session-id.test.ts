// Direct test of the shared session-id validator itself (agent-teams-rybk.5.4),
// imported straight from @agent-teams/shared rather than through a re-export —
// server/src/attach.test.ts and stop.test.ts cover it too, but only via
// attach.ts's re-export, which would silently lose coverage if that re-export
// were ever removed.
import { describe, it, expect } from "vitest";
import { isValidSessionId } from "@agent-teams/shared";

describe("isValidSessionId (@agent-teams/shared)", () => {
  it("accepts a well-formed short claude session id", () => {
    expect(isValidSessionId("21bd9e92")).toBe(true);
  });

  it("rejects the full UUID form", () => {
    expect(isValidSessionId("21bd9e92-ad92-4758-9a38-a236de7c6703")).toBe(false);
  });

  it("rejects empty string", () => {
    expect(isValidSessionId("")).toBe(false);
  });

  it("rejects uppercase hex (must be lowercase)", () => {
    expect(isValidSessionId("21BD9E92")).toBe(false);
  });

  it("rejects wrong-length hex", () => {
    expect(isValidSessionId("21bd9e9")).toBe(false);
    expect(isValidSessionId("21bd9e921")).toBe(false);
  });
});
