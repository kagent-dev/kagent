'use server'
import { RemoteMCPServer, MCPServer, ToolServerCreateRequest, ToolServerResponse } from "@/types";
import { createErrorResponse } from "./utils";
import { BaseResponse } from "@/types";
import { revalidatePath } from "next/cache";
import { getToolGrpcGateway } from "@/lib/grpc/client";

/**
 * Fetches all tool servers
 * @returns Promise with server data
 */
export async function getServers(): Promise<BaseResponse<ToolServerResponse[]>> {
  try {
    const gateway = await getToolGrpcGateway();

    return {
      message: "MCP servers fetched successfully",
      data: await gateway.listToolServers(),
    };
  } catch (error) {
    return createErrorResponse<ToolServerResponse[]>(error, "Error getting MCP servers");
  }
}

/**
 * Deletes a server
 * @param serverName Server name to delete (format: namespace/name)
 * @returns Promise with delete result
 */
export async function deleteServer(serverName: string): Promise<BaseResponse<void>> {
  try {
    const { namespace, name } = splitServerRef(serverName);
    const gateway = await getToolGrpcGateway();
    await gateway.deleteToolServer(namespace, name);

    revalidatePath("/mcp");
    revalidatePath("/mcp/new");
    return {
      message: "MCP server deleted successfully",
    };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting MCP server");
  }
}

/**
 * Creates a new server
 * @param serverData Server data to create
 * @returns Promise with create result
 */
export async function createServer(serverData: ToolServerCreateRequest): Promise<BaseResponse<RemoteMCPServer | MCPServer>> {
  try {
    const gateway = await getToolGrpcGateway();
    const created = await gateway.createToolServer(serverData);

    revalidatePath("/mcp");
    revalidatePath("/mcp/new");
    return { message: "MCP server created successfully", data: created };
  } catch (error) {
    return createErrorResponse<RemoteMCPServer | MCPServer>(error, "Error creating MCP server");
  }
}

/**
 * Fetches all supported tool server types
 * @returns Promise with server data
 */
export async function getToolServerTypes(): Promise<BaseResponse<string[]>> {
  try {
    const gateway = await getToolGrpcGateway();

    return {
      message: "Tool server types fetched successfully",
      data: await gateway.listToolServerTypes(),
    };
  } catch (error) {
    return createErrorResponse<string[]>(error, "Error getting tool server types");
  }
}

function splitServerRef(serverRef: string): { namespace: string; name: string } {
  const separator = serverRef.indexOf("/");
  if (separator <= 0 || separator === serverRef.length - 1 || serverRef.indexOf("/", separator + 1) !== -1) {
    throw new Error("ToolServer reference must use namespace/name format");
  }
  return {
    namespace: serverRef.slice(0, separator),
    name: serverRef.slice(separator + 1),
  };
}
