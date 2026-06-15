// Unit tests for the parser functions.
// Fixtures are captured from real CLI output (ateam list-json + claude agents --json --all).

import { describe, it, expect } from "vitest";
import {
  extractPrUrl,
  parseInitiative,
  parseAteamListJson,
  parseClaudeAgents,
  buildInitiativeNodes,
  buildOrphanSessions,
  buildInbox,
  deriveActivity,
  deriveDelivery,
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

  it("activity is needs-human when initiative is in humanGatedIds", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set(["at-v4e"]));
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("activity is busy when matched session has status=busy", () => {
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.activity).toBe("busy");
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

  it("needs-human overrides everything", () => {
    expect(deriveActivity(baseInitiative, busySession, true)).toBe("needs-human");
  });

  it("needs-human (review) when PR URL + idle session (PR awaiting review)", () => {
    const init = { ...baseInitiative, prUrl: "https://github.com/o/r/pull/1" };
    // delivery=pr-open, working=false → needs-human:review → deriveActivity returns "needs-human"
    expect(deriveActivity(init, idleSession, false)).toBe("needs-human");
  });

  it("busy when session status is busy", () => {
    expect(deriveActivity(baseInitiative, busySession, false)).toBe("busy");
  });

  it("busy when session has state=working even if status=idle", () => {
    const notDoneSession: SessionState = { ...busySession, status: "idle", state: "working" };
    expect(deriveActivity(baseInitiative, notDoneSession, false)).toBe("busy");
  });

  it("idle when session is not busy and state is not working", () => {
    const quietSession: SessionState = { ...busySession, status: "idle", state: "done" };
    // Use an initiative without a PR URL so the delivered branch doesn't fire.
    const noPrInitiative = { ...baseInitiative, prUrl: null as string | null };
    expect(deriveActivity(noPrInitiative, quietSession, false)).toBe("idle");
  });

  it("done when no session and initiative status is closed", () => {
    const closed = { ...baseInitiative, status: "closed" };
    expect(deriveActivity(closed, null, false)).toBe("done");
  });

  it("needs-human (generic) when no session but PR open and status is open", () => {
    // delivery=pr-open (prUrl present, status=open), signal=none, isHumanGated=false
    // → needsHuman="generic" (graceful degrade) → deriveActivity returns "needs-human"
    expect(deriveActivity(baseInitiative, null, false)).toBe("needs-human");
  });

  it("idle when no session, no PR, status is open", () => {
    const noPr = { ...baseInitiative, prUrl: null as string | null };
    expect(deriveActivity(noPr, null, false)).toBe("idle");
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

  it("waiting item carries the latest notes line as question", () => {
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_V4E)],
      sessions,
      new Set(["at-v4e"]),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-v4e");
    // Latest non-empty notes line of RAW_AT_V4E
    expect(item?.question).toContain("awaiting-merge");
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

  it("includes review items (needsHuman=review) when delivered + session ENDED", () => {
    // at-2jh: prUrl present, matched ENDED session -> signal=ended -> needsHuman="review"
    const nodes = buildInitiativeNodes(
      [parseInitiative(RAW_AT_2JH)],
      [ENDED_SESSION],
      new Set(),
    );
    const inbox = buildInbox(nodes);
    const item = inbox.find((i) => i.initiativeId === "at-2jh");
    expect(item).toBeDefined();
    expect(item?.kind).toBe("review");
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

  it("waiting takes priority over review when both conditions apply (human-gated + pr-open + idle)", () => {
    // at-2jh: prUrl present, no working session, and human-gated -> needsHuman="waiting" (not "review")
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

  it("does not include merged initiatives (needsHuman=false for merged)", () => {
    const done = { ...parseInitiative(RAW_AT_V4E), status: "done" };
    const nodes = buildInitiativeNodes([done], sessions, new Set(["at-v4e"]));
    // merged overrides gate: needsHuman=false
    expect(nodes[0]?.needsHuman).toBe(false);
    const inbox = buildInbox(nodes);
    expect(inbox.find((i) => i.initiativeId === "at-v4e")).toBeUndefined();
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

// ---- deriveNeedsHuman (agent-teams-blo) truth table -------------------------

describe("deriveNeedsHuman", () => {
  it("session WAITING -> 'waiting' (most urgent; active initiative)", () => {
    expect(deriveNeedsHuman("none", "waiting", false)).toBe("waiting");
  });

  it("session WAITING + delivered -> 'waiting' (most urgent; delivered initiative)", () => {
    expect(deriveNeedsHuman("pr-open", "waiting", false)).toBe("waiting");
  });

  it("explicit gate (no session) -> 'waiting' (human gate override)", () => {
    expect(deriveNeedsHuman("none", "none", true)).toBe("waiting");
  });

  it("explicit gate + working session -> 'waiting' (gate wins over working)", () => {
    expect(deriveNeedsHuman("none", "working", true)).toBe("waiting");
  });

  it("explicit gate + delivered -> 'waiting' (gate wins)", () => {
    expect(deriveNeedsHuman("pr-open", "none", true)).toBe("waiting");
  });

  it("session WORKING -> false (refining after delivery, not in inbox)", () => {
    expect(deriveNeedsHuman("pr-open", "working", false)).toBe(false);
  });

  it("none + WORKING -> false (initial work in progress)", () => {
    expect(deriveNeedsHuman("none", "working", false)).toBe(false);
  });

  it("none + ENDED -> false (active + ended = idle/dormant, no PR)", () => {
    expect(deriveNeedsHuman("none", "ended", false)).toBe(false);
  });

  it("none + NONE -> false (active + no session = idle/dormant)", () => {
    expect(deriveNeedsHuman("none", "none", false)).toBe(false);
  });

  it("delivered + ENDED -> 'review' (verify & merge — signal-backed)", () => {
    expect(deriveNeedsHuman("pr-open", "ended", false)).toBe("review");
  });

  it("delivered + NONE -> 'generic' (graceful degrade; no session info)", () => {
    expect(deriveNeedsHuman("pr-open", "none", false)).toBe("generic");
  });

  it("merged -> false (done, nothing needed)", () => {
    expect(deriveNeedsHuman("merged", "none", false)).toBe(false);
  });

  it("merged + gate -> false (closed initiatives never need human)", () => {
    expect(deriveNeedsHuman("merged", "none", true)).toBe(false);
  });

  it("merged + WAITING -> false (done wins over waiting)", () => {
    expect(deriveNeedsHuman("merged", "waiting", false)).toBe(false);
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

  it("needsHuman is review when delivery=pr-open and session ENDED", () => {
    // at-2jh: has prUrl, matched ENDED session -> signal=ended -> needsHuman="review"
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], [ENDED_SESSION], new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
    expect(nodes[0]?.needsHuman).toBe("review");
  });

  it("needsHuman is false when delivery=pr-open and working session present", () => {
    // at-v4e: has prUrl, matched session is busy+working
    const parsed = parseInitiative(RAW_AT_V4E);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("pr-open");
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("needsHuman is waiting when humanGated (regardless of delivery)", () => {
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set(["at-2jh"]));
    expect(nodes[0]?.needsHuman).toBe("waiting");
  });

  it("delivery is merged when initiative status is closed", () => {
    const closed = { ...parseInitiative(RAW_AT_V4E), status: "closed" };
    const nodes = buildInitiativeNodes([closed], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("merged");
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("delivery is none when no prUrl and status open", () => {
    const noPr = { ...parseInitiative(RAW_AT_V4E), prUrl: null as string | null };
    const nodes = buildInitiativeNodes([noPr], sessions, new Set());
    expect(nodes[0]?.delivery).toBe("none");
  });

  it("needsHuman is waiting when session is blocked (state=blocked) — the core blo fix", () => {
    // Blocked session: status=waiting, state=blocked -> signal="waiting" -> needsHuman="waiting"
    const parsed = parseInitiative(RAW_AT_2JH); // has prUrl but that's irrelevant
    const nodes = buildInitiativeNodes([parsed], [BLOCKED_SESSION], new Set());
    // BLOCKED_SESSION.cwd matches RAW_AT_2JH worktree? No — different path.
    // Use a tweaked RAW so the worktree matches BLOCKED_SESSION.cwd.
    const raw: RawInitiative = { ...RAW_AT_2JH, description: RAW_AT_2JH.description.replace(
      "/Users/ericlloyd/.agent-teams-worktrees/specialty-quote-api",
      "/Users/ericlloyd/.agent-teams-worktrees/some-blocked-initiative",
    )};
    const nodes2 = buildInitiativeNodes([parseInitiative(raw)], [BLOCKED_SESSION], new Set());
    expect(nodes2[0]?.needsHuman).toBe("waiting");
    expect(nodes2[0]?.activity).toBe("needs-human");
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

  it("session waiting/blocked -> needsHuman='waiting' (most urgent)", () => {
    const init = makeInit("blocked-1", false);
    const sess = makeSession("/wt/blocked-1", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("waiting");
    expect(nodes[0]?.activity).toBe("needs-human");
  });

  it("session working -> not needs-you (refining if delivered, working if active)", () => {
    const init = makeInit("working-1", true);
    const sess = makeSession("/wt/working-1", "busy", "working");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
  });

  it("delivered + session ended -> needsHuman='review' (verify & merge)", () => {
    const init = makeInit("review-1", true);
    const sess = makeSession("/wt/review-1", "idle", "stopped");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe("review");
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

  it("done initiative -> needsHuman=false regardless of session", () => {
    const init = makeInit("done-1", true, "closed");
    const sess = makeSession("/wt/done-1", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    expect(nodes[0]?.needsHuman).toBe(false);
    expect(nodes[0]?.activity).toBe("done");
  });

  it("inbox: waiting item has kind='waiting'", () => {
    const init = makeInit("w-inbox", false);
    const sess = makeSession("/wt/w-inbox", "waiting", "blocked");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("waiting");
  });

  it("inbox: review item has kind='review' (delivered + ended)", () => {
    const init = makeInit("r-inbox", true);
    const sess = makeSession("/wt/r-inbox", "idle", "stopped");
    const nodes = buildInitiativeNodes([init], [sess], new Set());
    const inbox = buildInbox(nodes);
    expect(inbox[0]?.kind).toBe("review");
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
