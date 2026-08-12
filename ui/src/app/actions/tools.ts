"use server";

import { ToolsResponse } from "@/types";
import { getToolGrpcGateway } from "@/lib/grpc/client";

/**
 * Gets all available tools
 * @returns A promise with all tools
 */
export async function getTools(): Promise<ToolsResponse[]> {
  try {
    const gateway = await getToolGrpcGateway();
    return await gateway.listTools();
  } catch (error) {
    throw new Error(`Error getting built-in tools. ${error}`);
  }
}
