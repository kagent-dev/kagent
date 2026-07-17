"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { AgentResponse, AgentType } from "@/types";

export type ChatAgentModelInfo = {
  /** Model name currently used by the agent (e.g. "gpt-4o"). */
  model: string;
  /** Model provider (e.g. "OpenAI"). */
  modelProvider: string;
  /** Ref ("namespace/name") of the ModelConfig backing the agent. */
  modelConfigRef: string;
};

type ChatAgentRuntimeContextValue = {
  currentAgent: AgentResponse;
  agentType: AgentType;
  runInSandbox: boolean;
  substrateSandbox: boolean;
  modelInfo?: ChatAgentModelInfo;
};

const ChatAgentRuntimeContext = createContext<ChatAgentRuntimeContextValue | undefined>(undefined);

export function ChatAgentProvider({
  currentAgent,
  agentType,
  runInSandbox = false,
  substrateSandbox = false,
  modelInfo,
  children,
}: {
  currentAgent: AgentResponse;
  agentType: AgentType;
  runInSandbox?: boolean;
  substrateSandbox?: boolean;
  modelInfo?: ChatAgentModelInfo;
  children: ReactNode;
}) {
  return (
    <ChatAgentRuntimeContext.Provider value={{ currentAgent, agentType, runInSandbox, substrateSandbox, modelInfo }}>
      {children}
    </ChatAgentRuntimeContext.Provider>
  );
}

/** Agent resolved by the chat route layout. */
export function useCurrentChatAgent(): AgentResponse {
  const context = useContext(ChatAgentRuntimeContext);
  if (!context) {
    throw new Error("useCurrentChatAgent must be used within a ChatAgentProvider");
  }
  return context.currentAgent;
}

/** Agent type for the current chat route (from layout). Undefined outside provider. */
export function useChatAgentType(): AgentType | undefined {
  return useContext(ChatAgentRuntimeContext)?.agentType;
}

/** SandboxAgent workloads (API `runInSandbox`). */
export function useChatRunInSandbox(): boolean {
  return useContext(ChatAgentRuntimeContext)?.runInSandbox ?? false;
}

/** Agent Substrate sandbox (multi-session; session actors resume on send). */
export function useChatSubstrateSandbox(): boolean {
  return useContext(ChatAgentRuntimeContext)?.substrateSandbox ?? false;
}

/** Model info (model, provider, ModelConfig ref) for the current chat agent. */
export function useChatModelInfo(): ChatAgentModelInfo | undefined {
  return useContext(ChatAgentRuntimeContext)?.modelInfo;
}
