// Unit tests for the parser functions.
// Fixtures are captured from real CLI output (ateam list-json + claude agents --json --all).

import { mkdtempSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { describe, it, expect } from "vitest";
import {
  extractPrUrl,
  extractEpic,
  extractLatestAsk,
  parseInitiative,
  parseAteamListJson,
  parseClaudeAgents,
  buildInitiativeNodes,
  buildOrphanSessions,
  buildInbox,
  deriveActivity,
  deriveDelivery,
  deriveExplicitGate,
  deriveNeedsHuman,
  deriveSessionSignal,
  derivePhase,
} from "./parse.js";
import type { RawInitiative, SessionState, ParsedInitiative } from "@agent-teams/shared";

// ---- Fixtures ---------------------------------------------------------------

// Captured from real `ateam list-json` output (at-v4e initiative).
const RAW_AT_V4E: RawInitiative = {
  id: "at-v4e",
  title: "Local web dashboard for agent-teams: inbox of things needing Eric",
  description:
    "problem: Local web dashboard for agent-teams: inbox of things needing Eric, a distinctive (non-table) live view of all agent teams, drill-in to logs/details, and a stretch terminal-attach\nrepo: /Users/ericlloyd/Code/agent-teams\nworktree: /Users/ericlloyd/.agent-teams-worktrees/local-web-dashboard-for-agent-teams-inbox-of\nbranch: local-web-dashboard-for-agent-teams-inbox-of\nteam: agent-teams-local-web-dashboard-for-agent-teams-inbox-of\nmode: bg\n\nGOAL (the loop to close): a local web dashboard",
  notes:
    "session 1, 2026-06-13, bg — resumed by id. Beginning discovery + clarify.\nsession 1 cont — DELIVERED — awaiting-merge. PR #3551: https://github.com/MGT-Insurance/midgard/pull/3551",
  status: "open",
  priority: "2",
  issue_type: "task",
  owner: "erlloyd@gmail.com",
  created_at: "2026-06-13T22:47:17Z",
  updated_at: "2026-06-13T23:00:12Z",
};

// Captured from real `ateam list-json` output (at-2jh — specialty quote, no PR in description).
const RAW_AT_2JH: RawInitiative = {
  id: "at-2jh",
  title: "Specialty Products quote API",
  description:
    "problem: Take over in-progress Specialty Products quote API\nrepo: /Users/ericlloyd/Code/midgard0\nworktree: /Users/ericlloyd/.agent-teams-worktrees/specialty-quote-api\nbranch: specialty-quote-api\nteam: midgard0-specialty-quote-api\nmode: bg",
  notes:
    "session 1 cont — DELIVERED — awaiting-merge. PR #3551: https://github.com/MGT-Insurance/midgard/pull/3551\n\nCI GREEN on PR #3551 after rename + py-SDK fix.",
  status: "open",
  priority: "2",
  issue_type: "task",
  owner: "erlloyd@gmail.com",
  created_at: "2026-06-12T22:37:05Z",
  updated_at: "2026-06-13T14:01:25Z",
};

// Captured from real `claude agents --json --all` output.
const REAL_SESSIONS_JSON = JSON.stringify([
  {
    pid: 45732,
    cwd: "/Users/ericlloyd/Code/midgard",
    kind: "interactive",
    startedAt: 1781299967240,
    sessionId: "63216460-3cf0-4a08-88ee-818cb460f5a4",
    status: "idle",
  },
  {
    id: "21bd9e92",
    cwd: "/Users/ericlloyd/.agent-teams-worktrees/local-web-dashboard-for-agent-teams-inbox-of",
    kind: "background",
    startedAt: 1781390837770,
    sessionId: "21bd9e92-ad92-4758-9a38-a236de7c6703",
    name: "local-web-dashboard-for-agent-teams-inbox-of",
    status: "busy",
    state: "working",
  },
  {
    id: "e8a3278e",
    cwd: "/Users/ericlloyd/.agent-teams-worktrees/per-initiative-token-cost-attribution-and",
    kind: "background",
    startedAt: 1781391438317,
    sessionId: "e8a3278e-cfa2-4711-ace9-9675519a59d0",
    name: "per-initiative-token-cost-attribution-and",
    status: "idle",
    state: "working",
  },
]);

// Session fixture for blocked/waiting state (agent-teams-blo).
const BLOCKED_SESSION: SessionState = {
  id: "5aa-blocked",
  cwd: "/Users/ericlloyd/.agent-teams-worktrees/some-blocked-initiative",
  kind: "background",
  startedAt: 1781400000000,
  sessionId: "5aa00000-0000-0000-0000-000000000000",
  name: "some-blocked-initiative",
  status: "waiting",
  state: "blocked",
};

const ENDED_SESSION: SessionState = {
  id: "5aa-ended",
  cwd: "/Users/ericlloyd/.agent-teams-worktrees/specialty-quote-api",
  kind: "background",
  startedAt: 1781400000001,
  sessionId: "5aa11111-0000-0000-0000-000000000000",
  name: "specialty-quote-api",
  // No pid — session self-stopped.
  status: "idle",
  state: "stopped",
};

// Process has exited and status is absent — session lingers in `claude agents --all`
// as a detached entry. Still attachable via `claude attach <id>`.
const DETACHED_SESSION: SessionState = {
  id: "deadbeef", // valid 8-hex — used for claude attach
  cwd: "/wt/detached-initiative",
  kind: "background",
  startedAt: 1781400000002,
  sessionId: "deadbeef-0000-0000-0000-000000000000",
  name: "detached-initiative",
  // No status — process exited, session no longer live.
};

// ---- extractPrUrl -----------------------------------------------------------

describe("extractPrUrl", () => {
  it("finds a GitHub PR URL in text", () => {
    const text = "DELIVERED — awaiting-merge. PR #3551: https://github.com/MGT-Insurance/midgard/pull/3551";
    expect(extractPrUrl(text)).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("returns null when no PR URL present", () => {
    expect(extractPrUrl("no link here")).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(extractPrUrl("")).toBeNull();
  });

  it("finds URL in multi-line text", () => {
    const text = "session 1\nsome context\nhttps://github.com/org/repo/pull/42\nmore text";
    expect(extractPrUrl(text)).toBe("https://github.com/org/repo/pull/42");
  });
});

// ---- extractEpic ------------------------------------------------------------

describe("extractEpic", () => {
  it("returns the epic id from description", () => {
    expect(extractEpic("repo: /r\nepic: agent-teams-x6ce\nworktree: /wt", "")).toBe("agent-teams-x6ce");
  });

  it("falls back to notes when description has no epic line", () => {
    expect(extractEpic("repo: /r\nworktree: /wt", "session 1\nepic: agent-teams-abcd\ndone")).toBe("agent-teams-abcd");
  });

  it("returns null when neither description nor notes has epic:", () => {
    expect(extractEpic("repo: /r", "session 1")).toBeNull();
  });

  it("returns null for empty strings", () => {
    expect(extractEpic("", "")).toBeNull();
  });

  it("description takes precedence over notes", () => {
    expect(extractEpic("epic: desc-epic\n", "epic: notes-epic\n")).toBe("desc-epic");
  });
});

// ---- parseInitiative --------------------------------------------------------

describe("parseInitiative", () => {
  it("parses structured fields from description text", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    expect(parsed.repo).toBe("/Users/ericlloyd/Code/agent-teams");
    expect(parsed.worktree).toBe(
      "/Users/ericlloyd/.agent-teams-worktrees/local-web-dashboard-for-agent-teams-inbox-of",
    );
    expect(parsed.branch).toBe("local-web-dashboard-for-agent-teams-inbox-of");
    expect(parsed.team).toBe("agent-teams-local-web-dashboard-for-agent-teams-inbox-of");
    expect(parsed.mode).toBe("bg");
  });

  it("extracts problem from description", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    expect(parsed.problem).toContain("Local web dashboard");
  });

  it("extracts PR URL from notes", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    expect(parsed.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("extracts PR URL from at-2jh notes", () => {
    const parsed = parseInitiative(RAW_AT_2JH);
    expect(parsed.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("returns null prUrl when neither notes nor description has a PR URL", () => {
    const raw: RawInitiative = { ...RAW_AT_V4E, notes: "no pr here", description: "just text" };
    const parsed = parseInitiative(raw);
    expect(parsed.prUrl).toBeNull();
  });

  it("preserves raw initiative fields", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    expect(parsed.id).toBe("at-v4e");
    expect(parsed.title).toBe(RAW_AT_V4E.title);
    expect(parsed.status).toBe("open");
  });

  it("extracts epic id from description when present", () => {
    const raw: RawInitiative = { ...RAW_AT_V4E, description: "repo: /r\nepic: agent-teams-x6ce\n", notes: "" };
    const parsed = parseInitiative(raw);
    expect(parsed.epic).toBe("agent-teams-x6ce");
  });

  it("falls back to notes for epic when description lacks it", () => {
    const raw: RawInitiative = { ...RAW_AT_V4E, description: "repo: /r\n", notes: "session 1\nepic: agent-teams-abcd\n" };
    const parsed = parseInitiative(raw);
    expect(parsed.epic).toBe("agent-teams-abcd");
  });

  it("sets epic to null for legacy initiatives without epic field", () => {
    const raw: RawInitiative = { ...RAW_AT_V4E, description: "repo: /r\n", notes: "no epic here" };
    const parsed = parseInitiative(raw);
    expect(parsed.epic).toBeNull();
  });
});

// ---- parseAteamListJson -----------------------------------------------------

describe("parseAteamListJson", () => {
  it("parses an array of initiatives", () => {
    const json = JSON.stringify([RAW_AT_V4E, RAW_AT_2JH]);
    const parsed = parseAteamListJson(json);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.id).toBe("at-v4e");
    expect(parsed[1]?.id).toBe("at-2jh");
  });

  it("returns empty array for empty JSON array", () => {
    expect(parseAteamListJson("[]")).toHaveLength(0);
  });

  it("throws on invalid JSON", () => {
    expect(() => parseAteamListJson("not json")).toThrow();
  });

  it("throws when result is not an array", () => {
    expect(() => parseAteamListJson('{"key":"val"}')).toThrow(
      "ateam list-json did not return an array",
    );
  });
});

// ---- parseClaudeAgents (join logic) -----------------------------------------

describe("parseClaudeAgents", () => {
  it("parses real agents fixture with background and interactive entries", () => {
    const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);
    expect(sessions).toHaveLength(3);
  });

  it("interactive session has no id/name/state", () => {
    const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);
    const interactive = sessions.find((s) => s.kind === "interactive");
    expect(interactive).toBeDefined();
    expect(interactive?.id).toBeUndefined();
    expect(interactive?.name).toBeUndefined();
    expect(interactive?.state).toBeUndefined();
  });

  it("background session has id/name/state", () => {
    const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);
    const bg = sessions.find((s) => s.kind === "background" && s.id === "21bd9e92");
    expect(bg).toBeDefined();
    expect(bg?.name).toBe("local-web-dashboard-for-agent-teams-inbox-of");
    expect(bg?.state).toBe("working");
    expect(bg?.status).toBe("busy");
  });
});

// ---- buildInitiativeNodes (join) --------------------------------------------

describe("buildInitiativeNodes", () => {
  const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);

  it("joins background session to initiative by cwd === worktree", () => {
    const parsed: ParsedInitiative = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.session).not.toBeNull();
    expect(nodes[0]?.session?.id).toBe("21bd9e92");
  });

  it("session is null when no background session matches worktree", () => {
    const raw: RawInitiative = { ...RAW_AT_V4E, description: RAW_AT_V4E.description.replace(
      "/Users/ericlloyd/.agent-teams-worktrees/local-web-dashboard-for-agent-teams-inbox-of",
      "/no/match/path",
    )};
    const parsed = parseInitiative(raw);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.session).toBeNull();
  });

  it("does not join interactive sessions (kind=interactive)", () => {
    // The interactive session has cwd=/Users/ericlloyd/Code/midgard
    const raw: RawInitiative = { ...RAW_AT_2JH, description: RAW_AT_2JH.description.replace(
      "/Users/ericlloyd/.agent-teams-worktrees/specialty-quote-api",
      "/Users/ericlloyd/Code/midgard",
    )};
    const parsed = parseInitiative(raw);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    // Interactive sessions should not match (only background sessions are joined)
    expect(nodes[0]?.session).toBeNull();
  });

  it("activity is needs-human when initiative is in humanGatedIds (legacy fallback)", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set(["at-v4e"]));
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("activity is busy when matched session has status=busy", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.activity).toBe("busy");
  });

  it("counts all matched background sessions and prefers an alive primary", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const wt = parsed.worktree;
    const dead: SessionState = {
      id: "d", cwd: wt, kind: "background", startedAt: 1, sessionId: "dead-0000", name: "x", state: "done",
    };
    const alive: SessionState = {
      id: "a", cwd: wt, kind: "background", startedAt: 2, sessionId: "alive-0000", name: "x", status: "busy", state: "working",
    };
    // dead listed first — find() would have picked it; alive-preference overrides.
    const nodes = buildInitiativeNodes([parsed], [dead, alive], new Set());
    expect(nodes[0]?.sessionCount).toBe(2);
    expect(nodes[0]?.session?.sessionId).toBe("alive-0000");
  });

  it("sessionCount is 0 when no background session matches", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], [], new Set());
    expect(nodes[0]?.sessionCount).toBe(0);
    expect(nodes[0]?.session).toBeNull();
  });
});

