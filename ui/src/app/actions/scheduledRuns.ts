"use server";

import { ScheduledRun, BaseResponse } from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Gets all scheduled runs
 * @returns A promise with all scheduled runs
 */
export async function getScheduledRuns(): Promise<BaseResponse<ScheduledRun[]>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRun[]>>("/scheduledruns");
    return { message: "Successfully fetched scheduled runs", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun[]>(error, "Error getting scheduled runs");
  }
}

/**
 * Gets a specific scheduled run
 * @param name The scheduled run name
 * @param namespace The scheduled run namespace
 * @returns A promise with the scheduled run
 */
export async function getScheduledRun(name: string, namespace: string): Promise<BaseResponse<ScheduledRun>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRun>>(`/scheduledruns/${namespace}/${name}`);
    return { message: "Successfully fetched scheduled run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error getting scheduled run");
  }
}

/**
 * Creates a new scheduled run
 * @param sr The scheduled run to create
 * @returns A promise with the created scheduled run
 */
export async function createScheduledRun(sr: ScheduledRun): Promise<BaseResponse<ScheduledRun>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRun>>("/scheduledruns", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(sr),
    });

    revalidatePath("/schedules");
    return { message: "Successfully created scheduled run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error creating scheduled run");
  }
}

/**
 * Updates an existing scheduled run
 * @param sr The scheduled run to update
 * @returns A promise with the updated scheduled run
 */
export async function updateScheduledRun(sr: ScheduledRun): Promise<BaseResponse<ScheduledRun>> {
  try {
    const namespace = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    const response = await fetchApi<BaseResponse<ScheduledRun>>(`/scheduledruns/${namespace}/${name}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(sr),
    });

    revalidatePath("/schedules");
    return { message: "Successfully updated scheduled run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error updating scheduled run");
  }
}

/**
 * Deletes a scheduled run
 * @param name The scheduled run name
 * @param namespace The scheduled run namespace
 * @returns A promise with the delete result
 */
export async function deleteScheduledRun(name: string, namespace: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/scheduledruns/${namespace}/${name}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

    revalidatePath("/schedules");
    return { message: "Successfully deleted scheduled run" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting scheduled run");
  }
}

/**
 * Triggers a manual run of a scheduled run
 * @param name The scheduled run name
 * @param namespace The scheduled run namespace
 * @returns A promise with the trigger result
 */
export async function triggerScheduledRun(name: string, namespace: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/scheduledruns/${namespace}/${name}/trigger`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
    });

    revalidatePath("/schedules");
    return { message: "Successfully triggered scheduled run" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error triggering scheduled run");
  }
}
