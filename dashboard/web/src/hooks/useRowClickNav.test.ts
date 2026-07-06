import { describe, it, expect, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { useRowClickNav } from "./useRowClickNav.js";

const mockNavigate = vi.fn();
vi.mock("react-router-dom", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router-dom")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

function renderRowNav(initiativeId: string, title: string) {
  return renderHook(() => useRowClickNav(initiativeId, title), {
    wrapper: MemoryRouter,
  });
}

describe("useRowClickNav", () => {
  it("returns the expected role/tabIndex/aria-label props", () => {
    const { result } = renderRowNav("init-1", "Some Initiative");
    expect(result.current.role).toBe("button");
    expect(result.current.tabIndex).toBe(0);
    expect(result.current["aria-label"]).toBe("Open initiative: Some Initiative");
  });

  it("onClick navigates to the initiative's drill-in route", () => {
    mockNavigate.mockClear();
    const { result } = renderRowNav("init-42", "Deploy canary");
    result.current.onClick();
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-42");
  });

  it("onKeyDown navigates on Enter and Space, but not on other keys", () => {
    mockNavigate.mockClear();
    const { result } = renderRowNav("init-7", "Refactor auth");
    result.current.onKeyDown({ key: "Enter" } as React.KeyboardEvent);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-7");

    mockNavigate.mockClear();
    result.current.onKeyDown({ key: " " } as React.KeyboardEvent);
    expect(mockNavigate).toHaveBeenCalledWith("/initiative/init-7");

    mockNavigate.mockClear();
    result.current.onKeyDown({ key: "Tab" } as React.KeyboardEvent);
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
