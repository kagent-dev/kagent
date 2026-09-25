import { afterEach, describe, expect, it, vi } from "vitest";
import { copyText } from "./copyText";

function clipboard(value: { writeText: () => Promise<void> } | undefined) {
  Object.defineProperty(navigator, "clipboard", { value, configurable: true });
}

/** `execCommand("copy")`, as a browser that honours it behaves. */
function execCommandThatCopies(setData: (type: string, value: string) => void) {
  return vi.fn(() => {
    const event = new Event("copy") as ClipboardEvent;
    Object.defineProperty(event, "clipboardData", { value: { setData } });
    document.dispatchEvent(event);
    return true;
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  clipboard(undefined);
});

describe("copyText", () => {
  it("uses the clipboard API where there is one", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    clipboard({ writeText });
    const exec = vi.fn();
    document.execCommand = exec;

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("a-token");
    expect(exec).not.toHaveBeenCalled();
  });

  it("answers the copy event when there is no clipboard API", async () => {
    clipboard(undefined);
    const setData = vi.fn();
    document.execCommand = execCommandThatCopies(setData);

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(setData).toHaveBeenCalledWith("text/plain", "a-token");
  });

  it("falls back when the clipboard API rejects", async () => {
    clipboard({ writeText: vi.fn().mockRejectedValue(new Error("denied")) });
    const setData = vi.fn();
    document.execCommand = execCommandThatCopies(setData);

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(setData).toHaveBeenCalledWith("text/plain", "a-token");
  });

  it("reports failure when the copy event never fires, whatever execCommand says", async () => {
    clipboard(undefined);
    // The browser's own answer when a copy is refused: true, and nothing copied.
    document.execCommand = vi.fn().mockReturnValue(true);

    await expect(copyText("a-token")).resolves.toBe(false);
  });

  it("leaves no copy listener behind", async () => {
    clipboard(undefined);
    document.execCommand = execCommandThatCopies(vi.fn());
    await copyText("a-token");

    const stray = vi.fn();
    const event = new Event("copy") as ClipboardEvent;
    Object.defineProperty(event, "clipboardData", { value: { setData: stray } });
    document.dispatchEvent(event);

    expect(stray).not.toHaveBeenCalled();
  });
});
