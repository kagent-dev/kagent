/**
 * Where the chat UI gets its transport.
 *
 * In mock mode this is the scripted client; in live mode the A2A client over
 * gRPC-Web. Either can be replaced with `setChatClientFactory`, which is how an
 * A2A version bump lands: register a different `ChatClient`, change nothing that
 * renders.
 */

import { isMockMode } from "../config";
import { A2AGrpcChatClient } from "./a2aGrpcChatClient";
import { MockChatClient } from "./mockChatClient";
import type { ChatClient } from "./types";

export type ChatClientFactory = () => ChatClient;

let factory: ChatClientFactory | null = null;
let instance: ChatClient | null = null;

/**
 * Installs the implementation `getChatClient` will hand out.
 *
 * Call it once, before the chat UI mounts. Registering replaces any client
 * already created, so a later call takes effect on the next `getChatClient`.
 */
export function setChatClientFactory(next: ChatClientFactory): void {
  factory = next;
  instance = null;
}

/** The chat transport, created on first use. */
export function getChatClient(): ChatClient {
  if (instance) return instance;

  if (factory) {
    instance = factory();
    return instance;
  }

  instance = isMockMode ? new MockChatClient() : new A2AGrpcChatClient();
  return instance;
}

/** Drops the cached client and any registered factory. Intended for tests. */
export function resetChatClient(): void {
  factory = null;
  instance = null;
}

export { A2AGrpcChatClient } from "./a2aGrpcChatClient";
export { MockChatClient } from "./mockChatClient";
export { conversationKey } from "./types";
export type {
  ChatClient,
  ChatAskUserPart,
  ChatConversationRef,
  ChatDataPart,
  ChatEvent,
  ChatMessage,
  ChatPart,
  ChatRole,
  ChatTextPart,
  ChatToolApprovalPart,
  ChatTurnState,
  SendMessageInput,
} from "./types";
