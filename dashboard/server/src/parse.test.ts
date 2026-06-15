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

  it("needs-human (review) when no session but PR open and status is open", () => {
    // delivery=pr-open (prUrl present, status=open), working=false, isHumanGated=false
    // → needsHuman="review" → deriveActivity returns "needs-human"
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

// ---- buildInbox -------------------------------------------------------------

describe("buildInbox", () => {
  const initiatives = [
    parseInitiative(RAW_AT_V4E),
    parseInitiative(RAW_AT_2JH),
  ];

  it("includes human-gated initiatives", () => {
    const inbox = buildInbox(initiatives, new Set(["at-v4e"]));
    const item = inbox.find((i) => i.initiativeId === "at-v4e");
    expect(item).toBeDefined();
    expect(item?.kind).toBe("human-gate");
  });

  it("includes pr-awaiting-merge initiatives when PR URL present and status is open", () => {
    const inbox = buildInbox(initiatives, new Set());
    const prItem = inbox.find((i) => i.kind === "pr-awaiting-merge");
    expect(prItem).toBeDefined();
    expect(prItem?.prUrl).toBe("https://github.com/MGT-Insurance/midgard/pull/3551");
  });

  it("does not duplicate: human-gate takes priority over pr-awaiting-merge", () => {
    const inbox = buildInbox(initiatives, new Set(["at-2jh"]));
    const items = inbox.filter((i) => i.initiativeId === "at-2jh");
    expect(items).toHaveLength(1);
    expect(items[0]?.kind).toBe("human-gate");
  });

  it("returns empty array when no human gates and no PRs", () => {
    const nopr = initiatives.map((i) => ({ ...i, prUrl: null as string | null }));
    const inbox = buildInbox(nopr, new Set());
    expect(inbox).toHaveLength(0);
  });

  it("does not include closed initiatives in pr-awaiting-merge", () => {
    const closed = initiatives.map((i) => ({ ...i, status: "closed" }));
    const inbox = buildInbox(closed, new Set());
    expect(inbox).toHaveLength(0);
  });

  it("does not include closed human-gated initiatives", () => {
    const closed = initiatives.map((i) => ({ ...i, status: "closed" }));
    const inbox = buildInbox(closed, new Set(["at-v4e"]));
    expect(inbox.find((i) => i.initiativeId === "at-v4e")).toBeUndefined();
  });

  it("does not include done human-gated initiatives", () => {
    const done = initiatives.map((i) => ({ ...i, status: "done" }));
    const inbox = buildInbox(done, new Set(["at-v4e"]));
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

// ---- deriveNeedsHuman (agent-teams-3e6) truth table -------------------------

describe("deriveNeedsHuman", () => {
  it("none + working → false (initial work in progress)", () => {
    expect(deriveNeedsHuman("none", true, false)).toBe(false);
  });

  it("none + idle + no gate → false", () => {
    expect(deriveNeedsHuman("none", false, false)).toBe(false);
  });

  it("none + idle + gate → answer (initial work, blocked on gate)", () => {
    expect(deriveNeedsHuman("none", false, true)).toBe("answer");
  });

  it("none + working + gate → answer (gate takes priority over working)", () => {
    expect(deriveNeedsHuman("none", true, true)).toBe("answer");
  });

  it("pr-open + working → false (refining after delivery, Eric waits)", () => {
    expect(deriveNeedsHuman("pr-open", true, false)).toBe(false);
  });

  it("pr-open + idle → review (PR awaiting Eric review — the key structural fix)", () => {
    expect(deriveNeedsHuman("pr-open", false, false)).toBe("review");
  });

  it("pr-open + idle + gate → answer (explicit gate takes priority over review)", () => {
    expect(deriveNeedsHuman("pr-open", false, true)).toBe("answer");
  });

  it("merged → false (done, nothing needed)", () => {
    expect(deriveNeedsHuman("merged", false, false)).toBe(false);
  });

  it("merged + gate → false (closed initiatives never need human)", () => {
    expect(deriveNeedsHuman("merged", false, true)).toBe(false);
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

  it("needsHuman is review when delivery=pr-open and no working session", () => {
    // at-2jh: has prUrl, status=open, no matched session (worktree not in REAL_SESSIONS_JSON)
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set());
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

  it("needsHuman is answer when humanGated (regardless of delivery)", () => {
    const parsed = parseInitiative(RAW_AT_2JH);
    const nodes = buildInitiativeNodes([parsed], sessions, new Set(["at-2jh"]));
    expect(nodes[0]?.needsHuman).toBe("answer");
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
