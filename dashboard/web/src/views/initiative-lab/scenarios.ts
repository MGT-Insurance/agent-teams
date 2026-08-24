export type PipelineStage = "ready" | "building" | "review" | "ship" | "done";
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
    pipelineStage: "review",
    queueGroup: "agents-working",
    pullRequests: [
      {
        number: 180,
        repository: "agent-teams/agent-teams",
        status: "open",
        href: "https://github.com/agent-teams/agent-teams/pull/180",
      },
    ],
    owner: "Eric",
    agent: { name: "Rowan", state: "exploring" },
    nextAction: {
      text: "Choose a direction after comparing all three concepts",
      responsible: "Eric",
    },
    review: "Needs your review",
    checks: "Concept comparison ready",
    blocker: null,
    progress: { complete: 3, total: 5 },
    implementationIds: ["agent-teams-tuy9.19"],
  },
  {
    id: "responsive-hardening",
    initiativeId: "dashboard-refresh",
    title: "Responsive hardening",
    summary: "Settle the small-screen navigation pattern and harden it across both dashboard shells.",
    pipelineStage: "building",
    queueGroup: "needs-you",
    pullRequests: [
      {
        number: 181,
        repository: "agent-teams/agent-teams",
        status: "open",
        href: "https://github.com/agent-teams/agent-teams/pull/181",
      },
      {
        number: 182,
        repository: "agent-teams/dashboard",
        status: "open",
        href: "https://github.com/agent-teams/dashboard/pull/182",
      },
    ],
    owner: "Eric",
    agent: { name: "Mira", state: "paused" },
    nextAction: {
      text: "Resolve the mobile navigation decision",
      responsible: "Eric",
    },
    review: "Review not requested",
    checks: "Checks wait on the design decision",
    blocker: {
      reason: "The mobile navigation direction is unresolved",
      unblocker: "Eric",
    },
    progress: { complete: 1, total: 4 },
    implementationIds: ["agent-teams-tuy9.21", "agent-teams-tuy9.22"],
  },
  {
    id: "session-identity",
    initiativeId: "session-recovery",
    title: "Preserve session identity",
    summary: "Carry the same session identity across a stopped process and the next recovery attempt.",
    pipelineStage: "ship",
    queueGroup: "waiting",
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
    implementationIds: ["agent-teams-tuy9.23", "agent-teams-tuy9.24"],
  },
  {
    id: "matching-runtimes",
    initiativeId: "release-parity",
    title: "Publish matching runtimes",
    summary: "Release the Claude and Codex runtimes at matching capability and version levels.",
    pipelineStage: "done",
    queueGroup: "landed",
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
