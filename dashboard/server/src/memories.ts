// Pure parsing/normalization helpers for the Memories tab (agent-teams-hvje).
// No process spawning here — see cli.ts for the `ateam memories-json` wrapper
// that calls this.

import type { MemoryEntry } from "@agent-teams/shared";

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function nullableStr(v: unknown): string | null {
  return typeof v === "string" ? v : null;
}

function tier(v: unknown): MemoryEntry["tier"] {
  return v === "hot" || v === "fresh" || v === "cold" ? v : "cold";
}

// Coerces the raw JSON array from `ateam memories-json` into MemoryEntry[].
// Defensive per-field: malformed/missing fields default rather than throw, but
// a non-array top level is a hard failure (the shape is fundamentally wrong).
export function normalizeMemoriesJson(raw: string): MemoryEntry[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(`ateam memories-json produced invalid JSON: ${String(err)}`);
  }
  if (!Array.isArray(parsed)) {
    throw new Error("ateam memories-json did not return an array");
  }
  return parsed.map((item): MemoryEntry => {
    const r = (item ?? {}) as Record<string, unknown>;
    return {
      role: str(r.role),
      key: str(r.key),
      slug: str(r.slug),
      tier: tier(r.tier),
      body: str(r.body),
      appliedCount: num(r.appliedCount),
      lastApplied: nullableStr(r.lastApplied),
    };
  });
}
