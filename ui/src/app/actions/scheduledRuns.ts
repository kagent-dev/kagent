"use server";

import { ScheduledRun, ScheduledRunExecution, ScheduledRunExecutionRecord, BaseResponse } from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Gets all Scheduled Runs
 * @returns A promise with all Scheduled Runs
 */
export async function getScheduledRuns(): Promise<BaseResponse<ScheduledRun[]>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRun[]>>("/scheduledruns");
    return { message: "Successfully fetched Scheduled Runs", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun[]>(error, "Error getting Scheduled Runs");
  }
}

/**
 * Gets a specific Scheduled Run
 * @param name The Scheduled Run name
 * @param namespace The Scheduled Run namespace
 * @returns A promise with the Scheduled Run
 */
export async function getScheduledRun(name: string, namespace: string): Promise<BaseResponse<ScheduledRun>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRun>>(`/scheduledruns/${namespace}/${name}`);
    return { message: "Successfully fetched Scheduled Run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error getting Scheduled Run");
  }
}

export async function getScheduledRunExecutions(
  name: string,
  namespace: string,
  before?: string,
  beforeID?: string,
  limit = 50,
): Promise<BaseResponse<ScheduledRunExecutionRecord[]>> {
  try {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    if (beforeID) params.set("beforeID", beforeID);
    const response = await fetchApi<BaseResponse<ScheduledRunExecutionRecord[]>>(
      `/scheduledruns/${namespace}/${name}/executions?${params.toString()}`,
    );
    return { message: "Successfully fetched executions", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRunExecutionRecord[]>(error, "Error getting executions");
  }
}

/**
 * Creates a new Scheduled Run
 * @param sr The Scheduled Run to create
 * @returns A promise with the created Scheduled Run
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
    return { message: "Successfully created Scheduled Run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error creating Scheduled Run");
  }
}

/**
 * Updates an existing Scheduled Run
 * @param sr The Scheduled Run to update
 * @returns A promise with the updated Scheduled Run
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
    return { message: "Successfully updated Scheduled Run", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRun>(error, "Error updating Scheduled Run");
  }
}

/**
 * Deletes a Scheduled Run
 * @param name The Scheduled Run name
 * @param namespace The Scheduled Run namespace
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
    return { message: "Successfully deleted Scheduled Run" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting Scheduled Run");
  }
}

/**
 * Triggers a manual execution of a Scheduled Run. The backend dispatches synchronously
 * and returns the initial ScheduledRunExecution. Every status except InProgress
 * is terminal; InProgress is resolved asynchronously in execution history.
 */
export async function triggerScheduledRun(name: string, namespace: string): Promise<BaseResponse<ScheduledRunExecution>> {
  try {
    const response = await fetchApi<BaseResponse<ScheduledRunExecution>>(`/scheduledruns/${namespace}/${name}/trigger`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
    });

    revalidatePath("/schedules");
    return { message: "Successfully triggered execution", data: response.data };
  } catch (error) {
    return createErrorResponse<ScheduledRunExecution>(error, "Error triggering execution");
  }
}
