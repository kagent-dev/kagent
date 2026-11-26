import toast from "react-hot-toast";
import { describe, expect, it, vi } from "vitest";
import { deleteMessage, toastDeleted } from "./deleteToast";

vi.mock("react-hot-toast", () => ({ default: { success: vi.fn() } }));

describe("toastDeleted", () => {
  it("words the message as the kind and name", () => {
    expect(deleteMessage("model", "gpt-4o")).toBe("Deleted model gpt-4o");
  });

  it("shows it as a success toast", () => {
    toastDeleted("model", "gpt-4o");
    expect(toast.success).toHaveBeenCalledWith("Deleted model gpt-4o");
  });
});
