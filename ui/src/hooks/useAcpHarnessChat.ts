"use client";

// useAcpHarnessChat — the Agent Client Protocol state machine behind
// AcpHarnessChat. Owns the WebSocket to the controller's same-origin proxy
// (/api/agentharnesses/{ns}/{name}/acp), the JSON-RPC request/response
// bookkeeping, and the session lifecycle (initialize → session/new |
// session/load → session/prompt). It maps streaming session/update
// notifications onto the A2A Message shapes the chat UI renders, and exposes a
// tiny imperative surface — connect / sendMessage / cancel — plus the derived
// render state (conn / messages / streamingContent). All transport concerns
// live here so the component stays presentational.

import { useCallback, useEffect, useRef, useState } from "react";
import { v4 as uuidv4 } from "uuid";
import { toast } from "sonner";
import type { Message } from "@a2a-js/sdk";
import { renameSession, createSession } from "@/app/actions/sessions";
import { createMessage, ProcessedToolCallData, ProcessedToolResultData } from "@/lib/messageHandlers";
import { chunkText, cleanSessionTitle, toolResultText } from "@/lib/acp";
import type { AcpSessionInfo, ConnState, JsonRpcMessage, PermissionOption, SessionUpdate } from "@/types/acp";

// How long to wait for a resume (session/load) to complete before giving up. A
// session/load can hang indefinitely when the harness's shared actor was
// restored from a checkpoint without the requested session still in memory,
// which would otherwise pin the chat in "loading-session" ("resuming") forever.
// On timeout we abandon the resume and start a fresh ACP session so the chat
// stays usable. Generous so a normal transcript replay never trips it.
const RESUME_LOAD_TIMEOUT_MS = 20000;


export interface UseAcpHarnessChatOptions {
  /** Same-origin WebSocket path, e.g. /api/agentharnesses/kagent/my-claw/acp */
  acpPath: string;
  namespace: string;
  agentName: string;
  /** The kagent/DB session id this chat maps to. For harness chats this IS the
   * ACP session id: the id session/new returns is adopted as the kagent session
   * id, so a reopened chat session/loads it directly. Undefined for a brand-new
   * chat until the first message creates the session. */
  sessionId?: string;
  /** Callback when ACP sessions are updated from session/list. */
  onSessionsUpdate?: (sessions: AcpSessionInfo[]) => void;
  /** The session ID to load on mount (from sidebar click). */
  initialLoadSessionId?: string;
  /** Connect on mount and resume the actor's prior transcript (existing chats).
   * New chats pass false so they stay idle until the first message. */
  autoConnect?: boolean;
}

export interface UseAcpHarnessChatReturn {
  conn: ConnState;
  messages: Message[];
  streamingContent: string;
  /** Open the WebSocket / re-handshake (lazy connect on first message). */
  connect: () => void;
  /** Send a prompt; lazily connects + queues it when still idle. */
  sendMessage: (text: string) => void;
  /** Cancel the in-flight turn. */
  cancel: () => void;
}

