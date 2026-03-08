"use server";

import { DashboardStatsResponse } from "@/types";
import { fetchApi } from "./utils";

export async function getDashboardStats(): Promise<DashboardStatsResponse> {
  return fetchApi<DashboardStatsResponse>("/dashboard/stats");
}
