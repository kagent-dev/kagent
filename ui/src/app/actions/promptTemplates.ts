"use server";

import type { BaseResponse, PromptTemplateDetail, PromptTemplateSummary } from "@/types";
import { createErrorResponse, fetchApi } from "./utils";
import { revalidatePath } from "next/cache";

export async function listPromptTemplates(namespace: string): Promise<BaseResponse<PromptTemplateSummary[]>> {
  try {
    const q = new URLSearchParams({ namespace });
    const res = await fetchApi<BaseResponse<PromptTemplateSummary[]>>(`/prompttemplates?${q.toString()}`);
    return { message: res.message || "ok", data: res.data };
  } catch (error) {
    return createErrorResponse<PromptTemplateSummary[]>(error, "Error listing prompt libraries");
  }
}

export async function getPromptTemplate(
  namespace: string,
  name: string,
): Promise<BaseResponse<PromptTemplateDetail>> {
  try {
    const res = await fetchApi<BaseResponse<PromptTemplateDetail>>(
      `/prompttemplates/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    );
    return { message: res.message || "ok", data: res.data };
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
    const res = await fetchApi<BaseResponse<PromptTemplateDetail>>("/prompttemplates", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    revalidatePath("/prompts");
    return { message: res.message || "ok", data: res.data };
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
    const res = await fetchApi<BaseResponse<PromptTemplateDetail>>(
      `/prompttemplates/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify({ data }),
      },
    );
    revalidatePath("/prompts");
    revalidatePath(`/prompts/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`);
    return { message: res.message || "ok", data: res.data };
  } catch (error) {
    return createErrorResponse<PromptTemplateDetail>(error, "Error updating prompt library");
  }
}

export async function deletePromptTemplate(namespace: string, name: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/prompttemplates/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {
      method: "DELETE",
    });
    revalidatePath("/prompts");
    return { message: "Deleted" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting prompt library");
  }
}
