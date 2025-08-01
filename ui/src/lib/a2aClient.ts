/* eslint-disable @typescript-eslint/no-explicit-any */
import { getBackendUrl } from "./utils";
import { v4 as uuidv4 } from 'uuid';

export interface A2AMessage {
  role: "user" | "agent";
  parts: Array<{ kind: "text"; text: string }>;
  messageId: string;
}

export interface A2AMessageSendParams {
  message: A2AMessage;
  metadata?: Record<string, any>;
}

export interface A2AJsonRpcRequest {
  jsonrpc: "2.0";
  method: string;
  params: A2AMessageSendParams;
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
  getAgentUrl(namespace: string, agentName: string): string {
    return `${this.baseUrl}/a2a/${namespace}/${agentName}`;
  }

  /**
   * Create a message in A2A format
   */
  createA2AMessage(content: string, messageId: string): A2AMessage {
    return {
      messageId,
      role: "user",
      parts: [{ kind: "text", text: content }],
    };
  }

  /**
   * Create message send parameters for A2A
   */
  createMessageSendParams(message: A2AMessage): A2AMessageSendParams {
    return {
      message,
      metadata: {}
    };
  }

  /**
   * Create JSON-RPC request for message streaming
   */
  createStreamingRequest(params: A2AMessageSendParams): A2AJsonRpcRequest {
    return {
      jsonrpc: "2.0",
      method: "message/stream",
      params,
      id: uuidv4(),  // A2A server requires an id field
    };
  }

  /**
   * Send a streaming message using the A2A protocol via Next.js API route
   */
  async sendMessageStream(
    namespace: string, 
    agentName: string, 
    params: A2AMessageSendParams
  ): Promise<AsyncIterable<any>> {
    const request = this.createStreamingRequest(params);
    // This redirects to the Next.js API route 
    // Note that this route CAN'T be the same 
    // as the routes on the backend.
    const proxyUrl = `/a2a/${namespace}/${agentName}`;

    const response = await fetch(proxyUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
      },
      body: JSON.stringify(request),
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
   * Process Server-Sent Events stream with proper event boundary detection
   */
  private async *processSSEStream(body: ReadableStream<Uint8Array>): AsyncIterable<any> {
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
                
                try {
                  const eventData = JSON.parse(dataString);
                  yield eventData.result || eventData;
                } catch (error) {
                  console.error("❌ Failed to parse SSE data:", error, dataString);
                }
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