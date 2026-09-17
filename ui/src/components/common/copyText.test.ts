import { afterEach, describe, expect, it, vi } from "vitest";
import { copyText } from "./copyText";

function secureContext(value: boolean) {
  Object.defineProperty(window, "isSecureContext", {
    value,
    configurable: true,
  });
}

function clipboard(value: { writeText: () => Promise<void> } | undefined) {
  Object.defineProperty(navigator, "clipboard", { value, configurable: true });
}

afterEach(() => {
  vi.restoreAllMocks();
  clipboard(undefined);
});

describe("copyText", () => {
  it("falls back to execCommand when there is no clipboard API", async () => {
    secureContext(false);
    clipboard(undefined);
    const exec = vi.fn().mockReturnValue(true);
    document.execCommand = exec;

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(exec).toHaveBeenCalledWith("copy");
    // The textarea it borrows is not left behind.
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("reports failure rather than claiming a copy that did not happen", async () => {
    secureContext(false);
    clipboard(undefined);
    document.execCommand = vi.fn().mockReturnValue(false);

    await expect(copyText("a-token")).resolves.toBe(false);
  });

  it("falls back when the clipboard API rejects", async () => {
    secureContext(true);
    clipboard({ writeText: vi.fn().mockRejectedValue(new Error("denied")) });
    const exec = vi.fn().mockReturnValue(true);
    document.execCommand = exec;

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(exec).toHaveBeenCalledWith("copy");
  });

  it("uses the clipboard API in a secure context", async () => {
    secureContext(true);
    const writeText = vi.fn().mockResolvedValue(undefined);
    clipboard({ writeText });
    const exec = vi.fn();
    document.execCommand = exec;

    await expect(copyText("a-token")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("a-token");
    expect(exec).not.toHaveBeenCalled();
  });
});

describe("copyText inside a modal", () => {
  it("puts the textarea in the dialog, because a focus trap owns the selection", async () => {
    secureContext(false);
    clipboard(undefined);
    document.body.innerHTML =
      '<div role="dialog"><button id="copy">Copy</button></div>';
    document.getElementById("copy")?.focus();

    let hostWhenCopied: string | undefined;
    document.execCommand = vi.fn(() => {
      hostWhenCopied =
        document.querySelector("textarea")?.parentElement?.getAttribute("role") ??
        undefined;
      return true;
    });

    await copyText("a-token");

    expect(hostWhenCopied).toBe("dialog");
  });
});
