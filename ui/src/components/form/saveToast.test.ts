import toast from "react-hot-toast";
import { afterEach, describe, expect, it, vi } from "vitest";
import { saveMessage, toastSaved } from "./saveToast";

vi.mock("react-hot-toast", () => ({ default: { success: vi.fn() } }));

describe("save toast", () => {
  afterEach(() => vi.mocked(toast.success).mockClear());

  it("words a create and an edit the same way", () => {
    expect(saveMessage("created", "model", "gpt-4o")).toBe("Created model gpt-4o");
    expect(saveMessage("saved", "prompt library", "team")).toBe("Saved prompt library team");
  });

  it("shows the message as a success toast", () => {
    toastSaved("created", "harness", "default");
    expect(toast.success).toHaveBeenCalledWith("Created harness default");
  });
});
