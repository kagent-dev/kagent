"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { AgentType } from "@/types";

type ChatAgentRuntimeContextValue = {
  agentType: AgentType;
  runInSandbox: boolean;
  substrateSandbox: boolean;
};

const ChatAgentRuntimeContext = createContext<ChatAgentRuntimeContextValue | undefined>(undefined);

export function ChatAgentProvider({
  agentType,
  runInSandbox = false,
  substrateSandbox = false,
  children,
}: {
  agentType: AgentType;
  runInSandbox?: boolean;
  substrateSandbox?: boolean;
  children: ReactNode;
}) {
  return (
    <ChatAgentRuntimeContext.Provider value={{ agentType, runInSandbox, substrateSandbox }}>
      {children}
    </ChatAgentRuntimeContext.Provider>
  );
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
