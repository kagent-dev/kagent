/**
 * Where the conversation UI gets its transport.
 *
 * In mock mode this is the scripted client; in live mode the A2A client over
 * gRPC-Web. Either can be replaced with `setConversationClientFactory`, which is how an
 * A2A version bump lands: register a different `ConversationClient`, change nothing that
 * renders.
 */

import { isMockMode } from "../config";
import { A2AGrpcConversationClient } from "./a2aGrpcConversationClient";
import { MockConversationClient } from "./mockConversationClient";
import type { ConversationClient } from "./types";

export type ConversationClientFactory = () => ConversationClient;

let factory: ConversationClientFactory | null = null;
let instance: ConversationClient | null = null;

/**
 * Installs the implementation `getConversationClient` will hand out.
 *
 * Call it once, before the conversation UI mounts. Registering replaces any client
 * already created, so a later call takes effect on the next `getConversationClient`.
 */
export function setConversationClientFactory(next: ConversationClientFactory): void {
  factory = next;
  instance = null;
}

/** The conversation transport, created on first use. */
export function getConversationClient(): ConversationClient {
  if (instance) return instance;

  if (factory) {
    instance = factory();
    return instance;
  }

  instance = isMockMode ? new MockConversationClient() : new A2AGrpcConversationClient();
  return instance;
}

/** Drops the cached client and any registered factory. Intended for tests. */
export function resetConversationClient(): void {
  factory = null;
  instance = null;
}

export { A2AGrpcConversationClient } from "./a2aGrpcConversationClient";
export { MockConversationClient } from "./mockConversationClient";
export { conversationKey } from "./types";
export type {
  ConversationClient,
  ConversationAskUserPart,
  ConversationRef,
  ConversationDataPart,
  ConversationEvent,
  ConversationMessage,
  ConversationPart,
  ConversationRole,
  ConversationTextPart,
  ConversationFilePart,
  ConversationToolApprovalPart,
  ConversationTurnState,
  SendMessageInput,
} from "./types";
