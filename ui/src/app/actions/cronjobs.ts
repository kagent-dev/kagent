"use server";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { BaseResponse, AgentCronJob } from "@/types";

export async function getCronJobs(): Promise<BaseResponse<AgentCronJob[]>> {
  try {
    const response = await fetchApi<BaseResponse<AgentCronJob[]>>("/cronjobs");

    if (!response) {
      throw new Error("Failed to get cron jobs");
    }

    response.data?.sort((a, b) =>
      `${a.metadata.namespace}/${a.metadata.name}`.localeCompare(
        `${b.metadata.namespace}/${b.metadata.name}`
      )
    );

    return {
      message: "Cron jobs fetched successfully",
      data: response.data ?? [],
    };
  } catch (error) {
    return createErrorResponse<AgentCronJob[]>(error, "Error getting cron jobs");
  }
}

export async function getCronJob(namespace: string, name: string): Promise<BaseResponse<AgentCronJob>> {
  try {
    const response = await fetchApi<BaseResponse<AgentCronJob>>(`/cronjobs/${namespace}/${name}`);

    if (!response) {
      throw new Error("Failed to get cron job");
    }

    return {
      message: "Cron job fetched successfully",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<AgentCronJob>(error, "Error getting cron job");
  }
}

export async function createCronJob(cronJob: AgentCronJob): Promise<BaseResponse<AgentCronJob>> {
  try {
    const response = await fetchApi<BaseResponse<AgentCronJob>>("/cronjobs", {
      method: "POST",
      body: JSON.stringify(cronJob),
    });

    if (!response) {
      throw new Error("Failed to create cron job");
    }

    revalidatePath("/cronjobs");

    return {
      message: "Cron job created successfully",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<AgentCronJob>(error, "Error creating cron job");
  }
}

export async function updateCronJob(
  namespace: string,
  name: string,
  cronJob: AgentCronJob
): Promise<BaseResponse<AgentCronJob>> {
  try {
    const response = await fetchApi<BaseResponse<AgentCronJob>>(`/cronjobs/${namespace}/${name}`, {
      method: "PUT",
      body: JSON.stringify(cronJob),
    });

    if (!response) {
      throw new Error("Failed to update cron job");
    }

    revalidatePath("/cronjobs");

    return {
      message: "Cron job updated successfully",
      data: response.data,
    };
  } catch (error) {
    return createErrorResponse<AgentCronJob>(error, "Error updating cron job");
  }
}

export async function deleteCronJob(namespace: string, name: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/cronjobs/${namespace}/${name}`, {
      method: "DELETE",
    });

    revalidatePath("/cronjobs");
    return { message: "Cron job deleted successfully" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting cron job");
  }
}
