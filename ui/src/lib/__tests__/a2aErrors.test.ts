import { formatA2AClientError } from "@/lib/a2aErrors";

describe("formatA2AClientError", () => {
  it("maps no free workers to a user-facing message", () => {
    expect(formatA2AClientError("rpc error: code = FailedPrecondition desc = no free workers available")).toContain(
      "All substrate workers are busy"
    );
  });

  it("returns trimmed raw text for other errors", () => {
    expect(formatA2AClientError("  boom  ")).toBe("boom");
  });
});