// ---- deriveActivity ---------------------------------------------------------

describe("deriveActivity", () => {
  const baseInitiative: ParsedInitiative = parseInitiative(RAW_AT_V4E);
  const busySession: SessionState = {
    pid: 1,
    cwd: "/tmp/x",
    kind: "background",
    startedAt: 0,
    sessionId: "abc",
    status: "busy",
    state: "working",
  };
  const idleSession: SessionState = { ...busySession, status: "idle", state: "done" };

  it("needs-human overrides everything (explicit gate:review)", () => {
    expect(deriveActivity(baseInitiative, busySession, "review")).toBe("needs-human");
  });

  it("needs-human (generic) when PR URL + idle session (no explicit gate)", () => {
    const init = { ...baseInitiative, prUrl: "https://github.com/o/r/pull/1" };
    // delivery=pr-open, signal=ended, gate=null → needs-human:generic → "needs-human"
    expect(deriveActivity(init, idleSession, null)).toBe("needs-human");
  });

  it("busy when session status is busy", () => {
    expect(deriveActivity(baseInitiative, busySession, null)).toBe("busy");
  });

  it("busy when session has state=working even if status=idle", () => {
    const notDoneSession: SessionState = { ...busySession, status: "idle", state: "working" };
    expect(deriveActivity(baseInitiative, notDoneSession, null)).toBe("busy");
  });

  it("idle when session is not busy and state is not working", () => {
    const quietSession: SessionState = { ...busySession, status: "idle", state: "done" };
    // Use an initiative without a PR URL so the delivered branch doesn't fire.
    const noPrInitiative = { ...baseInitiative, prUrl: null as string | null };
    expect(deriveActivity(noPrInitiative, quietSession, null)).toBe("idle");
  });

  it("done when no session and initiative status is closed", () => {
    const closed = { ...baseInitiative, status: "closed" };
    expect(deriveActivity(closed, null, null)).toBe("done");
  });

  it("needs-human (generic) when no session but PR open and status is open", () => {
    // delivery=pr-open (prUrl present, status=open), signal=none, gate=null
    // → needsHuman="generic" (graceful degrade) → deriveActivity returns "needs-human"
    expect(deriveActivity(baseInitiative, null, null)).toBe("needs-human");
  });

  it("idle when no session, no PR, status is open", () => {
    const noPr = { ...baseInitiative, prUrl: null as string | null };
    expect(deriveActivity(noPr, null, null)).toBe("idle");
  });
});

// ---- derivePhase ------------------------------------------------------------

describe("derivePhase", () => {
  it("returns parked for needs-human notes", () => {
    expect(derivePhase("session 1\nneeds human approval")).toBe("parked");
  });

  it("returns delivered for awaiting-merge notes", () => {
    expect(derivePhase("session 1\nDELIVERED — awaiting-merge")).toBe("delivered");
  });

  it("returns executing for implementing notes", () => {
    expect(derivePhase("session 1\nnow executing the build")).toBe("executing");
  });

  it("returns investigating for discovery notes", () => {
    expect(derivePhase("session 1\ninvestigating data sources")).toBe("investigating");
  });

  it("returns planning for plan notes", () => {
    expect(derivePhase("session 1\nplanning decomposition")).toBe("planning");
  });

  it("returns done for closed notes", () => {
    expect(derivePhase("initiative complete and done")).toBe("done");
  });

  it("returns active as fallback", () => {
    expect(derivePhase("session 1\nsomething unrecognised")).toBe("active");
  });

  // ---- regression: no-notes / undefined safety (agent-teams-cbe) ----
  it("returns active for empty string without throwing", () => {
    expect(derivePhase("")).toBe("active");
  });

  it("returns active for undefined without throwing (defensive guard)", () => {
    // Cast to string to simulate a runtime call-site that bypassed parseInitiative.
    expect(derivePhase(undefined as unknown as string)).toBe("active");
  });

  it("returns active for null without throwing (defensive guard)", () => {
    expect(derivePhase(null as unknown as string)).toBe("active");
  });
});

