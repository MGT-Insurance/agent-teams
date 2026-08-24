export type PipelineStage = "investigating" | "building" | "in-review" | "ready-to-land" | "done";
export type QueueGroup = "needs-you" | "agents-working" | "waiting" | "landed";

export interface InitiativeScenario {
  id: string;
  title: string;
  purpose: string;
}

export interface PullRequestScenario {
  number: number;
  repository: string;
  status: "open" | "merged";
  href: string;
}

export interface WorkScenario {
  id: string;
  initiativeId: string;
  title: string;
  summary: string;
  pipelineStage: PipelineStage;
  queueGroup: QueueGroup;
  needsYou: boolean;
  pullRequests: PullRequestScenario[];
  owner: string;
  agent: {
    name: string | null;
    state: string;
  };
  nextAction: {
    text: string;
    responsible: string;
  } | null;
  review: string;
  checks: string;
  blocker: {
    reason: string;
    unblocker: string;
  } | null;
  progress: {
    complete: number;
    total: number;
  };
  timeline: string[];
  implementationIds: string[];
}

export const initiatives: InitiativeScenario[] = [
  {
    id: "dashboard-refresh",
    title: "Dashboard initiative refresh",
    purpose: "Find a work-centered dashboard direction before committing to a production data model.",
  },
  {
    id: "session-recovery",
    title: "Reliable session recovery",
    purpose: "Keep a person's session identity stable through interruption and recovery.",
  },
  {
    id: "release-parity",
    title: "Plugin release parity",
    purpose: "Ship matching runtime capabilities as one dependable release.",
  },
];

export const workScenarios: WorkScenario[] = [
  {
    id: "prototype-lab",
    initiativeId: "dashboard-refresh",
    title: "Prototype interaction lab",
    summary: "Compare three information architectures using the same realistic delivery state.",
    pipelineStage: "in-review",
    queueGroup: "needs-you",
    needsYou: true,
    pullRequests: [
      {
        number: 180,
        repository: "agent-teams/agent-teams",
        status: "open",
        href: "https://github.com/agent-teams/agent-teams/pull/180",
      },
    ],
    owner: "Eric",
    agent: { name: "Rowan", state: "awaiting review" },
    nextAction: {
      text: "Choose a direction after comparing all three concepts",
      responsible: "Eric",
    },
    review: "Needs your review",
    checks: "Concept comparison ready",
    blocker: null,
    progress: { complete: 3, total: 5 },
    timeline: ["Interaction prototype opened as PR #180", "Concept comparison prepared for Eric"],
    implementationIds: ["agent-teams-tuy9.19"],
  },
  {
    id: "responsive-hardening",
    initiativeId: "dashboard-refresh",
    title: "Responsive hardening",
    summary: "Settle the small-screen navigation pattern and harden it across both dashboard shells.",
    pipelineStage: "building",
    queueGroup: "agents-working",
    needsYou: false,
    pullRequests: [],
    owner: "Eric",
    agent: { name: "Mira", state: "building mobile layout" },
    nextAction: {
      text: "Finish the responsive layout and open the first PR",
      responsible: "Mira",
    },
    review: "Not requested — no PR yet",
    checks: "Component checks running during the build",
    blocker: null,
    progress: { complete: 2, total: 4 },
    timeline: ["Mobile constraints mapped", "Responsive shell implementation started"],
    implementationIds: ["agent-teams-tuy9.21", "agent-teams-tuy9.22"],
  },
  {
    id: "reconnect-regression",
    initiativeId: "session-recovery",
    title: "Investigate reconnect regression",
    summary: "Reproduce an intermittent reconnect failure before deciding whether a code change is warranted.",
    pipelineStage: "investigating",
    queueGroup: "agents-working",
    needsYou: false,
    pullRequests: [],
    owner: "Eric",
    agent: { name: "Ada", state: "reproducing disconnect" },
    nextAction: {
      text: "Isolate the failing reconnect sequence",
      responsible: "Ada",
    },
    review: "Not requested — investigation may conclude without a PR",
    checks: "Reproduction matrix in progress",
    blocker: {
      reason: "The failure is intermittent across recovery modes",
      unblocker: "Ada",
    },
    progress: { complete: 1, total: 3 },
    timeline: ["Regression reported", "Recovery modes narrowed to two candidates"],
    implementationIds: ["agent-teams-tuy9.26"],
  },
  {
    id: "session-identity",
    initiativeId: "session-recovery",
    title: "Preserve session identity",
    summary: "Carry the same session identity across a stopped process and the next recovery attempt.",
    pipelineStage: "ready-to-land",
    queueGroup: "waiting",
    needsYou: false,
    pullRequests: [
      {
        number: 176,
        repository: "agent-teams/agent-teams",
        status: "open",
        href: "https://github.com/agent-teams/agent-teams/pull/176",
      },
      {
        number: 179,
        repository: "agent-teams/dashboard",
        status: "open",
        href: "https://github.com/agent-teams/dashboard/pull/179",
      },
    ],
    owner: "Eric",
    agent: { name: "Sol", state: "finished" },
    nextAction: {
      text: "Wait for maintainer review",
      responsible: "Maintainer",
    },
    review: "External review pending",
    checks: "Automated checks passed",
    blocker: null,
    progress: { complete: 4, total: 5 },
    timeline: ["Implementation finished", "Automated checks passed", "Maintainer review requested"],
    implementationIds: ["agent-teams-tuy9.23", "agent-teams-tuy9.24"],
  },
  {
    id: "matching-runtimes",
    initiativeId: "release-parity",
    title: "Publish matching runtimes",
    summary: "Release the Claude and Codex runtimes at matching capability and version levels.",
    pipelineStage: "done",
    queueGroup: "landed",
    needsYou: false,
    pullRequests: [
      {
        number: 174,
        repository: "agent-teams/agent-teams",
        status: "merged",
        href: "https://github.com/agent-teams/agent-teams/pull/174",
      },
    ],
    owner: "Eric",
    agent: { name: null, state: "finished" },
    nextAction: null,
    review: "No open review",
    checks: "Release checks passed",
    blocker: null,
    progress: { complete: 5, total: 5 },
    timeline: ["Runtime parity verified", "PR #174 merged", "Release concluded"],
    implementationIds: ["agent-teams-tuy9.25"],
  },
];

export function initiativeFor(work: WorkScenario): InitiativeScenario {
  const initiative = initiatives.find((item) => item.id === work.initiativeId);
  if (!initiative) throw new Error(`Missing initiative fixture for ${work.initiativeId}`);
  return initiative;
}

export function workForInitiative(initiativeId: string): WorkScenario[] {
  return workScenarios.filter((work) => work.initiativeId === initiativeId);
}
