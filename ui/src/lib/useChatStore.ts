import { create } from "zustand";
import { ChatStatus, setupWebSocket, WebSocketManager } from "./ws";
import { AgentMessageConfig, InitialMessage, Message, Run, Session, WebSocketMessage, SessionWithRuns, AgentResponse } from "@/types/datamodel";
import { loadExistingChat, sendMessage, startNewChat } from "@/app/actions/chat";
import { messageUtils } from "./utils";

interface ChatState {
  session: Session | null;
  sessions: SessionWithRuns[];
  run: Run | null;
  messages: Message[];
  status: ChatStatus;
  error: string | null;
  websocketManager: WebSocketManager | null;
  team: AgentResponse | null;
  currentStreamingContent: string;
  currentStreamingMessage: Message | null;

  // Actions
  initializeNewChat: (agentId: string) => Promise<void>;
  sendUserMessage: (content: string, agentId: string) => Promise<void>;
  loadChat: (chatId: string) => Promise<void>;
  cleanup: () => void;
  handleWebSocketMessage: (message: WebSocketMessage) => void;
}

const useChatStore = create<ChatState>((set, get) => ({
  session: null,
  sessions: [],
  run: null,
  messages: [],
  status: "ready",
  error: null,
  websocketManager: null,
  team: null,
  currentStreamingContent: "",
  currentStreamingMessage: null,

  initializeNewChat: async (agentId) => {
    try {
      // Clean up any existing websocket
      get().cleanup();
      const { team, session, run } = await startNewChat(agentId);
      // Add the new session to sessions list
      set({
        team,
        session,
        run,
        messages: [],
        status: "ready",
        error: null,
        websocketManager: null,
        currentStreamingContent: "",
        currentStreamingMessage: null,
      });
    } catch (error) {
      set({
        error: `Failed to start chat: ${error}`,
        status: "error",
        session: null,
        run: null,
        messages: [],
        websocketManager: null,
        currentStreamingContent: "",
        currentStreamingMessage: null,
      });
    }
  },

  handleWebSocketMessage: (message: WebSocketMessage) => {
    const state = get();
    const { run, session } = state;

    if (!run || !session?.id || !message.data) {
      console.warn("Invalid message or state", { run, session, message });
      return;
    }

    const messageConfig = message.data as AgentMessageConfig;
    if (messageUtils.isUserTextMessageContent(messageConfig)) {
      return;
    }

    if (
      messageUtils.isTeamResult(messageConfig) ||
      messageUtils.isFunctionExecutionResult(messageConfig.content) ||
      messageUtils.isToolCallContent(messageConfig.content) ||
      messageUtils.isMultiModalContent(messageConfig.content) ||
      messageUtils.isLlmCallEvent(messageConfig.content)
    ) {
      const systemMessage = {
        config: messageConfig,
        session_id: session.id!,
        run_id: run.id,
        message_meta: {},
      };

      set((state) => ({
        messages: [...state.messages, systemMessage],
        run: {
          ...run,
          messages: [...run.messages, systemMessage],
        },
      }));
      return;
    }

    // Check if this is a streaming chunk or complete message
    const isStreamingChunk = message.type === "message_chunk";

    if (isStreamingChunk) {
      // If this is a new streaming message (different source)
      if (!state.currentStreamingMessage || state.currentStreamingMessage.config.source !== messageConfig.source) {
        // Create new streaming message
        const newStreamingMessage = {
          config: {
            ...messageConfig,
            content: [String(messageConfig.content)],
          },
          session_id: session.id,
          run_id: run.id,
          message_meta: {},
        };
        set({
          currentStreamingMessage: newStreamingMessage,
          currentStreamingContent: String(messageConfig.content),
        });
      } else {
        // Append to existing streaming content
        set((state) => {
          const updatedContent = state.currentStreamingContent + String(messageConfig.content);

          // Update the currentStreamingMessage's content
          const updatedStreamingMessage = {
            ...state.currentStreamingMessage!,
            config: {
              ...state.currentStreamingMessage!.config,
              content: [updatedContent],
            },
          };

          return {
            currentStreamingContent: updatedContent,
            currentStreamingMessage: updatedStreamingMessage,
          };
        });
      }
    } else {
      // For non-streaming/complete messages

      // First check if this is the completion of a message we were streaming
      const isCompletionOfStreamingMessage = state.currentStreamingMessage && state.currentStreamingMessage.config.source === messageConfig.source;

      set((state) => {
        // If this is completing a streaming message, replace the streaming state with the final message
        // but don't add it to the messages array yet
        if (isCompletionOfStreamingMessage) {
          const updatedRun = {
            ...run,
            status: message.status || run.status,
          };

          return {
            run: updatedRun,
            // Don't touch messages array yet
            currentStreamingContent: "",
            currentStreamingMessage: {
              // Update the streaming message to its final form
              config: messageConfig,
              session_id: session.id!,
              run_id: run.id,
              message_meta: {},
            },
          };
        } else {
          // This is a new complete message (not related to streaming)
          const finalMessage = {
            config: messageConfig,
            session_id: session.id!,
            run_id: run.id,
            message_meta: {},
          };

          // Check the finalMessage is not included in the messages array (avoid duplicates)
          if (state.messages.find((m) => m.config.source === finalMessage.config.source && m.config.content === finalMessage.config.content)) {
            return state;
          }

          const finalMessages = [...state.messages, finalMessage];

          const updatedRun = {
            ...run,
            messages: finalMessages,
            status: message.status || run.status,
          };

          const updatedSessions = state.sessions.map((s) =>
            s.session.id === session.id
              ? {
                  ...s,
                  runs: s.runs.map((r) => (r.id === run.id ? updatedRun : r)),
                }
              : s
          );

          return {
            messages: finalMessages,
            run: updatedRun,
            sessions: updatedSessions,
          };
        }
      });

      // IMPORTANT: If this was a completion of a streaming message, add it to messages in a separate update
      // This ensures the UI has time to process the clearing of streaming state
      if (isCompletionOfStreamingMessage) {
        // Small delay to allow React to process the streaming state change
        setTimeout(() => {
          set((state) => {
            const finalMessage = state.currentStreamingMessage;

            if (!finalMessage) return state; // Safety check

            const finalMessages = [...state.messages, finalMessage];

            const updatedRun = {
              ...state.run!,
              messages: finalMessages,
            };

            const updatedSessions = state.sessions.map((s) =>
              s.session.id === session.id
                ? {
                    ...s,
                    runs: s.runs.map((r) => (r.id === updatedRun.id ? updatedRun : r)),
                  }
                : s
            );

            return {
              messages: finalMessages,
              run: updatedRun,
              sessions: updatedSessions,
              currentStreamingMessage: null, // Now fully clear the streaming message
            };
          });
        }, 10); // Small delay, just enough for React to process the first update
      }
    }
  },

  sendUserMessage: async (content, agentId) => {
    const state = get();
    set({ status: "thinking" });

    try {
      // If no session exists, create one
      if (!state.session || !state.run) {
        await get().initializeNewChat(agentId);
        set({ status: "thinking" });
      }

      const currentState = get();
      const { session, run, team } = currentState;
      if (!session?.id || !run || !team) {
        throw new Error("Failed to initialize chat session");
      }

      // Send the message
      const userMessage = await sendMessage(content, run.id, session.id);

      // Update state with user message
      set((state) => {
        const updatedRun = {
          ...run,
          messages: [...run.messages, userMessage],
        };

        const updatedSessions = state.sessions.map((s) => (s.session.id === session.id ? { ...s, runs: s.runs.map((r) => (r.id === run.id ? updatedRun : r)) } : s));

        return {
          messages: [...state.messages, userMessage],
          run: updatedRun,
          sessions: updatedSessions,
        };
      });

      // Check if we already have a websocket manager
      let manager = currentState.websocketManager;

      if (!manager) {
        // First message - setup WebSocket
        const startMessage: InitialMessage = {
          type: "start",
          task: content,
          team_config: team.component,
        };

        manager = setupWebSocket(
          run.id,
          {
            onMessage: (message) => get().handleWebSocketMessage(message),
            onError: (error) => set({ error, status: "error" }),
            onClose: () => {
              const currentStatus = get().status;
              if (currentStatus !== "error") {
                set({ status: "ready" });
              }
            },
            onStatusChange: (status) => set({ status }),
          },
          startMessage
        );

        set({ websocketManager: manager });
      } else {
        // Subsequent messages - send as input_response
        manager.send(
          JSON.stringify({
            type: "input_response",
            response: content,
            runId: run.id,
            sessionId: session.id,
          })
        );
      }
    } catch (error) {
      set({
        error: `Failed to send message: ${error}`,
        status: "error",
      });
    }
  },

  loadChat: async (chatId) => {
    set({ status: "ready" });
    try {
      // Clean up existing websocket
      get().cleanup();

      const { session, run, team, messages } = await loadExistingChat(chatId);

      const initialStatus: ChatStatus = run.status === "error" || run.status === "timeout" ? "error" : "ready";

      set({
        session,
        run,
        team,
        messages,
        status: initialStatus,
        error: run.error_message || null,
        websocketManager: null,
        currentStreamingContent: "",
        currentStreamingMessage: null,
      });
    } catch (error) {
      set({
        error: `Failed to load chat: ${error}`,
        status: "error",
        session: null,
        run: null,
        messages: [],
        websocketManager: null,
        currentStreamingContent: "",
        currentStreamingMessage: null,
      });
    }
  },

  cleanup: () => {
    const { websocketManager } = get();
    if (websocketManager) {
      websocketManager.cleanup();
    }
    set({
      session: null,
      run: null,
      messages: [],
      status: "ready",
      websocketManager: null,
      error: null,
      team: null,
      currentStreamingContent: "",
      currentStreamingMessage: null,
    });
  },
}));

export default useChatStore;
