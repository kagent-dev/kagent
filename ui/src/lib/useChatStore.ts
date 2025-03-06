import { create } from "zustand";
import { ChatStatus, setupWebSocket, WebSocketManager } from "./ws";
import { AgentMessageConfig, InitialMessage, Message, Run, Session, Team, WebSocketMessage, SessionWithRuns } from "@/types/datamodel";
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
  team: Team | null;
  currentStreamingContent: string;
  currentStreamingMessage: Message | null;

  // Actions
  initializeNewChat: (agentId: number) => Promise<void>;
  sendUserMessage: (content: string, agentId: number) => Promise<void>;
  loadChat: (chatId: string) => Promise<void>;
  cleanup: () => void;
  handleWebSocketMessage: (message: WebSocketMessage) => void;
  setSessions: (sessions: SessionWithRuns[]) => void;
  addSession: (session: Session, runs: Run[]) => void;
  removeSession: (sessionId: number) => void;
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

  setSessions: (sessions) => set({ sessions }),

  addSession: (session, runs) =>
    set((state) => ({
      sessions: [{ session, runs }, ...state.sessions],
    })),

  removeSession: (sessionId) =>
    set((state) => ({
      sessions: state.sessions.filter((s) => s.session.id !== sessionId),
    })),

  initializeNewChat: async (agentId) => {
    try {
      // Clean up any existing websocket
      get().cleanup();
      const { team, session, run } = await startNewChat(agentId);
      // Add the new session to sessions list
      get().addSession(session, [run]);
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
      set((state) => {
        // If there was a streaming message in progress, replace it with the complete message
        const finalMessages = state.currentStreamingMessage
          ? [
              ...state.messages.filter((m) => m !== state.currentStreamingMessage),
              {
                config: messageConfig,
                session_id: session.id!,
                run_id: run.id,
                message_meta: {},
              },
            ]
          : [
              ...state.messages,
              {
                config: messageConfig,
                session_id: session.id!,
                run_id: run.id,
                message_meta: {},
              },
            ];

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
          currentStreamingContent: "",
          currentStreamingMessage: null,
        };
      });
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

      // Update sessions list
      if (session && run) {
        get().addSession(session, [run]);
      }

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