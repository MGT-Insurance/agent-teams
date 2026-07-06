import { useNavigate } from "react-router-dom";

// Shared row navigate-to-drill-in wiring (agent-teams-m380.2): both
// Initiatives and Inbox rows navigate to /initiative/:id on click or
// Enter/Space keydown, with identical role/tabIndex/aria-label plumbing.
// Spread the returned object onto the row div — each view keeps its own
// className/data-* attributes untouched.
export function useRowClickNav(initiativeId: string, title: string) {
  const navigate = useNavigate();

  function handleRowClick() {
    navigate(`/initiative/${initiativeId}`);
  }

  return {
    onClick: handleRowClick,
    onKeyDown: (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") handleRowClick();
    },
    role: "button" as const,
    tabIndex: 0,
    "aria-label": `Open initiative: ${title}`,
  };
}