// ---- parseInitiative: no-notes resilience (agent-teams-cbe) -----------------

describe("parseInitiative — no-notes resilience", () => {
  it("normalises missing notes to empty string", () => {
    const raw: RawInitiative = {
      ...RAW_AT_V4E,
      notes: undefined as unknown as string,
    };
    const parsed = parseInitiative(raw);
    expect(parsed.notes).toBe("");
  });

  it("normalises missing description to empty string", () => {
    const raw: RawInitiative = {
      ...RAW_AT_V4E,
      description: undefined as unknown as string,
    };
    const parsed = parseInitiative(raw);
    expect(parsed.description).toBe("");
  });

  it("still extracts a prUrl when notes is present", () => {
    // Regression: the coercion must not lose real notes data.
    const parsed = parseInitiative(RAW_AT_V4E);
    expect(parsed.notes).toBe(RAW_AT_V4E.notes);
    expect(parsed.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });
});

// ---- buildInitiativeNodes: per-initiative resilience (agent-teams-cbe) ------

describe("buildInitiativeNodes — per-initiative resilience", () => {
  const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);

  it("returns a snapshot including a no-notes initiative rather than throwing", () => {
    // Simulate at-3rw: freshly dispatched, notes === undefined at the registry level.
    const noNotes: ParsedInitiative = {
      ...parseInitiative(RAW_AT_V4E),
      id: "at-3rw",
      notes: undefined as unknown as string,
    };
    const good = parseInitiative(RAW_AT_2JH);

    // Must not throw.
    let nodes: ReturnType<typeof buildInitiativeNodes>;
    expect(() => {
      nodes = buildInitiativeNodes([noNotes, good], sessions, new Set());
    }).not.toThrow();

    // Both initiatives present in output.
    expect(nodes!).toHaveLength(2);
    expect(nodes!.some((n) => n.initiative.id === "at-3rw")).toBe(true);
    expect(nodes!.some((n) => n.initiative.id === "at-2jh")).toBe(true);
  });

  it("healthy initiatives are unaffected when one throws", () => {
    // Force a throw by injecting an initiative whose status accessor blows up.
    const throws: ParsedInitiative = {
      ...parseInitiative(RAW_AT_V4E),
      id: "bad-one",
      // Getter that throws when status.toLowerCase() is called inside deriveDelivery.
      get status(): string { throw new Error("simulated bad data"); },
    };
    const good = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([throws, good], sessions, new Set());

    // good initiative still present and correct.
    expect(nodes).toHaveLength(2);
    const goodNode = nodes.find((n) => n.initiative.id === "at-2jh");
    expect(goodNode?.delivery).toBe("pr-open");

    // bad-one degrades to minimal safe node.
    const badNode = nodes.find((n) => n.initiative.id === "bad-one");
    expect(badNode?.activity).toBe("idle");
    expect(badNode?.needsHuman).toBe(false);
    expect(badNode?.session).toBeNull();
  });
});

// ---- buildInbox (two-dimension model) ---------------------------------------
//
// buildInbox now consumes InitiativeNode[] (which carry needsHuman) instead of
// re-deriving state from ParsedInitiative[] + humanGatedIds.

describe("buildInbox", () => {
  const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);

  // at-v4e: worktree matches a busy+working session -> needsHuman=false (refining)
  // at-2jh: no matched session, prUrl present -> needsHuman="review"

  it("includes waiting items (needsHuman=waiting) with kind='waiting'", () => {
    // Make at-v4e human-gated -> needsHuman="waiting"
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_V4E)],
      sessions,
      new Set(["at-v4e"]),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-v4e");
    expect(item).toBeDefined();
    expect(item?.kind).toBe("waiting");
  });

  it("waiting item nextAction is the constant fallback when no ask block present", () => {
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_V4E)],
      sessions,
      new Set(["at-v4e"]),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-v4e");
    expect(item?.nextAction).toBe("Look at the session for more info.");
  });

  it("includes generic items (needsHuman=generic) when delivered + no session", () => {
    // at-2jh: prUrl present, no matched session -> signal=none -> needsHuman="generic" (graceful degrade)
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_2JH)],
      sessions,
      new Set(),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-2jh");
    expect(item).toBeDefined();
    expect(item?.kind).toBe("generic");
    expect(item?.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("includes generic items (needsHuman=generic) when delivered + session ENDED (no explicit gate)", () => {
    // at-2jh: prUrl present, matched ENDED session, no labels -> signal=ended, gate=null -> needsHuman="generic"
    // (DEMOTED from "review": review now requires explicit gate:review label)
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_2JH)],
      [ENDED_SESSION],
      new Set(),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-2jh");
    expect(item).toBeDefined();
    expect(item?.kind).toBe("generic");
    expect(item?.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("does NOT include pr-open + working (refining) initiatives — the key correctness case", () => {
    // at-v4e: prUrl present, session is busy+working -> needsHuman=false (refining, not in inbox)
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_V4E)],
      sessions,
      new Set(),  // not human-gated
    );
    // Verify the node really has needsHuman=false before testing inbox
    expect(nodes[0]?.needsHuman).toBe(false);
    const inbox = buildInbox(nodes);
    expect(inbox.find((i) => i.initiativeId === "at-v4e")).toBeUndefined();
  });

  it("does NOT include pr-open + idle initiatives that are merged/closed", () => {
    const closed = { ...parseInitiative(RAW_AT_2JH), status: "closed" };
    const nodes = buildInitiativeNodes([closed], sessions, new Set());
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(0);
  });

  it("waiting takes priority over generic when humanGatedIds set (legacy path)", () => {
    // at-2jh: prUrl present, no working session, humanGatedIds set -> gate="question" -> needsHuman="waiting"
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_2JH)],
      sessions,
      new Set(["at-2jh"]),
    );
    const inbox = buildInbox(nodes);
    const items = inbox.filter((i) => i.initiativeId === "at-2jh");
    expect(items).toHaveLength(1);
    expect(items[0]?.kind).toBe("waiting");
  });

  it("returns empty array when all nodes have needsHuman=false", () => {
    // at-v4e is refining (busy+working), at-2jh with null prUrl has needsHuman=false
    const noPr = { ...parseInitiative(RAW_AT_2JH), prUrl: null as string | null };
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_V4E), noPr],
      sessions,
      new Set(),
    );
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(0);
  });

  it("does not include merged initiatives (needsHuman=false for merged when worktree exists)", () => {
    const done = { ...parseInitiative(RAW_AT_V4E), status: "done" };
    // Pass existsFn=true: worktree is still present (normal merged, not a zombie).
    const nodes = buildInitiativeNodes([done], sessions, new Set(["at-v4e"]), () => true);
    // merged + worktree exists: needsHuman=false (reap only fires when worktree is GONE)
    expect(nodes[0]?.needsHuman).toBe(false);
    const inbox = buildInbox(nodes);
    expect(inbox.find((i) => i.initiativeId === "at-v4e")).toBeUndefined();
  });
});

// ---- buildInbox: sessionId on detached sessions (agent-teams-u9f2) -----------
//
// A detached session (status absent, lingers in `claude agents --all`) still
// carries a short 8-hex id and is attachable via `claude attach <id>`.
// The inbox must expose that id so the dashboard can offer Attach instead of Launch.

describe("buildInbox — sessionId for detached/alive sessions (agent-teams-u9f2)", () => {
  function makeDetachedInit(): ParsedInitiative {
    return {
      id: "at-detached",
      title: "Detached initiative",
      description: `worktree: ${DETACHED_SESSION.cwd}`,
      notes: "",
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-30T00:00:00Z",
      updated_at: "2026-06-30T00:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: DETACHED_SESSION.cwd,
      branch: "at-detached",
      team: "t-x",
      mode: "bg",
      goal: "",
      prUrl: "https://github.com/org/repo/pull/99",
      labels: ["gate:review"],
      epic: null,
    };
  }

  it("sessionId is set from a detached session (no status) with a valid 8-hex id", () => {
    const nodes = buildInitiativeNodes([makeDetachedInit()], [DETACHED_SESSION], new Set());
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-detached");
    expect(item).toBeDefined();
    expect(item?.sessionId).toBe("deadbeef");
  });

  it("sessionId is set from an alive session (status present) with a valid 8-hex id", () => {
    const aliveSession: SessionState = {
      ...DETACHED_SESSION,
      id: "deadbeef",
      status: "idle",
      state: "stopped",
    };
    const nodes = buildInitiativeNodes([makeDetachedInit()], [aliveSession], new Set());
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-detached");
    expect(item?.sessionId).toBe("deadbeef");
  });

  it("sessionId is undefined when session has no id field", () => {
    const noIdSession: SessionState = {
      cwd: DETACHED_SESSION.cwd,
      kind: "background",
      startedAt: 0,
      sessionId: "deadbeef-0000-0000-0000-000000000000",
      // No `id` field.
    };
    const nodes = buildInitiativeNodes([makeDetachedInit()], [noIdSession], new Set());
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-detached");
    expect(item?.sessionId).toBeUndefined();
  });
});

