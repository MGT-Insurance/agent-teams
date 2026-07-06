// Shared row-action selection (agent-teams-rybk, CONTRACT D). ONE implementation
// of "which button shows" consumed by both the Initiatives table and the Inbox.
// Stays in web/lib (not shared/) — the server cannot import it; InboxItem.isClosed
// is the seam that lets the inbox call it without server-only status logic.
export type RowAction =
  | { kind: "stop"; initiativeId: string; sessionId: string }
  | { kind: "launch"; initiativeId: string }
  | { kind: "attach"; initiativeId: string; sessionId: string }
  | { kind: "none" };

export interface SelectRowActionInput {
  initiativeId: string;
  // initiatives: node.needsHuman === "reap"; inbox: item.kind === "reap".
  isReap: boolean;
  isClosed: boolean;
  // initiatives: node.worktreeExists; inbox: item.onThisMachine (same server existsSync check).
  worktreeExists: boolean;
  // stop id: node.session?.id (Initiatives — truly raw, unvalidated) / item.sessionId (Inbox —
  // NOT equivalently raw; buildInbox already validated it as an 8-hex id before assigning it here).
  rawSessionId: string | undefined;
  // validated 8-hex: sessionAttachId(node.session) / item.sessionId.
  attachId: string | undefined;
}

// Logic VERBATIM from the former initiatives/index.tsx:357-365 — order matters.
export function selectRowAction(input: SelectRowActionInput): RowAction {
  const { initiativeId, isReap, isClosed, worktreeExists, rawSessionId, attachId } = input;
  if (isReap && rawSessionId) return { kind: "stop", initiativeId, sessionId: rawSessionId };
  if (isClosed && rawSessionId) return { kind: "stop", initiativeId, sessionId: rawSessionId };
  if (attachId) return { kind: "attach", initiativeId, sessionId: attachId };
  if (worktreeExists && !isClosed) return { kind: "launch", initiativeId };
  return { kind: "none" };
}
