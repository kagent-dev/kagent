"use server";

import type { BaseResponse, PromptTemplateDetail, PromptTemplateSummary } from "@/types";
import { getPromptTemplateGrpcGateway } from "@/lib/grpc/client";
import { createErrorResponse } from "./utils";
import { revalidatePath } from "next/cache";

export async function listPromptTemplates(namespace: string): Promise<BaseResponse<PromptTemplateSummary[]>> {
  try {
    const gateway = await getPromptTemplateGrpcGateway();
    const promptTemplates = await gateway.listPromptTemplates(namespace);
    return { message: "Successfully listed prompt template ConfigMaps", data: promptTemplates };
  } catch (error) {
    return createErrorResponse<PromptTemplateSummary[]>(error, "Error listing prompt libraries");
  }
}

export async function getPromptTemplate(
  namespace: string,
  name: string,
): Promise<BaseResponse<PromptTemplateDetail>> {
  try {
    const gateway = await getPromptTemplateGrpcGateway();
    const promptTemplate = await gateway.getPromptTemplate(namespace, name);
    return { message: "Successfully retrieved prompt template library", data: promptTemplate };
  } catch (error) {
    return createErrorResponse<PromptTemplateDetail>(error, "Error loading prompt library");
  }
}

export async function createPromptTemplate(payload: {
  namespace: string;
  name: string;
  data: Record<string, string>;
}): Promise<BaseResponse<PromptTemplateDetail>> {
  try {
    const gateway = await getPromptTemplateGrpcGateway();
    const promptTemplate = await gateway.createPromptTemplate(payload.namespace, payload.name, payload.data);
    revalidatePath("/prompts");
    return { message: "Successfully created prompt template library", data: promptTemplate };
  } catch (error) {
    return createErrorResponse<PromptTemplateDetail>(error, "Error creating prompt library");
  }
}

export async function updatePromptTemplate(
  namespace: string,
  name: string,
  data: Record<string, string>,
): Promise<BaseResponse<PromptTemplateDetail>> {
  try {
    const gateway = await getPromptTemplateGrpcGateway();
    const promptTemplate = await gateway.updatePromptTemplate(namespace, name, data);
    revalidatePath("/prompts");
    revalidatePath(`/prompts/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`);
    return { message: "Successfully updated prompt template library", data: promptTemplate };
  } catch (error) {
    return createErrorResponse<PromptTemplateDetail>(error, "Error updating prompt library");
  }
}

export async function deletePromptTemplate(namespace: string, name: string): Promise<BaseResponse<void>> {
  try {
    const gateway = await getPromptTemplateGrpcGateway();
    await gateway.deletePromptTemplate(namespace, name);
    revalidatePath("/prompts");
    return { message: "Deleted" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting prompt library");
  }
}
