/* eslint-disable @typescript-eslint/no-explicit-any */
import { getBackendUrl } from "./utils";
import { v4 as uuidv4 } from 'uuid';
import { A2A_PROTOCOL_VERSION, A2A_VERSION_HEADER, Message } from "@a2a-js/sdk";
import type { Message as A2AMessage, StreamResponse } from "@a2a-js/sdk";

// A2A JSON-RPC methods
// NOTE: These are not exported by @a2a-js/sdk, so we need to define them here.
export const A2A_JSONRPC_METHODS = {
  sendStreamingMessage: "SendStreamingMessage",
  subscribeToTask: "SubscribeToTask",
} as const;

export interface A2AJsonRpcRequest {
  jsonrpc: "2.0";
  method: string;
  params: {
    message: unknown;
    metadata?: Record<string, unknown>;
  };
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
  createStreamingRequest(params: { message: A2AMessage; metadata?: Record<string, unknown> }): A2AJsonRpcRequest {
    return {
      jsonrpc: "2.0",
      method: A2A_JSONRPC_METHODS.sendStreamingMessage,
      params: {
        ...params,
        message: Message.toJSON(params.message),
      },
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
    params: { message: A2AMessage; metadata?: Record<string, unknown> },
    signal?: AbortSignal,
    runInSandbox = false
  ): Promise<AsyncIterable<StreamResponse>> {
    const request = this.createStreamingRequest(params);
    const proxyUrl = runInSandbox
      ? `/a2a-sandboxes/${namespace}/${agentName}`
      : `/a2a/${namespace}/${agentName}`;

    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
        [A2A_VERSION_HEADER]: A2A_PROTOCOL_VERSION,
      },
      body: JSON.stringify(request),
      signal,
    });

    if (!response.ok) {
      const errorText = await response.text();
      console.error("❌ Proxy request failed:", errorText);
      throw new Error(`A2A proxy request failed: ${response.status} ${response.statusText} - ${errorText}`);
    }

    if (!response.body) {
      throw new Error('Response body is null');
    }

    // Return an async iterable for SSE processing
    return this.processSSEStream(response.body);
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
    runInSandbox = false
  ): Promise<AsyncIterable<any>> {
    const request = {
      jsonrpc: "2.0" as const,
      method: A2A_JSONRPC_METHODS.subscribeToTask,
      params: { id: taskId },
      id: uuidv4(),
    };

    const proxyUrl = runInSandbox
      ? `/a2a-sandboxes/${namespace}/${agentName}`
      : `/a2a/${namespace}/${agentName}`;

    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
        [A2A_VERSION_HEADER]: A2A_PROTOCOL_VERSION,
      },
      body: JSON.stringify(request),
      signal,
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Resubscribe failed: ${response.status} ${response.statusText} - ${errorText}`);
    }

    if (!response.body) {
      throw new Error('Response body is null');
    }

    return this.processSSEStream(response.body);
  }

  /**
   * Process Server-Sent Events stream with proper event boundary detection
   */
  private async *processSSEStream(body: ReadableStream<Uint8Array>): AsyncIterable<StreamResponse> {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
      while (true) {
        const { value, done } = await reader.read();

        if (done) {
          break;
        }

        buffer += decoder.decode(value, { stream: true });

        // Process complete SSE events (delimited by \n\n)
        let eventEndIndex;
        while ((eventEndIndex = buffer.indexOf('\n\n')) >= 0) {
          const eventText = buffer.substring(0, eventEndIndex);
          buffer = buffer.substring(eventEndIndex + 2);

          if (eventText.trim()) {
            const lines = eventText.split('\n');
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const dataString = line.substring(6);

                if (dataString === '[DONE]') {
                  return;
                }

                let eventData: Record<string, unknown>;
                try {
                  eventData = JSON.parse(dataString);
                } catch {
                  console.error("❌ Failed to parse SSE data:", dataString);
                  continue;
                }
                if (eventData.error) {
                  const err = eventData.error as { code?: number; message?: string };
                  throw new Error(`A2A error ${err.code ?? "unknown"}: ${err.message ?? "unknown error"}`);
                }
                yield (eventData.result || eventData) as StreamResponse;
              }
            }
          }
        }
      }
    } finally {
      reader.releaseLock();
    }
  }
}

export const kagentA2AClient = new KagentA2AClient(); 