export function useAcpHarnessChat({
  acpPath,
  namespace,
  agentName,
  sessionId,
  onSessionsUpdate,
  initialLoadSessionId,
  autoConnect,
}: UseAcpHarnessChatOptions): UseAcpHarnessChatReturn {
  // Connection is deferred until the user sends their first message (or opens a
  // past session) so loading the chat page never blocks on a cold actor
  // create+resume — the actor is only provisioned once the chat is actually used.
  const [conn, setConn] = useState<ConnState>("idle");
  const [messages, setMessages] = useState<Message[]>([]);
  const [streamingContent, setStreamingContent] = useState("");

  const wsRef = useRef<WebSocket | null>(null);
  const nextIdRef = useRef(1);
  const pendingRef = useRef(new Map<number | string, string>());
  const sessionIdRef = useRef<string | null>(null);
  // Set when we close the socket ourselves (unmount/navigation/reconnect) so
  // onclose doesn't surface a spurious "disconnected" toast.
  const intentionalCloseRef = useRef(false);
  const streamBufRef = useRef("");
  const streamKindRef = useRef<"agent" | "thought" | "user" | null>(null);
  const toolNamesRef = useRef(new Map<string, string>());
  const toolResultsSentRef = useRef(new Set<string>());
  const planMessageIdRef = useRef<string | null>(null);
  const authMethodsRef = useRef<string[]>([]);
  const authTriedRef = useRef(false);
  const authQueueRef = useRef<string[]>([]);
  // session/load: replay in progress (user_message_chunk only rendered then),
  // and the session id we asked to load (the load response carries no id).
  const replayingRef = useRef(false);
  const pendingLoadRef = useRef<string | null>(null);
  // A first message typed before the socket connected; sent automatically once
  // session/new completes after the lazy connect.
  const pendingPromptRef = useRef<string | null>(null);
  // On the first session/list after connect, decide whether to resume the
  // actor's existing conversation (session/load) or start fresh (session/new).
  const needResumeDecisionRef = useRef(false);
  const canListSessionsRef = useRef(false);
  // Last title persisted to the DB session name, to avoid redundant renames.
  const lastTitleRef = useRef<string>("");
  // The kagent DB session id, which for harness chats IS the ACP session id
  // (the id session/new returns is adopted as the kagent session id). Held in a
  // ref so handleMessage stays stable. For a brand-new chat the prop is
  // undefined until session/new creates the DB session; once we set it locally
  // we must not let the still-undefined prop clobber it, hence the guarded sync.
  const dbSessionIdRef = useRef<string | undefined>(sessionId);
  if (sessionId && dbSessionIdRef.current !== sessionId) {
    dbSessionIdRef.current = sessionId;
  }
  // Auto-reconnect bookkeeping. The harness's actor is shared and resumes on
  // demand, so an unexpected socket drop (e.g. a suspended actor, a brief proxy
  // blip) is recovered automatically with exponential backoff instead of asking
  // the user to click Reconnect. Attempts reset once we get a successful
  // initialize response (the actor is up and serving).
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Set right before we intentionally close the socket because the actor was
  // suspended, so onclose lands the chat in "idle" (ready to lazily reconnect on
  // the next message) instead of "disconnected" (an error state).
  const suspendResetRef = useRef(false);
  // Holds the latest connect() so the onclose handler can re-dial without
  // creating a circular useCallback dependency.
  const connectRef = useRef<() => void>(() => {});

  const appendMessage = useCallback((message: Message) => {
    setMessages((prev) => [...prev, message]);
  }, []);

  const flushStream = useCallback(() => {
    const buf = streamBufRef.current;
    const kind = streamKindRef.current;
    streamBufRef.current = "";
    streamKindRef.current = null;
    setStreamingContent("");
    if (buf.trim()) {
      const role = kind === "thought" ? "thinking" : kind === "user" ? "user" : "assistant";
      appendMessage(createMessage(buf, role, { originalType: "TextMessage" }));
    }
  }, [appendMessage]);

  const addStreamChunk = useCallback(
    (kind: "agent" | "thought" | "user", text: string) => {
      if (!text) return;
      if (streamKindRef.current !== null && streamKindRef.current !== kind) {
        flushStream();
      }
      streamKindRef.current = kind;
      streamBufRef.current += text;
      setStreamingContent(streamBufRef.current);
    },
    [flushStream],
  );

  const sendRaw = useCallback((msg: Record<string, unknown>) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify(msg));
  }, []);

  const rpc = useCallback(
    (method: string, params: Record<string, unknown>) => {
      const id = nextIdRef.current++;
      pendingRef.current.set(id, method);
      sendRaw({ jsonrpc: "2.0", id, method, params });
    },
    [sendRaw],
  );

  const handleSessionUpdate = useCallback(
    (update: SessionUpdate) => {
      switch (update.sessionUpdate) {
        case "agent_message_chunk":
          addStreamChunk("agent", chunkText(update.content));
          break;
        case "agent_thought_chunk":
          addStreamChunk("thought", chunkText(update.content));
          break;
        case "user_message_chunk":
          // Only rendered during session/load replay; live user messages are
          // already appended locally when the prompt is sent.
          if (replayingRef.current) {
            addStreamChunk("user", chunkText(update.content));
          }
          break;
        case "tool_call": {
          flushStream();
          const id = update.toolCallId || uuidv4();
          const name = update.title || update.kind || "tool";
          toolNamesRef.current.set(id, name);
          const toolCallData: ProcessedToolCallData[] = [{ id, name, args: update.rawInput ?? {} }];
          appendMessage(
            createMessage("", "assistant", {
              originalType: "ToolCallRequestEvent",
              additionalMetadata: { toolCallData },
            }),
          );
          break;
        }
        case "tool_call_update": {
          const id = update.toolCallId;
          if (!id) break;
          const status = update.status ?? "";
          if (status !== "completed" && status !== "failed") break;
          if (toolResultsSentRef.current.has(id)) break;
          toolResultsSentRef.current.add(id);
          const name = toolNamesRef.current.get(id) || update.title || "tool";
          const toolResultData: ProcessedToolResultData[] = [
            {
              call_id: id,
              name,
              content: toolResultText(update) || (status === "failed" ? "tool call failed" : "done"),
              is_error: status === "failed",
            },
          ];
          appendMessage(
            createMessage("", "assistant", {
              originalType: "ToolCallExecutionEvent",
              additionalMetadata: { toolResultData },
            }),
          );
          break;
        }
        case "plan": {
          const text = (update.entries ?? [])
            .map((e) => `${e.status === "completed" ? "✓" : e.status === "in_progress" ? "▸" : "○"} ${e.content ?? ""}`)
            .join("\n");
          if (!text) break;
          if (planMessageIdRef.current === null) {
            planMessageIdRef.current = uuidv4();
          }
          const planId = planMessageIdRef.current;
          const planMessage = createMessage(text, "plan", { messageId: planId, originalType: "TextMessage" });
          setMessages((prev) =>
            prev.some((m) => m.messageId === planId)
              ? prev.map((m) => (m.messageId === planId ? planMessage : m))
              : [...prev, planMessage],
          );
          break;
        }
        default:
          break;
      }
    },
    [addStreamChunk, appendMessage, flushStream],
  );

  const handleAgentRequest = useCallback(
    (msg: JsonRpcMessage) => {
      if (msg.method === "session/request_permission") {
        const params = msg.params as { options?: PermissionOption[]; toolCall?: { title?: string } } | undefined;
        const options = params?.options ?? [];
        const allow =
          options.find((o) => o.kind === "allow_once") ??
          options.find((o) => o.kind === "allow_always") ??
          options[0];
        if (allow?.optionId) {
          sendRaw({
            jsonrpc: "2.0",
            id: msg.id,
            result: { outcome: { outcome: "selected", optionId: allow.optionId } },
          });
        } else {
          sendRaw({ jsonrpc: "2.0", id: msg.id, result: { outcome: { outcome: "cancelled" } } });
        }
        return;
      }
      // fs/* and anything else: we advertised no client capabilities.
      sendRaw({
        jsonrpc: "2.0",
        id: msg.id,
        error: { code: -32601, message: `method not supported by this client: ${msg.method}` },
      });
    },
    [sendRaw],
  );

  const handleMessage = useCallback(
    (msg: JsonRpcMessage) => {
      // Response to one of our requests.
      if (msg.id !== undefined && msg.method === undefined) {
        const method = pendingRef.current.get(msg.id);
        pendingRef.current.delete(msg.id);
        if (msg.error) {
          // Some agents (e.g. codex-acp) refuse session/new until an explicit
          // authenticate call, even when credentials are already present as
          // env vars. Try each advertised auth method (API-key ones first)
          // until one succeeds.
          if (method === "session/new" && !authTriedRef.current && authMethodsRef.current.length > 0) {
            authTriedRef.current = true;
            const ids = authMethodsRef.current;
            authQueueRef.current = [
              ...ids.filter((id) => id.includes("api-key")),
              ...ids.filter((id) => !id.includes("api-key")),
            ];
            rpc("authenticate", { methodId: authQueueRef.current.shift() });
            return;
          }
          // An auth method can fail (e.g. its env var is unset); fall through
          // to the next one before giving up.
          if (method === "authenticate" && authQueueRef.current.length > 0) {
            rpc("authenticate", { methodId: authQueueRef.current.shift() });
            return;
          }
          // Listing sessions is best-effort. When it was the first list after
          // connect (the resume decision), the agent likely requires an
          // authenticate step before it will list (e.g. codex) — fall back to
          // creating a fresh session, letting session/new run its own
          // authenticate dance. Otherwise degrade silently to no picker.
          if (method === "session/list") {
            console.debug("acp: session/list failed", msg.error.message);
            if (needResumeDecisionRef.current) {
              needResumeDecisionRef.current = false;
              setConn("creating-session");
              rpc("session/new", { cwd: "/home/agent", mcpServers: [] });
            }
            return;
          }
          toast.error(`${method ?? "request"} failed: ${msg.error.message}`);
          if (method === "session/prompt") {
            flushStream();
            setConn("ready");
          } else if (method === "session/load") {
            replayingRef.current = false;
            pendingLoadRef.current = null;
            flushStream();
            // Keep the previous session usable; only the load failed.
            setConn("ready");
          } else if (method === "initialize" || method === "session/new" || method === "authenticate") {
            wsRef.current?.close();
          }
          return;
        }
        if (method === "initialize") {
          const methods = (msg.result?.authMethods as { id?: string }[] | undefined) ?? [];
          authMethodsRef.current = methods.map((m) => m.id).filter((id): id is string => typeof id === "string");
          // The actor answered: it's up and serving, so clear the reconnect
          // backoff. Any later drop starts counting fresh from attempt 0.
          reconnectAttemptsRef.current = 0;
          // Previous chats: remember whether the agent supports session
          // listing + loading so we can resume rather than always start fresh.
          const caps = (msg.result?.agentCapabilities ?? {}) as {
            loadSession?: boolean;
            sessionCapabilities?: { list?: unknown };
          };
          canListSessionsRef.current = caps.loadSession === true && caps.sessionCapabilities?.list !== undefined;
          // Always ask the actor for its sessions first (session/list) so we
          // resume using the id the actor actually reports. session/new hands
          // back a bare id (which we adopt as the kagent session id) while
          // session/list returns namespaced ids (e.g. "agent:main:acp:<id>");
          // loading the bare id directly silently fails to replay. The list
          // response (needResumeDecision) matches our session id by suffix and
          // session/load's the actor's full id, or starts fresh when this chat
          // has no session yet.
          if (canListSessionsRef.current) {
            needResumeDecisionRef.current = true;
            setConn("loading-session");
            rpc("session/list", {});
          } else {
            // Agent can't list/load: start a new ACP session. Its id becomes the
            // kagent session id once session/new returns so reopening resumes it.
            setConn("creating-session");
            rpc("session/new", { cwd: "/home/agent", mcpServers: [] });
          }
        } else if (method === "authenticate") {
          authQueueRef.current = [];
          rpc("session/new", { cwd: "/home/agent", mcpServers: [] });
        } else if (method === "session/list") {
          const sessions = (msg.result?.sessions as AcpSessionInfo[] | undefined) ?? [];
          const sorted = sessions
            .filter((s) => typeof s.sessionId === "string" && s.sessionId.length > 0)
            .sort((a, b) => (b.updatedAt ?? "").localeCompare(a.updatedAt ?? ""));
          onSessionsUpdate?.(sorted);
          // Emit event for sidebar to pick up
          if (typeof window !== "undefined") {
            window.dispatchEvent(
              new CustomEvent("acp-sessions-updated", {
                detail: { agentRef: `${namespace}/${agentName}`, sessions: sorted },
              })
            );
          }
          // First list after connect: resume this chat's bound ACP session
          // using the id the actor actually reports. session/list ids are
          // namespaced (e.g. "agent:main:acp:<id>") while the bound id we
          // persisted from session/new is bare, so match by suffix in either
          // direction. A chat with no bound session (brand new) or whose bound
          // session is gone starts fresh.
          if (needResumeDecisionRef.current) {
            needResumeDecisionRef.current = false;
            const boundId = dbSessionIdRef.current;
            const resume = boundId
              ? sorted.find(
                  (s) =>
                    s.sessionId === boundId ||
                    s.sessionId.endsWith(boundId) ||
                    boundId.endsWith(s.sessionId),
                )
              : undefined;
            if (resume) {
              const resumeId = resume.sessionId;
              flushStream();
              setMessages([]);
              toolNamesRef.current.clear();
              toolResultsSentRef.current.clear();
              planMessageIdRef.current = null;
              replayingRef.current = true;
              pendingLoadRef.current = resumeId;
              setConn("loading-session");
              rpc("session/load", { sessionId: resumeId, cwd: "/home/agent", mcpServers: [] });
              return;
            }
            setConn("creating-session");
            rpc("session/new", { cwd: "/home/agent", mcpServers: [] });
            return;
          }
          // Adopt the agent-generated title as this chat's DB session name so the
          // sidebar shows a meaningful label instead of "Untitled".
          const dbSessionId = dbSessionIdRef.current;
          const currentAcpId = sessionIdRef.current;
          // session/list returns prefixed ids (e.g. "agent:main:acp:<id>") while
          // session/new hands back the bare id, so match by suffix; each actor
          // holds a single chat, so fall back to the only session if present.
          const match =
            sorted.find(
              (s) => currentAcpId && (s.sessionId === currentAcpId || s.sessionId.endsWith(currentAcpId)),
            ) ?? (sorted.length === 1 ? sorted[0] : undefined);
          const title = match?.title ? cleanSessionTitle(match.title) : "";
          if (dbSessionId && title && title !== lastTitleRef.current) {
            lastTitleRef.current = title;
            void renameSession(dbSessionId, title);
            if (typeof window !== "undefined") {
              window.dispatchEvent(
                new CustomEvent("harness-session-titled", {
                  detail: { sessionId: dbSessionId, title },
                })
              );
            }
          }
        } else if (method === "session/load") {
          replayingRef.current = false;
          flushStream();
          if (pendingLoadRef.current) {
            sessionIdRef.current = pendingLoadRef.current;
          }
          pendingLoadRef.current = null;
          setConn("ready");
          // A message typed before the transcript finished replaying is sent now.
          const pendingAfterLoad = pendingPromptRef.current;
          const loadedSid = sessionIdRef.current;
          if (pendingAfterLoad && loadedSid) {
            pendingPromptRef.current = null;
            toolResultsSentRef.current.clear();
            setConn("running");
            rpc("session/prompt", { sessionId: loadedSid, prompt: [{ type: "text", text: pendingAfterLoad }] });
          }
        } else if (method === "session/new") {
          const sid = msg.result?.sessionId as string | undefined;
          if (!sid) {
            toast.error("session/new returned no sessionId");
            wsRef.current?.close();
            return;
          }
          sessionIdRef.current = sid;
          setConn("ready");
          // Brand-new chat: the ACP session id session/new just returned becomes
          // this chat's kagent session id. Create the DB session now (actor-first
          // ordering) so the kagent record and the ACP session share one id with
          // no separate binding to persist. Reopened chats already have an id.
          if (!dbSessionIdRef.current) {
            dbSessionIdRef.current = sid;
            const agentRef = `${namespace}/${agentName}`;
            void createSession({ id: sid, agent_ref: agentRef }).then((res) => {
              if (res.error || !res.data || typeof window === "undefined") return;
              // Swap the URL to the real chat id without remounting, and surface
              // the new chat in the sidebar.
              window.history.replaceState(
                {},
                "",
                `/agents/${encodeURIComponent(namespace)}/${encodeURIComponent(agentName)}/chat/${encodeURIComponent(sid)}`,
              );
              window.dispatchEvent(
                new CustomEvent("new-session-created", {
                  detail: { agentRef, session: res.data },
                }),
              );
            });
          }
          // Fetch previous chats now that any required authenticate has run.
          if (canListSessionsRef.current) {
            rpc("session/list", {});
          }
          // If the user typed their first message before we lazily connected,
          // send it now that the session is live.
          const pending = pendingPromptRef.current;
          if (pending) {
            pendingPromptRef.current = null;
            toolResultsSentRef.current.clear();
            setConn("running");
            rpc("session/prompt", { sessionId: sid, prompt: [{ type: "text", text: pending }] });
          }
        } else if (method === "session/prompt") {
          const stop = msg.result?.stopReason as string | undefined;
          flushStream();
          planMessageIdRef.current = null;
          setConn("ready");
          if (stop && stop !== "end_turn") {
            toast.info(`Turn ended: ${stop}`);
          }
          // Pick up the current session and freshly generated titles.
          if (canListSessionsRef.current) {
            rpc("session/list", {});
          }
        }
        return;
      }
      // Agent-initiated request.
      if (msg.id !== undefined && msg.method !== undefined) {
        handleAgentRequest(msg);
        return;
      }
      // Notification.
      if (msg.method === "session/update") {
        const update = (msg.params as { update?: SessionUpdate } | undefined)?.update;
        if (update) handleSessionUpdate(update);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [flushStream, handleAgentRequest, handleSessionUpdate, rpc],
  );

  const connect = useCallback(() => {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${window.location.host}${acpPath}`;
    intentionalCloseRef.current = false;
    // Cancel any scheduled auto-reconnect; we're dialing now.
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    setConn("connecting");
    const ws = new WebSocket(url);
    wsRef.current = ws;
    ws.onopen = () => {
      authMethodsRef.current = [];
      authTriedRef.current = false;
      authQueueRef.current = [];
      replayingRef.current = false;
      pendingLoadRef.current = null;
      needResumeDecisionRef.current = false;
      setConn("initializing");
      rpc("initialize", {
        protocolVersion: 1,
        clientCapabilities: { fs: { readTextFile: false, writeTextFile: false } },
      });
    };
    ws.onmessage = (ev) => {
      try {
        handleMessage(JSON.parse(String(ev.data)) as JsonRpcMessage);
      } catch {
        console.debug("acp: received non-JSON frame", ev.data);
      }
    };
    ws.onclose = (ev) => {
      flushStream();
      sessionIdRef.current = null;
      pendingRef.current.clear();
      pendingPromptRef.current = null;
      // A normal close (1000/1001) or one we triggered ourselves
      // (navigation/unmount/reconnect) is final and silent.
      if (intentionalCloseRef.current || ev.code === 1000 || ev.code === 1001) {
        // A suspend tear-down resets to idle so the next message lazily
        // reconnects (resuming the actor) instead of being blocked by the
        // "disconnected" error state.
        if (suspendResetRef.current) {
          suspendResetRef.current = false;
          setConn("idle");
          return;
        }
        setConn("disconnected");
        return;
      }
      // Unexpected drop: the harness's shared actor resumes on demand, so
      // recover automatically with exponential backoff rather than surfacing a
      // Reconnect button. Keep the status as "connecting" so the composer shows
      // a reconnecting state instead of an error. Give up only after several
      // failed attempts (the actor likely can't start).
      const MAX_RECONNECT_ATTEMPTS = 6;
      const attempt = reconnectAttemptsRef.current;
      if (attempt >= MAX_RECONNECT_ATTEMPTS) {
        setConn("disconnected");
        toast.error("Lost connection to the agent and couldn't reconnect. Reload the page to try again.");
        return;
      }
      reconnectAttemptsRef.current = attempt + 1;
      const delay = Math.min(8000, 500 * 2 ** attempt);
      setConn("connecting");
      reconnectTimerRef.current = setTimeout(() => {
        reconnectTimerRef.current = null;
        connectRef.current();
      }, delay);
    };
  }, [acpPath, flushStream, handleMessage, rpc]);

  // Keep the latest connect() reachable from onclose's reconnect scheduler.
  connectRef.current = connect;

  const loadSession = useCallback(
    (sid: string) => {
      if (conn !== "ready" || !sid || sid === sessionIdRef.current) return;
      // Clear the transcript; the agent replays the conversation as
      // session/update notifications before answering session/load.
      flushStream();
      setMessages([]);
      toolNamesRef.current.clear();
      toolResultsSentRef.current.clear();
      planMessageIdRef.current = null;
      replayingRef.current = true;
      pendingLoadRef.current = sid;
      setConn("loading-session");
      rpc("session/load", { sessionId: sid, cwd: "/home/agent", mcpServers: [] });
    },
    [conn, flushStream, rpc],
  );

  const sendMessage = useCallback(
    (text: string) => {
      if (!text) return;
      // No live, ready session yet — a fresh chat, or the actor was suspended
      // and its socket torn down. Provision/resume the actor by connecting,
      // queue the prompt, and let session/new or session/load deliver it once
      // the actor is live again, instead of firing into a frozen actor.
      if (conn === "idle" || conn === "disconnected") {
        appendMessage(createMessage(text, "user", { originalType: "TextMessage" }));
        pendingPromptRef.current = text;
        connect();
        return;
      }
      const sid = sessionIdRef.current;
      if (!sid || conn !== "ready") return;
      appendMessage(createMessage(text, "user", { originalType: "TextMessage" }));
      toolResultsSentRef.current.clear();
      setConn("running");
      rpc("session/prompt", { sessionId: sid, prompt: [{ type: "text", text }] });
    },
    [conn, appendMessage, connect, rpc],
  );

  const cancel = useCallback(() => {
    const sid = sessionIdRef.current;
    if (!sid) return;
    sendRaw({ jsonrpc: "2.0", method: "session/cancel", params: { sessionId: sid } });
  }, [sendRaw]);

  // Connect lazily: auto-connect on mount for existing chats (autoConnect) or
  // when a specific past session was requested, resuming the actor's prior
  // transcript. A fresh chat stays idle until the user sends their first
  // message, so the page never blocks on a cold actor.
  useEffect(() => {
    if (autoConnect || initialLoadSessionId) connect();
    return () => {
      intentionalCloseRef.current = true;
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      wsRef.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [acpPath]);

  // If a session ID is passed (e.g., from sidebar click), load it once ready.
  useEffect(() => {
    if (initialLoadSessionId && conn === "ready") {
      loadSession(initialLoadSessionId);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialLoadSessionId, conn]);

  // Resume watchdog: a session/load that never answers (e.g. the actor was
  // restored without the session) would leave the chat stuck in
  // "loading-session" ("resuming") forever. If a load doesn't complete in time,
  // abandon the resume and start a fresh ACP session so the chat stays usable
  // instead of being permanently stuck. A successful/failed load flips conn off
  // "loading-session", which clears this timer before it fires.
  useEffect(() => {
    if (conn !== "loading-session") return;
    const timer = setTimeout(() => {
      if (wsRef.current?.readyState !== WebSocket.OPEN) return;
      replayingRef.current = false;
      pendingLoadRef.current = null;
      flushStream();
      setMessages([]);
      toolNamesRef.current.clear();
      toolResultsSentRef.current.clear();
      planMessageIdRef.current = null;
      setConn("creating-session");
      rpc("session/new", { cwd: "/home/agent", mcpServers: [] });
    }, RESUME_LOAD_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [conn, flushStream, rpc]);

  // React to manual Suspend/Resume of the harness's shared actor (from the
  // right-sidebar menu). Suspending freezes the actor while the proxied socket
  // stays half-open, so we tear it down and drop to idle; the next message then
  // lazily reconnects (resuming the actor) and is delivered instead of being
  // sent into a frozen actor. Resuming brings the chat back online immediately.
  useEffect(() => {
    const onActorStateChange = (e: Event) => {
      const detail = (e as CustomEvent).detail as { state?: string; sessionId?: string } | undefined;
      if (detail?.sessionId && detail.sessionId !== dbSessionIdRef.current) return;
      if (detail?.state === "suspended") {
        intentionalCloseRef.current = true;
        suspendResetRef.current = true;
        if (reconnectTimerRef.current) {
          clearTimeout(reconnectTimerRef.current);
          reconnectTimerRef.current = null;
        }
        const ws = wsRef.current;
        if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
          ws.close();
        } else {
          // No socket to close; drop straight to idle so the next send reconnects.
          suspendResetRef.current = false;
          setConn("idle");
        }
      } else if (detail?.state === "running") {
        const ws = wsRef.current;
        if (!ws || (ws.readyState !== WebSocket.OPEN && ws.readyState !== WebSocket.CONNECTING)) {
          connectRef.current();
        }
      }
    };
    window.addEventListener("harness-session-suspended", onActorStateChange);
    return () => window.removeEventListener("harness-session-suspended", onActorStateChange);
  }, []);

  return { conn, messages, streamingContent, connect, sendMessage, cancel };
}