// ---- deriveDelivery (agent-teams-3e6) ----------------------------------------

describe("deriveDelivery", () => {
  it("returns pr-open when prUrl present and status is open", () => {
    const init = parseInitiative(RAW_AT_V4E); // has prUrl, status="open"
    expect(deriveDelivery(init)).toBe("pr-open");
  });

  it("returns pr-open when prUrl present and status is anything other than closed/done", () => {
    const init = { ...parseInitiative(RAW_AT_V4E), status: "in_progress" };
    expect(deriveDelivery(init)).toBe("pr-open");
  });

  it("returns merged when status is closed (regardless of prUrl)", () => {
    const init = { ...parseInitiative(RAW_AT_V4E), status: "closed" };
    expect(deriveDelivery(init)).toBe("merged");
  });

  it("returns merged when status is done", () => {
    const init = { ...parseInitiative(RAW_AT_V4E), status: "done" };
    expect(deriveDelivery(init)).toBe("merged");
  });

  it("returns none when no prUrl and status is open", () => {
    const init = { ...parseInitiative(RAW_AT_V4E), prUrl: null as string | null };
    expect(deriveDelivery(init)).toBe("none");
  });
});

// ---- deriveSessionSignal (agent-teams-blo) ----------------------------------

describe("deriveSessionSignal", () => {
  it("null session -> 'none'", () => {
    expect(deriveSessionSignal(null)).toBe("none");
  });

  it("status=waiting -> 'waiting' (agent paused on human input)", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "waiting", state: "blocked" };
    expect(deriveSessionSignal(s)).toBe("waiting");
  });

  it("state=blocked -> 'waiting' (older API style)", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "idle", state: "blocked" };
    expect(deriveSessionSignal(s)).toBe("waiting");
  });

  it("status=busy -> 'working'", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "busy", state: "working" };
    expect(deriveSessionSignal(s)).toBe("working");
  });

  it("state=working, status=idle -> 'working'", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "idle", state: "working" };
    expect(deriveSessionSignal(s)).toBe("working");
  });

  it("status=idle, state=stopped -> 'ended'", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "idle", state: "stopped" };
    expect(deriveSessionSignal(s)).toBe("ended");
  });

  it("status=idle, state=done -> 'ended'", () => {
    const s: SessionState = { sessionId: "a", kind: "background", cwd: "/x", startedAt: 0, status: "idle", state: "done" };
    expect(deriveSessionSignal(s)).toBe("ended");
  });

  it("interactive session, status=idle -> 'ended' (not working, not blocked)", () => {
    const s: SessionState = { sessionId: "a", kind: "interactive", cwd: "/x", startedAt: 0, status: "idle" };
    expect(deriveSessionSignal(s)).toBe("ended");
  });
});

// ---- deriveExplicitGate (agent-teams-0rl) ------------------------------------

describe("deriveExplicitGate", () => {
  it("gate:review label -> 'review'", () => {
    expect(deriveExplicitGate(["gate:review", "human"])).toBe("review");
  });

  it("gate:review alone -> 'review'", () => {
    expect(deriveExplicitGate(["gate:review"])).toBe("review");
  });

  it("gate:question label -> 'question'", () => {
    expect(deriveExplicitGate(["gate:question", "human"])).toBe("question");
  });

  it("gate:question alone -> 'question'", () => {
    expect(deriveExplicitGate(["gate:question"])).toBe("question");
  });

  it("human-only (no gate:*) -> 'question' (legacy gate)", () => {
    expect(deriveExplicitGate(["human"])).toBe("question");
  });

  it("gate:review takes priority over gate:question", () => {
    expect(deriveExplicitGate(["gate:review", "gate:question"])).toBe("review");
  });

  it("empty labels array -> null", () => {
    expect(deriveExplicitGate([])).toBeNull();
  });

  it("undefined labels -> null (missing field resilience)", () => {
    expect(deriveExplicitGate(undefined)).toBeNull();
  });

  it("unrelated labels -> null", () => {
    expect(deriveExplicitGate(["some-other-label"])).toBeNull();
  });
});

// ---- deriveNeedsHuman (agent-teams-0rl) truth table -------------------------

describe("deriveNeedsHuman", () => {
  // Explicit gate cases (new behavior, agent-teams-0rl)
  it("gate:review -> 'review' (AUTHORITATIVE; wins over session)", () => {
    expect(deriveNeedsHuman("none", "working", "review")).toBe("review");
  });

  it("gate:review + delivered -> 'review' (authoritative)", () => {
    expect(deriveNeedsHuman("pr-open", "ended", "review")).toBe("review");
  });

  it("gate:review wins over working session", () => {
    expect(deriveNeedsHuman("pr-open", "working", "review")).toBe("review");
  });

  it("gate:question -> 'waiting' (agent asking a question)", () => {
    expect(deriveNeedsHuman("none", "none", "question")).toBe("waiting");
  });

  it("gate:question + working session -> 'waiting' (gate wins)", () => {
    expect(deriveNeedsHuman("none", "working", "question")).toBe("waiting");
  });

  it("gate:question + delivered -> 'waiting' (gate wins)", () => {
    expect(deriveNeedsHuman("pr-open", "none", "question")).toBe("waiting");
  });

  // Session-based cases (gate=null) — no declared gate, softer "check" tier
  it("session WAITING, no gate -> 'check' (soft tier; no declared ask)", () => {
    expect(deriveNeedsHuman("none", "waiting", null)).toBe("check");
  });

  it("session WAITING + delivered, no gate -> 'check' (soft tier; no declared ask)", () => {
    expect(deriveNeedsHuman("pr-open", "waiting", null)).toBe("check");
  });

  it("session WORKING -> false (refining after delivery, not in inbox)", () => {
    expect(deriveNeedsHuman("pr-open", "working", null)).toBe(false);
  });

  it("none + WORKING -> false (initial work in progress)", () => {
    expect(deriveNeedsHuman("none", "working", null)).toBe(false);
  });

  it("none + ENDED -> false (active + ended = idle/dormant, no PR)", () => {
    expect(deriveNeedsHuman("none", "ended", null)).toBe(false);
  });

  it("none + NONE -> false (active + no session = idle/dormant)", () => {
    expect(deriveNeedsHuman("none", "none", null)).toBe(false);
  });

  it("delivered + ENDED -> 'generic' (DEMOTED from review; no explicit gate)", () => {
    expect(deriveNeedsHuman("pr-open", "ended", null)).toBe("generic");
  });

  it("delivered + NONE -> 'generic' (graceful degrade; no session info)", () => {
    expect(deriveNeedsHuman("pr-open", "none", null)).toBe("generic");
  });

  it("merged -> false (done, nothing needed)", () => {
    expect(deriveNeedsHuman("merged", "none", null)).toBe(false);
  });

  it("merged + gate:review -> false (closed initiatives never need human)", () => {
    expect(deriveNeedsHuman("merged", "none", "review")).toBe(false);
  });

  it("merged + WAITING -> false (done wins over waiting)", () => {
    expect(deriveNeedsHuman("merged", "waiting", null)).toBe(false);
  });

  // reap rows (agent-teams-d10b.2): zombie = merged + worktree gone + session alive
  it("merged + !worktreeExists + alive session (idle) -> 'reap'", () => {
    expect(deriveNeedsHuman("merged", "ended", null, false)).toBe("reap");
  });

  it("merged + !worktreeExists + session working -> 'reap'", () => {
    expect(deriveNeedsHuman("merged", "working", null, false)).toBe("reap");
  });

  it("merged + worktreeExists TRUE + alive session -> false (not a zombie)", () => {
    expect(deriveNeedsHuman("merged", "ended", null, true)).toBe(false);
  });

  it("merged + !worktreeExists + signal='none' (no session) -> false (nothing to reap)", () => {
    expect(deriveNeedsHuman("merged", "none", null, false)).toBe(false);
  });
});

// ---- buildInitiativeNodes: new two-dimension fields (agent-teams-3e6) -------

