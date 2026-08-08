import { getPlugins } from "@/app/actions/plugins";
import { fetchApi } from "@/app/actions/utils";

jest.mock("@/app/actions/utils", () => ({
  fetchApi: jest.fn(),
  createErrorResponse: jest.fn((err: unknown, message: string) => ({
    error: true,
    message,
  })),
}));

jest.mock("@/lib/utils", () => ({ getBackendRoot: jest.fn(() => "http://backend") }));
jest.mock("@/lib/auth", () => ({ getAuthHeadersFromContext: jest.fn(async () => ({})) }));

const mockedFetchApi = fetchApi as jest.Mock;

const plugin = {
  name: "kanban",
  pathPrefix: "kanban",
  displayName: "Kanban",
  icon: "kanban",
  section: "Plugins",
};

describe("getPlugins", () => {
  beforeEach(() => jest.clearAllMocks());

  it("returns plugin data on success", async () => {
    mockedFetchApi.mockResolvedValue({ error: false, data: [plugin], message: "OK" });

    const result = await getPlugins();

    expect(mockedFetchApi).toHaveBeenCalledWith("/plugins");
    expect(result.data).toEqual([plugin]);
    expect(result.message).toBe("OK");
  });

  it("surfaces a backend error response", async () => {
    mockedFetchApi.mockResolvedValue({ error: true, message: "boom" });

    const result = await getPlugins();

    expect(result.data).toBeUndefined();
    expect(result.message).toBe("boom");
    expect(result.error).toBe("boom");
  });

  it("treats a 404 as an empty plugin list (feature optional)", async () => {
    mockedFetchApi.mockRejectedValue(new Error("Request failed with status 404"));

    const result = await getPlugins();

    expect(result.data).toEqual([]);
    expect(result.message).toBe("No plugins available");
  });

  it("returns an error response for unexpected failures", async () => {
    mockedFetchApi.mockRejectedValue(new Error("network down"));

    const result = await getPlugins();

    expect(result).toEqual({ error: true, message: "Failed to fetch plugins" });
  });
});
