import { selectRowAction, type SelectRowActionInput } from "../lib/actions.js";
import { StopButton } from "./StopButton.js";
import { LaunchButton } from "./LaunchButton.js";
import { AttachButton } from "./AttachButton.js";

// Shared row-action render (agent-teams-rybk, CONTRACT E): calls selectRowAction
// and renders the one button (or none) it picks. Consumed by both the
// Initiatives row and the Inbox row. Caller supplies its own layout wrapper.
export function RowActions({ input }: { input: SelectRowActionInput }) {
  const action = selectRowAction(input);
  switch (action.kind) {
    case "stop":
      return <StopButton initiativeId={action.initiativeId} sessionId={action.sessionId} />;
    case "attach":
      return <AttachButton initiativeId={action.initiativeId} sessionId={action.sessionId} />;
    case "launch":
      return <LaunchButton initiativeId={action.initiativeId} />;
    case "none":
      return null;
  }
}