describe("buildInitiativeNodes — two-dimension fields", () => {
  const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);

  it("delivery is pr-open when initiative has prUrl and is open", () => {
    const parsed = parseInitiative(RAW_AT_V4E); // has prUrl, status=open
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
  });

  it("needsHuman is generic when delivery=pr-open and no matched session", () => {
    // at-2jh: has prUrl, status=open, no matched session -> signal=none -> needsHuman="generic" (graceful degrade)
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
    expect(nodes[0]?.needsHuman).toBe("generic");
  });

  it("needsHuman is generic when delivery=pr-open and session ENDED (no explicit gate)", () => {
    // at-2jh: has prUrl, matched ENDED session, no labels -> signal=ended, gate=null -> needsHuman="generic"
    // (DEMOTED: delivered+ended is no longer "review" — review requires explicit gate:review label)
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], [ENDED_SESSION], new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
    expect(nodes[0]?.needsHuman).toBe("generic");
  });

  it("needsHuman is false when delivery=pr-open and working session present", () => {
    // at-v4e: has prUrl, matched session is busy+working
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("needsHuman is waiting when humanGatedIds set (legacy fallback -> gate:question)", () => {
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set(["at-2jh"]));
    expect(nodes[0]?.needsHuman).toBe("waiting");
  });

  it("delivery is merged when initiative status is closed", () => {
    const closed = { ...parseInitiative(RAW_AT_V4E), status: "closed" };
    // Pass existsFn=true: worktree still present (normal merged, not zombie).
    const nodes = buildInitiativeNodes([closed], sessions, new Set(), () => true);
    expect(nodes[0]?.delivery).toBe("merged");
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("delivery is none when no prUrl and status open", () => {
    const noPr = { ...parseInitiative(RAW_AT_V4E), prUrl: null as string | null };
    const nodes = buildInitiativeNodes([noPr], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("none");
  });

  it("session blocked (state=blocked) with no gate -> needsHuman='check' (soft tier, agent-teams-ja9c)", () => {
    // Blocked session + NO gate: signal="waiting", gate=null -> needsHuman="check" (not "waiting")
    const raw: RawInitiative = { ...RAW_AT_2JH, description: RAW_AT_2JH.description.replace(
      "/Users/ericlloyd/.agent-teams-worktrees/specialty-quote-api",
      "/Users/ericlloyd/.agent-teams-worktrees/some-blocked-initiative",
    )};
    const nodes = buildInitiativeNodes([parseInitiative(raw)], [BLOCKED_SESSION], new Set());
    expect(nodes[0]?.needsHuman).toBe("check");
    expect(nodes[0]?.activity).toBe("needs-human");
  });
});

// ---- spec-required attention state test cases (agent-teams-blo) -------------

describe("attention state: spec-required scenarios", () => {
  // Helper: make a ParsedInitiative with a specific worktree.
  function makeInit(id: string, haspr: boolean, status = "open"): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: /wt/${id}`,
      notes: "",
      status,
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-15",
      updated_at: "2026-06-15",
      problem: "",
      repo: "/repo",
      worktree: `/wt/${id}`,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: haspr ? `https://github.com/org/repo/pull/1` : null,
      epic: null,
    };
  }

  function makeSession(worktree: string, status: string, state?: string): SessionState {
    return {
      sessionId: `sess-${worktree}`,
      kind: "background",
      cwd: worktree,
      startedAt: 0,
      status: status as "idle" | "busy" | "waiting",
      state: state as "working" | "blocked" | "done" | "stopped" | undefined,
    };
  }

  it("session waiting/blocked, no gate -> needsHuman='check' (soft tier, agent-teams-ja9c)", () => {
    const init = makeInit("blocked-1", false);
    const sess = makeSession("/wt/blocked-1", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("check");
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("session working -> not needs-you (refining if delivered, working if active)", () => {
    const init = makeInit("working-1", true);
    const sess = makeSession("/wt/working-1", "busy", "working");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("delivered + session ended -> needsHuman='generic' (no explicit gate; DEMOTED from review)", () => {
    const init = makeInit("generic-via-ended-1", true);
    const sess = makeSession("/wt/generic-via-ended-1", "idle", "stopped");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("generic");
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("delivered + no session -> needsHuman='generic' (graceful degrade)", () => {
    const init = makeInit("generic-1", true);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe("generic");
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("active + no session -> needsHuman=false (idle/dormant)", () => {
    const init = makeInit("idle-1", false); // no PR
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
    expect(nodes[0]?.activity).toBe("idle");
  });

  it("active + session ended -> needsHuman=false (idle/dormant, no PR)", () => {
    const init = makeInit("idle-2", false);
    const sess = makeSession("/wt/idle-2", "idle", "done");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
    expect(nodes[0]?.activity).toBe("idle");
  });

  it("done initiative -> needsHuman=false when worktree still exists", () => {
    const init = makeInit("done-1", true, "closed");
    const sess = makeSession("/wt/done-1", "waiting", "blocked");
    // Pass existsFn=true: worktree present (normal merged, not zombie).
    const nodes = buildInitiativeNodes([init], [sess], new Set(), () => true);
    expect(nodes[0]?.needsHuman).toBe(false);
    expect(nodes[0]?.activity).toBe("done");
  });

  it("inbox: no-gate blocked session -> kind='check' (soft tier, agent-teams-ja9c)", () => {
    const init = makeInit("w-inbox", false);
    const sess = makeSession("/wt/w-inbox", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("check");
  });

  it("inbox: generic item when delivered + ended (no explicit gate)", () => {
    // delivered+ended is now "generic" (DEMOTED from "review"; review requires explicit gate:review)
    const init = makeInit("r-inbox", true);
    const sess = makeSession("/wt/r-inbox", "idle", "stopped");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("generic");
  });

  it("inbox: generic item has kind='generic' (delivered + no session)", () => {
    const init = makeInit("g-inbox", true);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("generic");
  });

  it("inbox: working session -> not in inbox", () => {
    const init = makeInit("notinbox", true);
    const sess = makeSession("/wt/notinbox", "busy", "working");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(0);
  });
});

// ---- gate:review label cases (agent-teams-0rl) --------------------------------
//
// These verify the AUTHORITATIVE review signal derived from explicit gate labels.

describe("buildInitiativeNodes — explicit gate:review label (agent-teams-0rl)", () => {
  // Helper: make a ParsedInitiative with a specific worktree and optional labels.
  function makeGateInit(
    id: string,
    labels: string[] | undefined,
    haspr: boolean,
  ): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: /wt/${id}`,
      notes: "session 1 — parked waiting on review",
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-15",
      updated_at: "2026-06-15",
      problem: "",
      repo: "/repo",
      worktree: `/wt/${id}`,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      labels,
      prUrl: haspr ? "https://github.com/org/repo/pull/1" : null,
      epic: null,
    };
  }

  function makeSessionForId(id: string, status: string, state?: string) {
    return {
      sessionId: `sess-${id}`,
      kind: "background" as const,
      cwd: `/wt/${id}`,
      startedAt: 0,
      status: status as "idle" | "busy" | "waiting",
      state: state as "working" | "blocked" | "done" | "stopped" | undefined,
    };
  }

  it("gate:review -> needsHuman='review' (AUTHORITATIVE)", () => {
    const init = makeGateInit("r1", ["gate:review", "human"], true);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe("review");
  });

  it("gate:review wins over working session -> needsHuman='review'", () => {
    const init = makeGateInit("r2", ["gate:review", "human"], true);
    const sess = makeSessionForId("r2", "busy", "working");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("review");
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("gate:question -> needsHuman='waiting'", () => {
    const init = makeGateInit("q1", ["gate:question", "human"], false);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe("waiting");
  });

  it("human-only label (legacy gate) -> needsHuman='waiting'", () => {
    const init = makeGateInit("h1", ["human"], false);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe("waiting");
  });

  it("delivered + ended + no gate -> needsHuman='generic' (NOT review)", () => {
    const init = makeGateInit("g1", undefined, true);
    const sess = makeSessionForId("g1", "idle", "stopped");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("generic");
  });

  it("gate:review initiative has delivery ring AND review badge (review item in inbox)", () => {
    const init = makeGateInit("r3", ["gate:review", "human"], true);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("review");
    expect(inbox[0]?.prUrl).toBe("https://github.com/org/repo/pull/1");
  });

  it("labels=[]: tolerate empty array -> no gate", () => {
    const init = makeGateInit("e1", [], false);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("labels=undefined: tolerate missing labels -> no gate", () => {
    const init = makeGateInit("e2", undefined, false);
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
  });
});

// ---- extractLatestAsk (agent-teams-1saz) ------------------------------------

describe("extractLatestAsk", () => {
  it("returns null when notes has no ask block", () => {
    expect(extractLatestAsk("no ask block here")).toBeNull();
  });

  it("returns null for empty string", () => {
    expect(extractLatestAsk("")).toBeNull();
  });

  it("parses decision from a well-formed block", () => {
    const notes = "<<<ateam-ask\ndecision: Should we deploy now?\n>>>";
    expect(extractLatestAsk(notes)).toEqual({ decision: "Should we deploy now?", recommendation: "", alternative: "" });
  });

  it("returns the LAST valid block when multiple blocks are present", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: First question.",
      ">>>",
      "some prose",
      "<<<ateam-ask",
      "decision: Second question.",
      ">>>",
    ].join("\n");
    expect(extractLatestAsk(notes)).toEqual({ decision: "Second question.", recommendation: "", alternative: "" });
  });

  it("skips an unclosed block at end of notes, keeps the last valid closed block", () => {
    // The unclosed block comes AFTER a closed one; closeLine returns -1 for it.
    const notes = [
      "<<<ateam-ask",
      "decision: Closed block.",
      ">>>",
      "some intervening notes",
      "<<<ateam-ask",
      "decision: Unclosed — no close marker follows.",
    ].join("\n");
    expect(extractLatestAsk(notes)).toEqual({ decision: "Closed block.", recommendation: "", alternative: "" });
  });

  it("skips blocks with empty decision", () => {
    const notes = "<<<ateam-ask\nrecommendation: Do X.\n>>>";
    expect(extractLatestAsk(notes)).toBeNull();
  });

  it("ignores >>> embedded in prose (must be line-anchored)", () => {
    const notes = "<<<ateam-ask\ndecision: X or >>> something\n>>>";
    // The inline >>> in the decision value is prose; the real close is on its own line.
    expect(extractLatestAsk(notes)).toEqual({ decision: "X or >>> something", recommendation: "", alternative: "" });
  });

  it("parses recommendation and alternative when present", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Should we roll back?",
      "recommendation: Yes — error rate is above 5%.",
      "alternative: Partial rollback to 10% traffic.",
      ">>>",
    ].join("\n");
    expect(extractLatestAsk(notes)).toEqual({
      decision: "Should we roll back?",
      recommendation: "Yes — error rate is above 5%.",
      alternative: "Partial rollback to 10% traffic.",
    });
  });

  it("returns empty strings for recommendation/alternative when fields are absent", () => {
    const notes = "<<<ateam-ask\ndecision: Go or no-go?\n>>>";
    const result = extractLatestAsk(notes);
    expect(result?.recommendation).toBe("");
    expect(result?.alternative).toBe("");
  });
});

// ---- buildInbox: updatedAt + nextAction (agent-teams-1saz) -------------------

describe("buildInbox — updatedAt and nextAction", () => {
  function makeInit(
    id: string,
    kind: "review" | "waiting" | "generic",
    notes = "",
    labels?: string[],
  ): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: /wt/${id}`,
      notes,
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-20T00:00:00Z",
      updated_at: "2026-06-25T12:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: `/wt/${id}`,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: kind === "generic" || kind === "review" ? "https://github.com/org/repo/pull/1" : null,
      labels,
      epic: null,
    };
  }

  it("review item has updatedAt from initiative.updated_at", () => {
    const init = makeInit("rev", "review", "", ["gate:review"]);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.updatedAt).toBe("2026-06-25T12:00:00Z");
  });

  it("review item nextAction is the constant string", () => {
    const init = makeInit("rev", "review", "", ["gate:review"]);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe("Review the PR and merge or send it back.");
  });

  it("generic item nextAction is the constant string", () => {
    const init = makeInit("gen", "generic");
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe(
      "Delivered with no gate — open the worktree to see what's needed.",
    );
  });

  it("generic item has updatedAt from initiative.updated_at", () => {
    const init = makeInit("gen", "generic");
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.updatedAt).toBe("2026-06-25T12:00:00Z");
  });

  it("waiting item: nextAction == decision when ask block present", () => {
    const notes = [
      "Starting the initiative.",
      "<<<ateam-ask",
      "decision: Should we use approach A or approach B?",
      ">>>",
    ].join("\n");
    const init = { ...makeInit("w-ask", "waiting"), notes, labels: ["gate:question", "human"] };
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe("Should we use approach A or approach B?");
  });

  it("waiting item: fallback is constant when no ask block present", () => {
    const notes =
      "Early note.\nThis is the latest entry. It has more text after the period.";
    const init = { ...makeInit("w-fb", "waiting"), notes, labels: ["human"] };
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe("Look at the session for more info.");
  });

  it("waiting item: fallback is constant even when notes are very long (no ask block)", () => {
    const longLine = "A".repeat(200);
    const init = { ...makeInit("w-long", "waiting"), notes: longLine, labels: ["human"] };
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe("Look at the session for more info.");
  });

  it("waiting item has updatedAt from initiative.updated_at", () => {
    const init = { ...makeInit("w-ts", "waiting"), labels: ["human"] };
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.updatedAt).toBe("2026-06-25T12:00:00Z");
  });
});

// ---- buildInbox: recommendation/alternative passthrough (agent-teams-oc3p) ----

describe("buildInbox — recommendation and alternative", () => {
  function makeWaitingInit(notes: string): ParsedInitiative {
    return {
      id: "w-rec",
      title: "Rec test",
      description: "worktree: /wt/w-rec",
      notes,
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-26T00:00:00Z",
      updated_at: "2026-06-26T00:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: "/wt/w-rec",
      branch: "w-rec",
      team: "t-w-rec",
      mode: "bg",
      goal: "",
      prUrl: null,
      labels: ["human"],
      epic: null,
    };
  }

  it("waiting item carries recommendation and alternative from ask block", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Ship or hold?",
      "recommendation: Ship — tests are green.",
      "alternative: Hold 24h and rerun perf suite.",
      ">>>",
    ].join("\n");
    const init = makeWaitingInit(notes);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.recommendation).toBe("Ship — tests are green.");
    expect(inbox[0]?.alternative).toBe("Hold 24h and rerun perf suite.");
  });

  it("waiting item has empty recommendation/alternative when ask block omits them", () => {
    const notes = "<<<ateam-ask\ndecision: Go or no-go?\n>>>";
    const init = makeWaitingInit(notes);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.recommendation).toBe("");
    expect(inbox[0]?.alternative).toBe("");
  });

  it("non-waiting items always have empty recommendation/alternative", () => {
    const reviewInit: ParsedInitiative = {
      id: "rv",
      title: "Review init",
      description: "worktree: /wt/rv",
      notes: "",
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-26T00:00:00Z",
      updated_at: "2026-06-26T00:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: "/wt/rv",
      branch: "rv",
      team: "t-rv",
      mode: "bg",
      goal: "",
      prUrl: "https://github.com/org/repo/pull/1",
      labels: ["gate:review"],
      epic: null,
    };
    const nodes = buildInitiativeNodes([reviewInit], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("review");
    expect(inbox[0]?.recommendation).toBe("");
    expect(inbox[0]?.alternative).toBe("");
  });
});

// ---- buildInbox: onThisMachine (agent-teams-1l70) ----------------------------

describe("buildInbox — onThisMachine", () => {
  function makeWaitingInit(id: string, worktree: string): ParsedInitiative {
    return {
      id,
      title: `Init ${id}`,
      description: `worktree: ${worktree}`,
      notes: "",
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-26T00:00:00Z",
      updated_at: "2026-06-26T00:00:00Z",
      problem: "",
      repo: "/repo",
      worktree,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: null,
      labels: ["human"],
      epic: null,
    };
  }

  it("onThisMachine=true when worktree path exists on disk", () => {
    const tmp = mkdtempSync(`${tmpdir()}/at-test-`);
    try {
      const init = makeWaitingInit("tm-exists", tmp);
      const nodes = buildInitiativeNodes([init], [], new Set(["tm-exists"]));
      const inbox = buildInbox(nodes);
      expect(inbox[0]?.onThisMachine).toBe(true);
    } finally {
      rmdirSync(tmp);
    }
  });

  it("onThisMachine=false when worktree path doesn't exist", () => {
    const init = makeWaitingInit("tm-missing", "/does/not/exist/xxyyzz-agent-teams-test");
    const nodes = buildInitiativeNodes([init], [], new Set(["tm-missing"]));
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.onThisMachine).toBe(false);
  });

  it("onThisMachine=false when worktree is empty string", () => {
    const init = makeWaitingInit("tm-empty", "");
    const nodes = buildInitiativeNodes([init], [], new Set(["tm-empty"]));
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.onThisMachine).toBe(false);
  });
});

