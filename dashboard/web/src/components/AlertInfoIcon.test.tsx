import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import type { Alert } from "@agent-teams/shared";
import { AlertInfoIcon } from "./AlertInfoIcon.js";

const alert: Alert = {
  level: "urgent",
  reason: "Multiple sessions matched this worktree.",
  action: "Stop the extras.",
};

describe("AlertInfoIcon", () => {
  it("always renders the outer slot, but no icon/popover when alert is null", () => {
    const { container } = render(<AlertInfoIcon alert={null} />);
    const slot = container.querySelector(".alert-info");
    expect(slot).not.toBeNull();
    expect(slot?.getAttribute("data-tier")).toBeNull();
    expect(container.querySelector(".alert-info__icon")).toBeNull();
    expect(container.querySelector(".alert-info__pop")).toBeNull();
  });

  it("renders the icon + popover with the alert's why/do text and tier when alert is set", () => {
    const { container, getByRole } = render(<AlertInfoIcon alert={alert} />);
    expect(container.querySelector(".alert-info")?.getAttribute("data-tier")).toBe("urgent");
    expect(container.querySelector(".alert-info__icon")).not.toBeNull();
    const pop = getByRole("tooltip");
    expect(pop.textContent).toMatch(/Why: Multiple sessions matched this worktree\./);
    expect(pop.textContent).toMatch(/Do: Stop the extras\./);
  });

  it("stops click/keydown propagation so activating the icon can't fire a row's navigate handler", () => {
    const rowClick = vi.fn();
    const { container } = render(
      <div onClick={rowClick}>
        <AlertInfoIcon alert={alert} />
      </div>
    );
    const icon = container.querySelector(".alert-info__icon") as Element;
    fireEvent.click(icon);
    fireEvent.keyDown(icon, { key: "Enter" });
    expect(rowClick).not.toHaveBeenCalled();
  });
});
