import { ThemeProvider } from "@emotion/react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { ChatComposer } from "./ChatComposer";

function composer(send: (text: string) => Promise<boolean>, isStreaming = false) {
  render(<ThemeProvider theme={themeFor("dark")}><ChatComposer send={send} isStreaming={isStreaming} /></ThemeProvider>);
  return screen.getByTestId("chat-input");
}

describe("ChatComposer admission", () => {
  it("preserves a refused draft without resubmitting it", async () => {
    const send = vi.fn(async () => false);
    const input = composer(send);
    fireEvent.change(input, { target: { value: "My prompt" } });
    fireEvent.click(screen.getByTestId("chat-send"));
    await waitFor(() => expect(input).toHaveValue("My prompt"));
    expect(send).toHaveBeenCalledTimes(1);
  });

  it("preserves text typed while an earlier draft is being refused", async () => {
    let release!: (accepted: boolean) => void;
    const input = composer(() => new Promise((resolve) => { release = resolve; }));
    fireEvent.change(input, { target: { value: "Original" } });
    fireEvent.click(screen.getByTestId("chat-send"));
    fireEvent.change(input, { target: { value: "New draft" } });
    await act(async () => { release(false); });
    expect(input).toHaveValue("Original\n\nNew draft");
  });

  it("blocks both the send button and Enter while observing another turn", () => {
    const send = vi.fn(async () => true);
    const input = composer(send, true);
    fireEvent.change(input, { target: { value: "Next prompt" } });
    expect(screen.getByTestId("chat-send")).toBeDisabled();
    fireEvent.keyDown(input, { key: "Enter", code: "Enter", keyCode: 13 });
    expect(send).not.toHaveBeenCalled();
  });
});