// ---- parseAteamListJson shape checks ----------------------------------------

describe("parseAteamListJson shape validation", () => {
  it("throws on element missing id field", () => {
    const bad = JSON.stringify([{ title: "no-id", description: "", notes: "", status: "open", priority: "1", issue_type: "task", owner: "x", created_at: "", updated_at: "" }]);
    expect(() => parseAteamListJson(bad)).toThrow("unexpected element shape");
  });

  it("throws on element with non-string id", () => {
    const bad = JSON.stringify([{ id: 42, title: "t", description: "", notes: "", status: "open", priority: "1", issue_type: "task", owner: "x", created_at: "", updated_at: "" }]);
    expect(() => parseAteamListJson(bad)).toThrow("unexpected element shape");
  });

  it("accepts valid element shape", () => {
    const good = JSON.stringify([RAW_AT_V4E]);
    expect(() => parseAteamListJson(good)).not.toThrow();
  });
});

// ---- parseClaudeAgents shape checks -----------------------------------------

describe("parseClaudeAgents shape validation", () => {
  it("accepts an element with no pid (stopped sessions have no pid)", () => {
    const stopped = JSON.stringify([{ sessionId: "abc", kind: "background", cwd: "/", startedAt: 0, status: "idle", state: "stopped" }]);
    expect(() => parseClaudeAgents(stopped)).not.toThrow();
  });

  it("throws on element missing sessionId field", () => {
    const bad = JSON.stringify([{ pid: 1, kind: "interactive", cwd: "/", startedAt: 0, status: "idle" }]);
    expect(() => parseClaudeAgents(bad)).toThrow("unexpected element shape");
  });

  it("accepts valid element shape", () => {
    expect(() => parseClaudeAgents(REAL_SESSIONS_JSON)).not.toThrow();
  });
});

