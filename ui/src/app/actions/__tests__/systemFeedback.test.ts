import { submitNegativeFeedback, submitPositiveFeedback } from "@/app/actions/feedback";
import { listNamespaces } from "@/app/actions/namespaces";
import { getSubstrateStatus } from "@/app/actions/substrate";
import { getFeedbackGrpcGateway, getSystemGrpcGateway } from "@/lib/grpc/client";

jest.mock("@/app/actions/utils", () => ({
  createErrorResponse: jest.fn((error: unknown, defaultMessage: string) => ({
    error: error instanceof Error ? error.message : defaultMessage,
    message: error instanceof Error ? error.message : defaultMessage,
  })),
}));

jest.mock("@/lib/grpc/client", () => ({
  getFeedbackGrpcGateway: jest.fn(),
  getSystemGrpcGateway: jest.fn(),
}));

const listNamespacesGateway = jest.fn();
const getSubstrateStatusGateway = jest.fn();
const submitFeedbackGateway = jest.fn();
const mockedGetSystemGrpcGateway = getSystemGrpcGateway as jest.MockedFunction<typeof getSystemGrpcGateway>;
const mockedGetFeedbackGrpcGateway = getFeedbackGrpcGateway as jest.MockedFunction<typeof getFeedbackGrpcGateway>;

describe("system and feedback server actions", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    listNamespacesGateway.mockResolvedValue([]);
    getSubstrateStatusGateway.mockResolvedValue({
      enabled: false,
      workerPools: [],
      actorTemplates: [],
      actors: [],
      workers: [],
    });
    submitFeedbackGateway.mockResolvedValue(undefined);
    mockedGetSystemGrpcGateway.mockResolvedValue({
      listNamespaces: listNamespacesGateway,
      getSubstrateStatus: getSubstrateStatusGateway,
    } as never);
    mockedGetFeedbackGrpcGateway.mockResolvedValue({
      submitFeedback: submitFeedbackGateway,
    } as never);
  });

  it("lists namespaces through the System gRPC gateway", async () => {
    listNamespacesGateway.mockResolvedValueOnce([{ name: "team", status: "Active" }]);

    await expect(listNamespaces()).resolves.toEqual({
      message: "Namespaces fetched successfully",
      data: [{ name: "team", status: "Active" }],
    });
    expect(listNamespacesGateway).toHaveBeenCalledWith();
  });

  it("trims the optional namespace and loads substrate status through gRPC", async () => {
    const status = {
      enabled: true,
      workerPools: [{ namespace: "team", name: "pool", replicas: 2, ateomImage: "ateom:test" }],
      actorTemplates: [],
      actors: [],
      workers: [],
    };
    getSubstrateStatusGateway.mockResolvedValueOnce(status);

    await expect(getSubstrateStatus("  team  ")).resolves.toEqual({
      message: "Successfully listed substrate status",
      data: status,
    });
    expect(getSubstrateStatusGateway).toHaveBeenCalledWith("team");
  });

  it("returns compatibility error envelopes for System gRPC failures", async () => {
    listNamespacesGateway.mockRejectedValueOnce(new Error("backend unavailable"));
    getSubstrateStatusGateway.mockRejectedValueOnce(new Error("inventory unavailable"));

    await expect(listNamespaces()).resolves.toEqual({
      error: "backend unavailable",
      message: "backend unavailable",
    });
    await expect(getSubstrateStatus()).resolves.toEqual({
      error: "inventory unavailable",
      message: "inventory unavailable",
    });
  });

  it("submits positive and negative feedback through the Feedback gRPC gateway", async () => {
    await expect(submitPositiveFeedback(42, "helpful")).resolves.toEqual({
      error: false,
      data: {},
      message: "Feedback submitted successfully",
    });
    await expect(submitNegativeFeedback(84, "incorrect", "factual")).resolves.toEqual({
      error: false,
      data: {},
      message: "Feedback submitted successfully",
    });

    expect(submitFeedbackGateway).toHaveBeenNthCalledWith(1, {
      isPositive: true,
      feedbackText: "helpful",
      messageId: 42,
    });
    expect(submitFeedbackGateway).toHaveBeenNthCalledWith(2, {
      isPositive: false,
      feedbackText: "incorrect",
      issueType: "factual",
      messageId: 84,
    });
  });

  it("rejects Feedback gRPC failures", async () => {
    submitFeedbackGateway.mockRejectedValueOnce(new Error("feedback unavailable"));

    await expect(submitPositiveFeedback(42, "helpful")).rejects.toThrow("feedback unavailable");
  });
});
