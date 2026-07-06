// Tests for splitNotesBlocks: the shared notes-block split used by both
// parse.ts's lastNotesBlock and index.ts's DrillInDetail.notesHistory.

import { describe, it, expect } from "vitest";
import { splitNotesBlocks } from "./notes.js";

describe("splitNotesBlocks", () => {
  it("splits on the lookahead before a line starting 'session '", () => {
    const notes =
      "session 1, 2026-06-30 — investigating flaky test.\nsession 2, 2026-07-01 — fix landed.";
    expect(splitNotesBlocks(notes)).toEqual([
      "session 1, 2026-06-30 — investigating flaky test.",
      "session 2, 2026-07-01 — fix landed.",
    ]);
  });

  it("is case-insensitive on the 'session ' marker", () => {
    const notes = "Session 1, kickoff.\nSESSION 2, wrap-up.";
    expect(splitNotesBlocks(notes)).toEqual(["Session 1, kickoff.", "SESSION 2, wrap-up."]);
  });

  it("preserves multi-line content within a single block", () => {
    const notes = "session 1, line one.\nmore detail on line two.\nsession 2, next entry.";
    expect(splitNotesBlocks(notes)).toEqual([
      "session 1, line one.\nmore detail on line two.",
      "session 2, next entry.",
    ]);
  });

  it("trims whitespace and drops empty blocks", () => {
    const notes = "\n\nsession 1, padded.  \n\n";
    expect(splitNotesBlocks(notes)).toEqual(["session 1, padded."]);
  });

  it("returns an empty array for empty notes", () => {
    expect(splitNotesBlocks("")).toEqual([]);
  });

  it("returns a single block when no 'session ' line is present", () => {
    expect(splitNotesBlocks("just some freeform text")).toEqual(["just some freeform text"]);
  });
});