// ---- buildOrphanSessions ----------------------------------------------------

describe("buildOrphanSessions", () => {
  const sessions = parseClaudeAgents(REAL_SESSIONS_JSON);
  // REAL_SESSIONS_JSON has:
  //   - interactive: cwd=/Users/ericlloyd/Code/midgard
  //   - bg id=21bd9e92: cwd matches RAW_AT_V4E worktree
  //   - bg id=e8a3278e: cwd=/Users/ericlloyd/.agent-teams-worktrees/per-initiative-token-cost-attribution-and (no initiative)

  const initiatives = [parseInitiative(RAW_AT_V4E), parseInitiative(RAW_AT_2JH)];

  it("includes background sessions whose cwd matches no initiative worktree", () => {
    const orphans = buildOrphanSessions(initiatives, sessions);
    expect(orphans.some((s) => s.id === "e8a3278e")).toBe(true);
  });

  it("excludes background sessions that match an initiative worktree", () => {
    const orphans = buildOrphanSessions(initiatives, sessions);
    expect(orphans.some((s) => s.id === "21bd9e92")).toBe(false);
  });

  it("excludes interactive sessions entirely", () => {
    const orphans = buildOrphanSessions(initiatives, sessions);
    expect(orphans.every((s) => s.kind === "background")).toBe(true);
  });

  it("returns empty array when all background sessions are matched", () => {
    // Provide an initiative whose worktree matches the only orphan bg session.
    const extraInitiative: ParsedInitiative = {
      ...parseInitiative(RAW_AT_V4E),
      id: "extra",
      worktree: "/Users/ericlloyd/.agent-teams-worktrees/per-initiative-token-cost-attribution-and",
    };
    const allMatched = buildOrphanSessions([...initiatives, extraInitiative], sessions);
    expect(allMatched).toHaveLength(0);
  });

  it("returns all background sessions when no initiatives exist", () => {
    const orphans = buildOrphanSessions([], sessions);
    // Only the 2 bg sessions — interactive excluded.
    expect(orphans).toHaveLength(2);
    expect(orphans.every((s) => s.kind === "background")).toBe(true);
  });
});

// ---- buildInitiativeNodes: worktreeExists + closed initiatives (at-gvv) -----

describe("buildInitiativeNodes — worktreeExists (at-gvv)", () => {
  function makeInit(id: string, worktree: string, status = "open"): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: ${worktree}`,
      notes: "",
      status,
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-15",
      updated_at: "2026-06-15",
      problem: "",
      repo: "/repo",
      worktree,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: null,
      epic: null,
    };
  }

  it("worktreeExists is true when existsFn returns true for the worktree", () => {
    const init = makeInit("at-aaa", "/wt/at-aaa");
    const nodes = buildInitiativeNodes([init], [], new Set(), (p) => p === "/wt/at-aaa");
    expect(nodes[0]?.worktreeExists).toBe(true);
  });

  it("worktreeExists is false when existsFn returns false", () => {
    const init = makeInit("at-bbb", "/wt/at-bbb");
    const nodes = buildInitiativeNodes([init], [], new Set(), () => false);
    expect(nodes[0]?.worktreeExists).toBe(false);
  });

  it("worktreeExists is false for empty worktree path without calling existsFn", () => {
    const init = makeInit("at-ccc", "");
    // existsFn would return true for anything — empty path must short-circuit to false.
    const nodes = buildInitiativeNodes([init], [], new Set(), () => true);
    expect(nodes[0]?.worktreeExists).toBe(false);
  });

  it("defaults worktreeExists to false when no existsFn is supplied", () => {
    const init = makeInit("at-ddd", "/wt/at-ddd");
    const nodes = buildInitiativeNodes([init], [], new Set());
    expect(nodes[0]?.worktreeExists).toBe(false);
  });

  it("closed initiatives appear in nodes (merged) but never enter the inbox", () => {
    const open = makeInit("at-open", "/wt/at-open", "open");
    const closed = makeInit("at-closed", "/wt/at-closed", "closed");
    const nodes = buildInitiativeNodes([open, closed], [], new Set());

    const closedNode = nodes.find((n) => n.initiative.id === "at-closed");
    expect(closedNode).toBeDefined();
    expect(closedNode?.delivery).toBe("merged");
    expect(closedNode?.needsHuman).toBe(false);

    const inbox = buildInbox(nodes);
    expect(inbox.some((i) => i.initiativeId === "at-closed")).toBe(false);
  });
});

// ---- reap flavor (agent-teams-d10b.2) ----------------------------------------
//
// Zombie = merged initiative + worktree gone (!worktreeExists) + alive session.
// deriveNeedsHuman should return "reap"; buildInbox should emit kind="reap" with
// the constant nextAction and the short sessionId when the session id is 8-hex.

describe("buildInbox — reap flavor (agent-teams-d10b.2)", () => {
  function makeClosedInit(id: string, worktree: string): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: ${worktree}`,
      notes: "",
      status: "closed",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-29",
      updated_at: "2026-06-29",
      problem: "",
      repo: "/repo",
      worktree,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: null,
      epic: null,
    };
  }

  function makeAliveSession(worktree: string, status: "idle" | "busy" | "waiting", shortId: string): SessionState {
    return {
      id: shortId,
      sessionId: `${shortId}-0000-0000-0000-000000000000`,
      kind: "background",
      cwd: worktree,
      startedAt: 0,
      status,
    };
  }

  it("merged + !worktreeExists + alive session -> needsHuman='reap'", () => {
    const init = makeClosedInit("reap-1", "/wt/reap-1");
    const sess = makeAliveSession("/wt/reap-1", "idle", "ab12cd34");
    // existsFn returns false (worktree gone)
    const nodes = buildInitiativeNodes([init], [sess], new Set(), () => false);
    expect(nodes[0]?.needsHuman).toBe("reap");
  });

  it("merged + !worktreeExists + alive session -> buildInbox emits kind='reap' with sessionId", () => {
    const init = makeClosedInit("reap-2", "/wt/reap-2");
    const sess = makeAliveSession("/wt/reap-2", "idle", "ab12cd34");
    const nodes = buildInitiativeNodes([init], [sess], new Set(), () => false);
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(1);
    expect(inbox[0]?.kind).toBe("reap");
    expect(inbox[0]?.nextAction).toBe("Session still running after teardown — stop it to reap it.");
    expect(inbox[0]?.sessionId).toBe("ab12cd34");
  });

  it("merged + worktreeExists TRUE + alive session -> needsHuman=false (not a zombie)", () => {
    const init = makeClosedInit("reap-3", "/wt/reap-3");
    const sess = makeAliveSession("/wt/reap-3", "idle", "ab12cd34");
    // existsFn returns true (worktree still present)
    const nodes = buildInitiativeNodes([init], [sess], new Set(), () => true);
    expect(nodes[0]?.needsHuman).toBe(false);
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(0);
  });

  it("merged + !worktreeExists + NO matched session (signal='none') -> needsHuman=false", () => {
    const init = makeClosedInit("reap-4", "/wt/reap-4");
    // No session passed — nothing to reap
    const nodes = buildInitiativeNodes([init], [], new Set(), () => false);
    expect(nodes[0]?.needsHuman).toBe(false);
    const inbox = buildInbox(nodes);
    expect(inbox).toHaveLength(0);
  });
});

