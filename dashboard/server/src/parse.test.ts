// Unit tests for the parser functions.
// Fixtures are captured from real CLI output (ateam list-json + claude agents --json --all).

import { describe, it, expect } from "vitest";
import {
  extractPrUrl,
  parseInitiative,
  parseAteamListJson,
  parseClaudeAgents,
  buildInitiativeNodes,
  buildInbox,
  deriveActivity,
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

  it("delivered when PR URL + session state=done", () => {
    const init = { ...baseInitiative, prUrl: "https://github.com/o/r/pull/1" };
    expect(deriveActivity(init, idleSession, false)).toBe("delivered");
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

  it("idle when no session and status is open", () => {
    expect(deriveActivity(baseInitiative, null, false)).toBe("idle");
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
});
