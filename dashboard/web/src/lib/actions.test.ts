import { describe, it, expect } from "vitest";
import { selectRowAction, type SelectRowActionInput } from "./actions.js";

function baseInput(over: Partial<SelectRowActionInput> = {}): SelectRowActionInput {
  return {
    initiativeId: "init-1",
    isReap: false,
    isClosed: false,
    worktreeExists: false,
    rawSessionId: undefined,
    attachId: undefined,
    ...over,
  };
}

describe("selectRowAction — branch precedence (CONTRACT D)", () => {
  it("reap-stop: isReap + a raw session id -> stop, wins over everything else", () => {
    const action = selectRowAction(
      baseInput({ isReap: true, rawSessionId: "s1", isClosed: true, attachId: "ab12cd34", worktreeExists: true })
    );
    expect(action).toEqual({ kind: "stop", initiativeId: "init-1", sessionId: "s1" });
  });

  it("closed-stop: isClosed + a raw session id (not reap) -> stop", () => {
    const action = selectRowAction(
      baseInput({ isClosed: true, rawSessionId: "s2", attachId: "ab12cd34", worktreeExists: true })
    );
    expect(action).toEqual({ kind: "stop", initiativeId: "init-1", sessionId: "s2" });
  });

  it("attach: not reap/closed-with-session, but a valid attachId -> attach", () => {
    const action = selectRowAction(baseInput({ attachId: "ab12cd34", worktreeExists: true }));
    expect(action).toEqual({ kind: "attach", initiativeId: "init-1", sessionId: "ab12cd34" });
  });

  it("launch: no reap/closed-stop/attach, worktree exists and not closed -> launch", () => {
    const action = selectRowAction(baseInput({ worktreeExists: true }));
    expect(action).toEqual({ kind: "launch", initiativeId: "init-1" });
  });

  it("none: no session, no attach id, worktree missing -> none", () => {
    const action = selectRowAction(baseInput());
    expect(action).toEqual({ kind: "none" });
  });

  it("isReap true but no rawSessionId falls through past the reap branch", () => {
    const action = selectRowAction(baseInput({ isReap: true, rawSessionId: undefined, worktreeExists: true }));
    expect(action).toEqual({ kind: "launch", initiativeId: "init-1" });
  });

  it("isClosed true but no rawSessionId falls through past the closed-stop branch to none", () => {
    // worktreeExists && !isClosed is false here (isClosed is true) — launch is not reachable either.
    const action = selectRowAction(baseInput({ isClosed: true, worktreeExists: true }));
    expect(action).toEqual({ kind: "none" });
  });

  it("attachId wins over launch when both a valid worktree and an attach id are present", () => {
    const action = selectRowAction(baseInput({ attachId: "ff00aa11", worktreeExists: true }));
    expect(action).toEqual({ kind: "attach", initiativeId: "init-1", sessionId: "ff00aa11" });
  });
});
