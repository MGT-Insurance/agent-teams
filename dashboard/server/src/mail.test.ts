import { describe, it, expect } from "vitest";
import { normalizeMailJson, parseSendOutput } from "./mail.js";

describe("normalizeMailJson", () => {
  it("maps a full record", () => {
    const raw = JSON.stringify([
      {
        id: "at-abcd",
        to: "at-target",
        from: "Eric Lloyd",
        subject: "message from Eric Lloyd",
        body: "hello there",
        status: "acked",
        createdAt: "2026-07-12T00:00:00Z",
        readAt: "2026-07-12T00:05:00Z",
        readBy: "impl-hw71-3",
        thread: "thread-1",
      },
    ]);

    expect(normalizeMailJson(raw)).toEqual([
      {
        id: "at-abcd",
        to: "at-target",
        from: "Eric Lloyd",
        subject: "message from Eric Lloyd",
        body: "hello there",
        status: "acked",
        createdAt: "2026-07-12T00:00:00Z",
        readAt: "2026-07-12T00:05:00Z",
        readBy: "impl-hw71-3",
        thread: "thread-1",
      },
    ]);
  });

  it("defaults missing/unknown fields on a partial record", () => {
    const raw = JSON.stringify([{ id: "at-partial", to: "at-target" }]);

    expect(normalizeMailJson(raw)).toEqual([
      {
        id: "at-partial",
        to: "at-target",
        from: "",
        subject: "",
        body: "",
        status: "pending",
        createdAt: "",
        readAt: null,
        readBy: null,
        thread: null,
      },
    ]);
  });

  it("returns an empty array for an empty mailbox", () => {
    expect(normalizeMailJson("[]")).toEqual([]);
  });

  it("throws on a non-array top level", () => {
    expect(() => normalizeMailJson('{"not": "an array"}')).toThrow();
  });

  it("throws on invalid JSON", () => {
    expect(() => normalizeMailJson("not json")).toThrow();
  });
});

describe("parseSendOutput", () => {
  it("extracts messageId and recipient from the first two lines", () => {
    const stdout = "message_id: at-xyz9\nrecipient: at-target\n";
    expect(parseSendOutput(stdout)).toEqual({ messageId: "at-xyz9", recipient: "at-target" });
  });

  it("tolerates trailing liveness/note/respawn diagnostic lines", () => {
    const stdout = [
      "message_id: at-xyz9",
      "recipient: at-target",
      "liveness: recipient is active",
      "note: delivered to worktree",
      "recipient worktree=/Users/erlloyd/.agent-teams-worktrees/foo",
    ].join("\n");
    expect(parseSendOutput(stdout)).toEqual({ messageId: "at-xyz9", recipient: "at-target" });
  });

  it("throws when message_id/recipient lines are absent", () => {
    expect(() => parseSendOutput("error: recipient not found\n")).toThrow();
  });
});
