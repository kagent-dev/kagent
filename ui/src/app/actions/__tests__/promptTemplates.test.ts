import {
  createPromptTemplate,
  deletePromptTemplate,
  getPromptTemplate,
  listPromptTemplates,
  updatePromptTemplate,
} from "@/app/actions/promptTemplates";
import { getPromptTemplateGrpcGateway } from "@/lib/grpc/client";
import { revalidatePath } from "next/cache";

jest.mock("@/app/actions/utils", () => ({
  createErrorResponse: jest.fn((err: unknown, message: string) => ({
    error: true,
    message,
  })),
}));

jest.mock("@/lib/grpc/client", () => ({
  getPromptTemplateGrpcGateway: jest.fn(),
}));

jest.mock("next/cache", () => ({
  revalidatePath: jest.fn(),
}));

const listPromptTemplatesGateway = jest.fn();
const getPromptTemplateGateway = jest.fn();
const createPromptTemplateGateway = jest.fn();
const updatePromptTemplateGateway = jest.fn();
const deletePromptTemplateGateway = jest.fn();
const mockedGetPromptTemplateGrpcGateway = getPromptTemplateGrpcGateway as jest.MockedFunction<
  typeof getPromptTemplateGrpcGateway
>;

describe("prompt template server actions", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    listPromptTemplatesGateway.mockResolvedValue([]);
    getPromptTemplateGateway.mockResolvedValue({ namespace: "team", name: "library", data: {} });
    createPromptTemplateGateway.mockResolvedValue({ namespace: "team", name: "library", data: {} });
    updatePromptTemplateGateway.mockResolvedValue({ namespace: "team", name: "library", data: {} });
    deletePromptTemplateGateway.mockResolvedValue(undefined);
    mockedGetPromptTemplateGrpcGateway.mockResolvedValue({
      listPromptTemplates: listPromptTemplatesGateway,
      getPromptTemplate: getPromptTemplateGateway,
      createPromptTemplate: createPromptTemplateGateway,
      updatePromptTemplate: updatePromptTemplateGateway,
      deletePromptTemplate: deletePromptTemplateGateway,
    } as never);
  });

  it("lists and gets prompt templates through gRPC", async () => {
    listPromptTemplatesGateway.mockResolvedValueOnce([{
      namespace: "team",
      name: "library",
      keyCount: 1,
      keys: ["intro"],
    }]);
    getPromptTemplateGateway.mockResolvedValueOnce({
      namespace: "team",
      name: "library",
      data: { intro: "hello" },
    });

    await expect(listPromptTemplates("team")).resolves.toEqual({
      message: "Successfully listed prompt template ConfigMaps",
      data: [{ namespace: "team", name: "library", keyCount: 1, keys: ["intro"] }],
    });
    await expect(getPromptTemplate("team", "library")).resolves.toEqual({
      message: "Successfully retrieved prompt template library",
      data: { namespace: "team", name: "library", data: { intro: "hello" } },
    });
    expect(listPromptTemplatesGateway).toHaveBeenCalledWith("team");
    expect(getPromptTemplateGateway).toHaveBeenCalledWith("team", "library");
  });

  it("creates, updates, and deletes through gRPC with the existing revalidation paths", async () => {
    const data = { intro: "hello" };
    createPromptTemplateGateway.mockResolvedValueOnce({ namespace: "team", name: "library", data });
    updatePromptTemplateGateway.mockResolvedValueOnce({ namespace: "team", name: "library", data });

    await expect(createPromptTemplate({ namespace: "team", name: "library", data })).resolves.toEqual({
      message: "Successfully created prompt template library",
      data: { namespace: "team", name: "library", data },
    });
    await expect(updatePromptTemplate("team", "library", data)).resolves.toEqual({
      message: "Successfully updated prompt template library",
      data: { namespace: "team", name: "library", data },
    });
    await expect(deletePromptTemplate("team", "library")).resolves.toEqual({ message: "Deleted" });

    expect(createPromptTemplateGateway).toHaveBeenCalledWith("team", "library", data);
    expect(updatePromptTemplateGateway).toHaveBeenCalledWith("team", "library", data);
    expect(deletePromptTemplateGateway).toHaveBeenCalledWith("team", "library");
    expect(revalidatePath).toHaveBeenCalledWith("/prompts");
    expect(revalidatePath).toHaveBeenCalledWith("/prompts/team/library");
  });

  it("returns a compatibility error response when gRPC fails", async () => {
    listPromptTemplatesGateway.mockRejectedValueOnce(new Error("backend unavailable"));

    await expect(listPromptTemplates("team")).resolves.toEqual({
      error: true,
      message: "Error listing prompt libraries",
    });
  });
});
