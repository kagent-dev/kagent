import { getBackendUrl } from "./utils";
import { v4 as uuidv4 } from 'uuid';
import {
  A2A_PROTOCOL_VERSION,
  A2A_VERSION_HEADER,
  parseSseStream,
  SendMessageRequest,
  StreamResponse,
  SubscribeToTaskRequest,
} from '@a2a-js/sdk';
import type {
  SendMessageRequest as A2ASendMessageRequest,
  StreamResponse as A2AStreamResponse,
  SubscribeToTaskRequest as A2ASubscribeToTaskRequest,
} from '@a2a-js/sdk';
import { formatA2AClientError } from './a2aErrors';

export const A2A_JSONRPC_METHODS = {
  sendStreamingMessage: "SendStreamingMessage",
  subscribeToTask: "SubscribeToTask",
} as const;

export interface A2AJsonRpcRequest {
  jsonrpc: "2.0";
  method: string;
  params: Record<string, unknown>;
  id: string | number;
}

export class KagentA2AClient {
  private baseUrl: string;

  constructor() {
    this.baseUrl = getBackendUrl();
  }

  /**
   * Get the A2A URL for a specific agent
   */
  getAgentUrl(namespace: string, agentName: string, runInSandbox = false): string {
    const prefix = runInSandbox ? "a2a-sandboxes" : "a2a";
    return `${this.baseUrl}/${prefix}/${namespace}/${agentName}`;
  }

  /**
   * Create JSON-RPC request for message streaming
   */
  createStreamingRequest(params: A2ASendMessageRequest): A2AJsonRpcRequest {
    return {
      jsonrpc: "2.0",
      method: A2A_JSONRPC_METHODS.sendStreamingMessage,
      params: SendMessageRequest.toJSON(params) as Record<string, unknown>,
      id: uuidv4(),  // A2A server requires an id field
    };
  }

  /**
   * Send a streaming message using the A2A protocol via Next.js API route
   * Accepts an optional AbortSignal for cancellation support
   */
  async sendMessageStream(
    namespace: string,
    agentName: string,
    params: A2ASendMessageRequest,
    signal?: AbortSignal,
    runInSandbox = false,
    shareToken?: string
  ): Promise<AsyncIterable<A2AStreamResponse>> {
    const request = this.createStreamingRequest(params);
    const proxyUrl = runInSandbox
      ? `/a2a-sandboxes/${namespace}/${agentName}`
      : `/a2a/${namespace}/${agentName}`;

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'Accept': 'text/event-stream',
      [A2A_VERSION_HEADER]: A2A_PROTOCOL_VERSION,
    };
    if (shareToken) headers['X-Share-Token'] = shareToken;

    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers,
      body: JSON.stringify(request),
      signal,
    });

    if (!response.ok) {
      const errorText = await response.text();
      console.error("❌ Proxy request failed:", errorText);
      throw new Error(formatA2AClientError(errorText || `${response.status} ${response.statusText}`));
    }

    // Return an async iterable for SSE processing
    return this.processSSEStream(response);
  }

  /**
   * Resubscribe to an existing in-progress task's event stream.
   * Use this on page load when a task is still running to reconnect without
   * sending a new message. Fails if the task is already in a terminal state.
   */
  async resubscribeStream(
    namespace: string,
    agentName: string,
    taskId: string,
    signal?: AbortSignal,
    runInSandbox = false,
    shareToken?: string
  ): Promise<AsyncIterable<A2AStreamResponse>> {
    const request: A2AJsonRpcRequest = {
      jsonrpc: "2.0" as const,
      method: A2A_JSONRPC_METHODS.subscribeToTask,
      params: SubscribeToTaskRequest.toJSON({
        tenant: "",
        id: taskId,
      } satisfies A2ASubscribeToTaskRequest) as Record<string, unknown>,
      id: uuidv4(),
    };

    const proxyUrl = runInSandbox
      ? `/a2a-sandboxes/${namespace}/${agentName}`
      : `/a2a/${namespace}/${agentName}`;

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'Accept': 'text/event-stream',
      [A2A_VERSION_HEADER]: A2A_PROTOCOL_VERSION,
    };
    if (shareToken) headers['X-Share-Token'] = shareToken;

    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers,
      body: JSON.stringify(request),
      signal,
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(formatA2AClientError(errorText || `${response.status} ${response.statusText}`));
    }

    return this.processSSEStream(response);
  }

  /**
   * Process Server-Sent Events stream with proper event boundary detection
   */
  private async *processSSEStream(response: Response): AsyncIterable<A2AStreamResponse> {
    for await (const event of parseSseStream(response)) {
      if (event.data === '[DONE]') {
        return;
      }

      let eventData: unknown;
      try {
        eventData = JSON.parse(event.data) as unknown;
      } catch (error) {
        console.error("❌ Failed to parse SSE data:", error, event.data);
        continue;
      }

      const error = getA2AStreamError(event.type, eventData);
      if (error) {
        throw new Error(`A2A error ${error.code ?? "unknown"}: ${error.message ?? "unknown error"}`);
      }

      const payload = isRecord(eventData) && "result" in eventData ? eventData.result : eventData;
      yield StreamResponse.fromJSON(payload);
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function getA2AStreamError(
  eventType: string,
  eventData: unknown
): { code?: number; message?: string } | undefined {
  const errorData =
    isRecord(eventData) && isRecord(eventData.error)
      ? eventData.error
      : eventType === "error" && isRecord(eventData)
        ? eventData
        : undefined;

  if (!errorData) {
    return undefined;
  }

  return {
    code: typeof errorData.code === "number" ? errorData.code : undefined,
    message: typeof errorData.message === "string" ? errorData.message : undefined,
  };
}

export const kagentA2AClient = new KagentA2AClient(); 