// ---- check flavor (agent-teams-ja9c) -----------------------------------------
//
// A session reporting waiting/blocked with NO explicit gate must produce kind="check"
// (soft tier), NOT "waiting". A real gate:question must still produce "waiting"
// even when the session is also blocked.

describe("buildInbox — check flavor (agent-teams-ja9c)", () => {
  function makeInit(id: string): ParsedInitiative {
    return {
      id,
      title: `Initiative ${id}`,
      description: `worktree: /wt/${id}`,
      notes: "",
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-26T00:00:00Z",
      updated_at: "2026-06-26T10:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: `/wt/${id}`,
      branch: id,
      team: `t-${id}`,
      mode: "bg",
      goal: "",
      prUrl: null,
      epic: null,
    };
  }

  function makeSession(id: string, status: string, state?: string): SessionState {
    return {
      sessionId: `sess-${id}`,
      kind: "background",
      cwd: `/wt/${id}`,
      startedAt: 0,
      status: status as "idle" | "busy" | "waiting",
      state: state as "working" | "blocked" | "done" | "stopped" | undefined,
    };
  }

  it("no-gate + state='blocked' → InboxItem.kind === 'check'", () => {
    const init = makeInit("chk-blocked");
    const sess = makeSession("chk-blocked", "idle", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("check");
  });

  it("no-gate + status='waiting' → InboxItem.kind === 'check'", () => {
    const init = makeInit("chk-waiting");
    const sess = makeSession("chk-waiting", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("check");
  });

  it("check item nextAction is the constant fallback string", () => {
    const init = makeInit("chk-action");
    const sess = makeSession("chk-action", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.nextAction).toBe("Look at the session for more info.");
  });

  it("gate:question + session also blocked → kind='waiting' (gate wins; gate checked first)", () => {
    const init: ParsedInitiative = { ...makeInit("chk-q"), labels: ["gate:question", "human"] };
    const sess = makeSession("chk-q", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("waiting");
  });

  it("gate:question without blocked session → kind='waiting' (authoritative declared ask)", () => {
    const init: ParsedInitiative = { ...makeInit("chk-q2"), labels: ["gate:question"] };
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("waiting");
  });
});

// ---- extractLatestAsk: edge cases (agent-teams-oc3p) -------------------------
//
// Core-path tests (basic parsing, last-valid-wins for decision, unclosed blocks)
// are in the earlier extractLatestAsk describe block.  This block covers the
// non-happy paths specific to recommendation/alternative handling.

describe("extractLatestAsk — recommendation/alternative edge cases (oc3p)", () => {
  // 1. decision: key present but value blank → entire block is invalid (returns null).
  //    Distinct from the existing "no decision key" test: here the key exists, value
  //    is the empty string after trim.
  it("block with blank decision: value (key present, value empty) → null (block invalid)", () => {
    const notes = [
      "<<<ateam-ask",
      "decision:   ",
      "recommendation: Do the thing.",
      ">>>",
    ].join("\n");
    expect(extractLatestAsk(notes)).toBeNull();
  });

  // 2. recommendation: key present but value blank → recommendation == "" (not undefined).
  it("blank recommendation: line (key present, value empty after trim) → recommendation == ''", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Should we proceed?",
      "recommendation:  ",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.recommendation).toBe("");
  });

  // 3. recommendation present, alternative ABSENT → alternative == "".
  it("recommendation present, alternative absent → alternative is empty string", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Deploy?",
      "recommendation: Yes — metrics look good.",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.recommendation).toBe("Yes — metrics look good.");
    expect(result?.alternative).toBe("");
  });

  // 4. alternative present, recommendation ABSENT → recommendation == "".
  it("alternative present, recommendation absent → recommendation is empty string", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Deploy?",
      "alternative: Wait 24h and rerun perf suite.",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.recommendation).toBe("");
    expect(result?.alternative).toBe("Wait 24h and rerun perf suite.");
  });

  // 5. Multiple blocks: last-valid-wins applies to recommendation too.
  //    First block has a recommendation; second block has valid decision but no recommendation.
  //    The SECOND block wins — its empty recommendation replaces the first block's.
  it("last block wins: second valid block has no recommendation → recommendation == ''", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: First question.",
      "recommendation: First rec.",
      ">>>",
      "some prose between blocks",
      "<<<ateam-ask",
      "decision: Second question.",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.decision).toBe("Second question.");
    expect(result?.recommendation).toBe("");
  });

  // 6. Whitespace trimming: extra leading/trailing spaces in recommendation value are stripped.
  it("trims whitespace from recommendation value", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Do it?",
      "recommendation:   Lots of leading spaces.   ",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.recommendation).toBe("Lots of leading spaces.");
  });

  // 7. recommendation value containing a "decision:"-like substring must NOT
  //    cross-contaminate the parsed decision field.
  it("embedded 'decision:' substring in recommendation value does not cross-contaminate", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Override or not?",
      "recommendation: The decision: to act was already made.",
      ">>>",
    ].join("\n");
    const result = extractLatestAsk(notes);
    // decision field must not pick up the "decision:" inside the recommendation value.
    expect(result?.decision).toBe("Override or not?");
    expect(result?.recommendation).toBe("The decision: to act was already made.");
  });

  // 8. Unclosed block with recommendation present and NO prior closed block → null.
  //    The unclosed block is skipped entirely; there is nothing valid to return.
  it("unclosed block with recommendation only (no prior closed block) → null", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Open question.",
      "recommendation: Open rec.",
      // deliberately NO >>>
    ].join("\n");
    expect(extractLatestAsk(notes)).toBeNull();
  });

  // 9. Unclosed block with recommendation comes AFTER a closed valid block.
  //    The unclosed block is skipped; the closed block is returned.
  //    (Mirrors the existing "skips unclosed block at end" test but asserts recommendation
  //    of the closed block is preserved, not contaminated by the unclosed one.)
  it("unclosed block after valid closed block → closed block's recommendation preserved", () => {
    const notes = [
      "<<<ateam-ask",
      "decision: Closed question.",
      "recommendation: Closed rec.",
      ">>>",
      "some prose",
      "<<<ateam-ask",
      "decision: Unclosed — different rec.",
      "recommendation: Unclosed rec — must not appear.",
    ].join("\n");
    const result = extractLatestAsk(notes);
    expect(result?.decision).toBe("Closed question.");
    expect(result?.recommendation).toBe("Closed rec.");
  });
});

// ---- buildInbox: recommendation/alternative 120-char cap (agent-teams-oc3p) ----
//
// buildInbox slices recommendation and alternative to 120 chars before putting
// them in the InboxItem.

describe("buildInbox — recommendation/alternative 120-char cap (oc3p)", () => {
  function makeWaitingInitWithNotes(notes: string): ParsedInitiative {
    return {
      id: "cap-test",
      title: "Cap test",
      description: "worktree: /wt/cap-test",
      notes,
      status: "open",
      priority: "2",
      issue_type: "task",
      owner: "eric",
      created_at: "2026-06-26T00:00:00Z",
      updated_at: "2026-06-26T00:00:00Z",
      problem: "",
      repo: "/repo",
      worktree: "/wt/cap-test",
      branch: "cap-test",
      team: "t-cap-test",
      mode: "bg",
      goal: "",
      prUrl: null,
      labels: ["human"],
      epic: null,
    };
  }

  it("recommendation longer than 120 chars is capped at 120", () => {
    const longRec = "R".repeat(150);
    const notes = [
      "<<<ateam-ask",
      "decision: Should we?",
      `recommendation: ${longRec}`,
      ">>>",
    ].join("\n");
    const init = makeWaitingInitWithNotes(notes);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.recommendation).toHaveLength(120);
    expect(inbox[0]?.recommendation).toBe(longRec.slice(0, 120));
  });

  it("alternative longer than 120 chars is capped at 120", () => {
    const longAlt = "A".repeat(200);
    const notes = [
      "<<<ateam-ask",
      "decision: Deploy?",
      `alternative: ${longAlt}`,
      ">>>",
    ].join("\n");
    const init = makeWaitingInitWithNotes(notes);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.alternative).toHaveLength(120);
    expect(inbox[0]?.alternative).toBe(longAlt.slice(0, 120));
  });

  it("values exactly 120 chars are not truncated", () => {
    const exactly120 = "X".repeat(120);
    const notes = [
      "<<<ateam-ask",
      "decision: Go?",
      `recommendation: ${exactly120}`,
      ">>>",
    ].join("\n");
    const init = makeWaitingInitWithNotes(notes);
    const nodes = buildInitiativeNodes([init], [], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.recommendation).toHaveLength(120);
    expect(inbox[0]?.recommendation).toBe(exactly120);
  });
});
