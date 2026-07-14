import { getAgents } from "@/app/actions/agents";
import { fetchApi } from "@/app/actions/utils";

jest.mock("next/cache", () => ({
  revalidatePath: jest.fn(),
}));

jest.mock("@/app/actions/utils", () => ({
  fetchApi: jest.fn(),
  createErrorResponse: jest.fn((error: unknown, defaultMessage: string) => ({
    message: error instanceof Error ? error.message : defaultMessage,
    error: error instanceof Error ? error.message : defaultMessage,
  })),
}));

const mockFetchApi = fetchApi as jest.MockedFunction<typeof fetchApi>;

describe("getAgents", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("normalizes a successful response without data to an empty list", async () => {
    mockFetchApi.mockResolvedValueOnce({ message: "Successfully fetched agents" });

    const result = await getAgents();

    expect(result.error).toBeUndefined();
    expect(result.data).toEqual([]);
  });
});
