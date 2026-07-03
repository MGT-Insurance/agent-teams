import { describe, it, expect, vi, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useAsyncButtonState } from "./useAsyncButtonState.js";

function makeEvent(): React.MouseEvent {
  return { stopPropagation: vi.fn() } as unknown as React.MouseEvent;
}

describe("useAsyncButtonState", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("starts idle", () => {
    const { result } = renderHook(() => useAsyncButtonState());
    expect(result.current.state).toBe("idle");
    expect(result.current.errMsg).toBe("");
  });

  it("stops propagation on trigger", () => {
    const { result } = renderHook(() => useAsyncButtonState());
    const e = makeEvent();
    act(() => {
      result.current.trigger(e, () => new Promise(() => {}));
    });
    expect(e.stopPropagation).toHaveBeenCalledTimes(1);
  });

  it("goes idle -> pending -> ok on success, then auto-resets to idle after okTimeoutMs", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useAsyncButtonState({ okTimeoutMs: 1500 }));
    const action = vi.fn().mockResolvedValue(undefined);

    act(() => {
      result.current.trigger(makeEvent(), action);
    });
    expect(result.current.state).toBe("pending");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.state).toBe("ok");
    expect(action).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(result.current.state).toBe("idle");
  });

  it("goes idle -> pending -> err on failure, captures message, then auto-resets after errTimeoutMs", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useAsyncButtonState({ errTimeoutMs: 3000 }));
    const action = vi.fn().mockRejectedValue(new Error("boom"));

    act(() => {
      result.current.trigger(makeEvent(), action);
    });
    expect(result.current.state).toBe("pending");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.state).toBe("err");
    expect(result.current.errMsg).toBe("boom");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(result.current.state).toBe("idle");
    expect(result.current.errMsg).toBe("");
  });

  it("stringifies non-Error rejections", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useAsyncButtonState());
    const action = vi.fn().mockRejectedValue("plain string failure");

    act(() => {
      result.current.trigger(makeEvent(), action);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.errMsg).toBe("plain string failure");
  });

  it("re-entrant clicks while pending are a no-op: action runs exactly once", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useAsyncButtonState());
    let resolveAction: () => void = () => {};
    const action = vi.fn().mockImplementation(
      () => new Promise<void>((resolve) => { resolveAction = resolve; })
    );

    act(() => {
      result.current.trigger(makeEvent(), action);
    });
    expect(result.current.state).toBe("pending");

    // Second click while still pending.
    act(() => {
      result.current.trigger(makeEvent(), action);
    });
    expect(action).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveAction();
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.state).toBe("ok");
  });

  it("clears any previous error message when a new attempt starts", async () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useAsyncButtonState({ errTimeoutMs: 100000 }));

    await act(async () => {
      result.current.trigger(makeEvent(), () => Promise.reject(new Error("first failure")));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.errMsg).toBe("first failure");

    act(() => {
      // Start a new attempt before the err->idle timeout fires.
      result.current.trigger(makeEvent(), () => new Promise(() => {}));
    });
    expect(result.current.state).toBe("pending");
    expect(result.current.errMsg).toBe("");
  });
});
