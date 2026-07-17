import { describe, it, expect } from "vitest";
import { normalizeMemoriesJson } from "./memories.js";

describe("normalizeMemoriesJson", () => {
  it("maps a full record", () => {
    const raw = JSON.stringify([
      {
        role: "dri",
        key: "dri:hot:verify-live",
        slug: "verify-live",
        tier: "hot",
        body: "Always verify live before declaring done.",
        appliedCount: 3,
        lastApplied: "2026-07-15T00:00:00Z",
      },
    ]);

    expect(normalizeMemoriesJson(raw)).toEqual([
      {
        role: "dri",
        key: "dri:hot:verify-live",
        slug: "verify-live",
        tier: "hot",
        body: "Always verify live before declaring done.",
        appliedCount: 3,
        lastApplied: "2026-07-15T00:00:00Z",
      },
    ]);
  });

  it("defaults missing/unknown fields on a partial record", () => {
    const raw = JSON.stringify([{ role: "implementer", key: "implementer:cold:foo" }]);

    expect(normalizeMemoriesJson(raw)).toEqual([
      {
        role: "implementer",
        key: "implementer:cold:foo",
        slug: "",
        tier: "cold",
        body: "",
        appliedCount: 0,
        lastApplied: null,
      },
    ]);
  });

  it("falls back to tier cold for an unrecognized tier value", () => {
    const raw = JSON.stringify([{ role: "dri", key: "dri:x", tier: "lukewarm" }]);
    expect(normalizeMemoriesJson(raw)[0]!.tier).toBe("cold");
  });

  it("coerces a non-number appliedCount to 0", () => {
    const raw = JSON.stringify([{ role: "dri", key: "dri:x", appliedCount: "three" }]);
    expect(normalizeMemoriesJson(raw)[0]!.appliedCount).toBe(0);
  });

  it("coerces a non-string lastApplied to null", () => {
    const raw = JSON.stringify([{ role: "dri", key: "dri:x", lastApplied: 12345 }]);
    expect(normalizeMemoriesJson(raw)[0]!.lastApplied).toBeNull();
  });

  it("passes through an explicit null lastApplied as null", () => {
    const raw = JSON.stringify([{ role: "dri", key: "dri:x", lastApplied: null }]);
    expect(normalizeMemoriesJson(raw)[0]!.lastApplied).toBeNull();
  });

  it("returns an empty array for no memories", () => {
    expect(normalizeMemoriesJson("[]")).toEqual([]);
  });

  it("throws on a non-array top level", () => {
    expect(() => normalizeMemoriesJson('{"not": "an array"}')).toThrow();
  });

  it("throws on invalid JSON", () => {
    expect(() => normalizeMemoriesJson("not json")).toThrow();
  });
});
