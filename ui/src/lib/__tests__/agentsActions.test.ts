import { getAgents } from "@/app/actions/agents";
import { getAgentGrpcGateway } from "@/lib/grpc/client";

jest.mock("next/cache", () => ({
  revalidatePath: jest.fn(),
}));

jest.mock("@/app/actions/utils", () => ({
  createErrorResponse: jest.fn((error: unknown, defaultMessage: string) => ({
    message: error instanceof Error ? error.message : defaultMessage,
    error: error instanceof Error ? error.message : defaultMessage,
  })),
}));

jest.mock("@/lib/grpc/client", () => ({
  getAgentGrpcGateway: jest.fn(),
}));

const mockGetAgentGrpcGateway = getAgentGrpcGateway as jest.MockedFunction<typeof getAgentGrpcGateway>;

describe("getAgents", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("sorts a successful generated gRPC response", async () => {
    const listAgents = jest.fn().mockResolvedValue([
      { agent: { metadata: { namespace: "team-b", name: "beta" } } },
      { agent: { metadata: { namespace: "team-a", name: "alpha" } } },
    ]);
    mockGetAgentGrpcGateway.mockResolvedValueOnce({ listAgents } as never);

    const result = await getAgents();

    expect(result.error).toBeUndefined();
    expect(result.data?.map((row) => row.agent.metadata.name)).toEqual(["alpha", "beta"]);
    expect(listAgents).toHaveBeenCalledWith(undefined);
  });
});
