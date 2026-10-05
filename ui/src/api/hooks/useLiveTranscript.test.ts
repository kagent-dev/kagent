import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useLiveTranscript } from "./useLiveTranscript";

afterEach(() => vi.useRealTimers());

it("finishes a slow poll before starting another", async () => {
  vi.useFakeTimers();
  let release!: () => void;
  const refresh = vi.fn(() => new Promise<void>((resolve) => { release = resolve; }));
  renderHook(() => useLiveTranscript(refresh, { enabled: true, isBusy: false }));
  await act(async () => { await vi.advanceTimersByTimeAsync(12_000); });
  expect(refresh).toHaveBeenCalledTimes(1);
  await act(async () => { release(); });
  await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
  expect(refresh).toHaveBeenCalledTimes(2);
  await act(async () => { release(); });
});
