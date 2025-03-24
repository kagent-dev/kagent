"use server";
import { fetchApi } from "./utils";
import { BaseResponse, Model } from "@/lib/types";

export async function getModels(): Promise<BaseResponse<Model[]>> {
  const response = await fetchApi<Model[]>("/modelconfigs");

  if (!response) {
    return {
      success: false,
      error: "Failed to get models. Please try again.",
      data: [],
    };
  }

  return {
    success: true,
    data: response,
  };
}
