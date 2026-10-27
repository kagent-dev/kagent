import PromptsPage from "@/app/prompts/page";
import { getUiRuntimeConfig } from "@/app/actions/config";
import { listPromptTemplates } from "@/app/actions/promptTemplates";

jest.mock("@/app/actions/config", () => ({
  getUiRuntimeConfig: jest.fn(),
}));

jest.mock("@/app/actions/promptTemplates", () => ({
  listPromptTemplates: jest.fn(),
}));

const mockGetUiRuntimeConfig = getUiRuntimeConfig as jest.MockedFunction<
  typeof getUiRuntimeConfig
>;
const mockListPromptTemplates = listPromptTemplates as jest.MockedFunction<
  typeof listPromptTemplates
>;

describe("PromptsPage namespace default", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGetUiRuntimeConfig.mockResolvedValue({
      streamTimeoutMs: 1_800_000,
      defaultNamespace: "dev-kagent",
    });
    mockListPromptTemplates.mockResolvedValue({
      message: "Successfully listed prompt template ConfigMaps",
      data: [],
    });
  });

  it("uses the install namespace when the URL has no namespace", async () => {
    await PromptsPage({ searchParams: Promise.resolve({}) });

    expect(mockListPromptTemplates).toHaveBeenCalledWith("dev-kagent");
  });

  it("prefers a namespace from the URL", async () => {
    await PromptsPage({
      searchParams: Promise.resolve({ namespace: "  dev-shared  " }),
    });

    expect(mockListPromptTemplates).toHaveBeenCalledWith("dev-shared");
  });
});
