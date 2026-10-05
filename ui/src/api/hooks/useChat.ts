/**
 * Conversation state for one agent instance.
 *
 * The transport is reached through the `ChatClient` port, so this hook — and
 * everything that renders from it — is unchanged when the real A2A client
 * replaces the mock, or when A2A's own version moves under it. That port is what
 * absorbed chat moving from REST to gRPC-Web: nothing here changed for it except
 * the address, which is now an instance rather than a session.
 *
 * Everything here is stamped with the conversation it belongs to and then
 * *derived* for the one currently being viewed, rather than cleared by an effect
 * when that changes. Clearing after the fact means a render happens first, so
 * switching conversations would flash the previous one's messages under the new
 * one's heading — and an in-flight turn could land in the wrong transcript.
 *
 * ## The reader's own message is this hook's to show
 *
 * It used to wait to be told. `run()` set the turn going and then reacted only to
 * events from the server, so the reader's words appeared only if something echoed
 * them back — and the A2A gateway does not. Measured against the cluster on
 * 2026-08-24: a completed turn emits a `WORKING` frame and a `COMPLETED` frame and
 * no message frame at all, while `ListTasks` afterwards holds the user's message
 * and nothing else. So the words existed on the server and never on the page,
 * until a reload read them back out of history. That is exactly the reported
 * symptom, and it is why the message is appended here, at the moment of sending.
 *
 * It is appended under an id this hook mints and hands to the transport, so
 * whatever comes back for it — an echo from a gateway that does echo, or the same
 * message read out of history later — carries that id and *replaces* it rather
 * than arriving beside it. Optimism without a stable key is how a transcript ends
 * up showing everything twice.
 *
 * ## The turn is a state machine, and it lives next door
 *
 * See `api/chat/turnMachine.ts`. What matters here is that this file no longer
 * assigns a turn state from four different branches of an event loop, which is how
 * a cancelled turn could be overwritten by a `working` frame that was already in
 * flight when the reader pressed stop.
 */

import { ApiError } from "../ApiError";
import { useLiveTranscript } from "./useLiveTranscript";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getChatClient } from "../chat";
import { runtimeConfig } from "../runtimeConfig";
import { randomId } from "../randomId";
import { conversationKey } from "../chat/types";
import {
  IDLE_TURN,
  isActive,
  nextTurn,
  turnStateOf,
  type ChatTurnPhase,
  type TurnEvent,
  type TurnState,
} from "../chat/turnMachine";
import {
  askUserAnswer,
  answerText,
  toolApprovalAnswer,
  toolApprovalText,
  type PendingRequest,
  type ToolApprovalDecision,
} from "../chat/hitl";
import type {
  ChatHistory,
  ChatConversationRef,
  ChatMessage,
  ChatPart,
  ChatTurnState,
} from "../chat/types";

/** What the conversation is doing, from the UI's point of view. */
export type ChatPhase = "idle" | "streaming";

export interface ChatController {
  messages: ChatMessage[];
  /** First load of the transcript, before anything is on screen. */
  isLoadingHistory: boolean;
  /** The transcript could not be loaded at all. */
  historyError?: Error;
  /**
   * Reads persisted messages and task state without interrupting a local stream.
   * Stale reads are discarded after a send, cancellation or navigation.
   */
  refreshTranscript: () => Promise<void>;
  /** The last turn failed. Cleared when a new turn starts. */
  turnError?: Error;
  /** Lifecycle of the turn in flight, or of the last one to finish. */
  turnState: ChatTurnState | "idle";
  /**
   * The same turn in the UI's own vocabulary, which draws two distinctions A2A
   * does not: waiting for a first frame is not the same as watching an answer
   * arrive, and the difference between them is a page that looks stalled and a
   * page that looks alive.
   */
  turnPhase: ChatTurnPhase;
  phase: ChatPhase;
  /**
   * The turn the agent parked to ask the reader something, if there is one.
   *
   * Not a turn state: it outlives the turn that created it and survives a reload,
   * because the *task* is what stays non-terminal. While it is set the controller
   * refuses every message, so a page that does not show it leaves the reader
   * turned away for a reason nothing on screen explains.
   */
  pendingQuestion?: PendingRequest;
  /** False means admission was refused; keep the draft and never replay it. */
  send: (text: string) => Promise<boolean>;
  /** Admission is being checked, or could not yet be confirmed. */
  isCheckingTask: boolean;
  sessionNotice?: string;
  cancel: () => Promise<void>;
  /**
   * Gives up a pending question without answering it.
   *
   * Not the only way out any more — `answerQuestion` below is the other, and is
   * the one a reader normally wants. This stays because abandoning is a real
   * intention: the question may no longer be worth answering, and until one of
   * the two happens the conversation accepts nothing else.
   */
  dismissQuestion: () => Promise<void>;
  /**
   * Answers the question the conversation is holding.
   *
   * One entry per question, in the order they were asked — the runtime pairs them
   * positionally. Each is an array because a question may accept several choices;
   * one that did not still gets an array of one.
   */
  answerQuestion: (answers: readonly string[][]) => Promise<void>;
  /** Approves or rejects every tool invocation in the pending request. */
  answerToolApproval: (decisions: readonly ToolApprovalDecision[]) => Promise<void>;
  /** Re-sends the message whose turn failed. */
  retry: () => Promise<boolean>;
  canRetry: boolean;
}

/**
 * The loaded transcript, tagged with whose it is.
 *
 * Tagged by the conversation's *key* rather than by the ref object, because the
 * ref is rebuilt on every render from route params — two structurally equal refs
 * are different objects, and comparing them by identity would discard the
 * transcript on every render.
 */
type Transcript =
  | { key: string; messages: ChatMessage[] }
  | { key: string; error: Error };

const NO_MESSAGES: ChatMessage[] = [];

export function useChat(
  conversation: ChatConversationRef | undefined,
  /**
   * Run before every turn, and awaited.
   *
   * The one thing a surface may need to do that this hook cannot know about: the
   * conversation may be suspended, and the gateway refuses a message for an instance
   * that is not ready. Resuming is the page's business — it holds the instance read —
   * but *when* to do it is this hook's, because there are two ways a turn begins.
   *
   * A page that wrapped `send` alone would miss `retry`, which starts a turn from in
   * here and never touches the page's wrapper. That is not hypothetical: with
   * conversations giving their workers back after every turn, the state a failed turn
   * leaves behind is suspended, so retry is precisely the button most likely to be
   * pressed against one.
   */
  beforeSend?: () => Promise<void>,
): ChatController {
  // The key, not the ref, is what everything below is stamped with and what every
  // dependency list watches — see `Transcript`.
  const key = conversation ? conversationKey(conversation) : undefined;

  const [transcript, setTranscript] = useState<Transcript | null>(null);
  const [turn, setTurn] = useState<TurnState>(IDLE_TURN);
  /*
   * The question the conversation is holding, tagged with whose it is.
   *
   * Separate from the turn because it is not one: the turn that asked has ended,
   * and what remains is a task the controller will not let anything past. It is
   * learned from the history read, from a turn that parks while the page watches,
   * and from a refused send — three routes to the same fact, because a reader can
   * arrive at a parked conversation by any of them.
   */
  const [pending, setPending] = useState<{ key: string; request: PendingRequest } | null>(
    null,
  );
  const [isDismissing, setDismissing] = useState(false);
  // Read from a click handler, outside any render — the same reason `taskRef` is a
  // ref rather than the machine's own copy of the task id.
  const pendingRef = useRef<{ key: string; request: PendingRequest } | null>(pending);
  useEffect(() => {
    pendingRef.current = pending;
  });

  // Kept current every render for the reason given where it is called.
  const beforeSendRef = useRef(beforeSend);
  useEffect(() => {
    beforeSendRef.current = beforeSend;
  });

  // Refs, not state: cancelling and cleanup read these outside a render, and a
  // stale closure over them would abort the wrong turn.
  const abortRef = useRef<AbortController | null>(null);
  const [lastAttempt, setLastAttempt] = useState<{ key: string; text: string; taskId?: string } | null>(null);
  /*
   * The task the turn in flight is filed under, for cancelling it.
   *
   * A ref and not part of the machine's state, even though the machine carries it
   * too: `cancel` runs from a click handler, outside any render, and reading it
   * from state there would give it whatever the last render happened to see. The
   * machine's copy is what the UI renders from; this is what the RPC is addressed
   * with, and both are written from the same event.
   */
  const taskRef = useRef<string | undefined>(undefined);

  const [observed, setObserved] = useState<{ key: string; history: ChatHistory } | null>(null);
  const [checking, setChecking] = useState<{ key: string; error?: Error } | null>(null);
  const [notice, setNotice] = useState<{ key: string; text: string } | null>(null);
  const [localStream, setLocalStream] = useState<string>();
  const keyRef = useRef(key);
  // Invalidate reads that began before a send, cancellation or navigation. A slow
  // poll must never restore an old task over a newer local turn.
  const revision = useRef(0);
  const cancelling = useRef<AbortController | null>(null);

  const applyHistory = useCallback((history: ChatHistory) => {
    if (!key || keyRef.current !== key) return;
    setTranscript((current) => mergeHistory(current, key, history.messages));
    setObserved({ key, history });
    const parked = history.awaitingReply ? { key, request: history.awaitingReply } : null;
    pendingRef.current = parked;
    setPending(parked);
    setChecking(null);
    if (!history.turn || !["submitted", "working"].includes(history.turn.state)) setNotice(null);
    taskRef.current = history.turn?.taskId;
  }, [key]);

  const refreshTranscript = useCallback(async () => {
    if (!conversation || !key || abortRef.current) return;
    const version = ++revision.current;
    try {
      const history = await getChatClient().history(conversation);
      if (keyRef.current !== key || revision.current !== version || abortRef.current) return;
      applyHistory(history);
    } catch (cause: unknown) {
      if (keyRef.current !== key || revision.current !== version || abortRef.current) return;
      // Keep known activity and history visible, but do not admit another turn
      // until the next poll confirms the server's state.
      setChecking({ key, error: asError(cause) });
    }
    // The address is keyed independently of the ref object.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, applyHistory]);

  useEffect(() => {
    keyRef.current = key;
    if (!conversation || !key) return;
    revision.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
    const controller = new AbortController();
    const version = revision.current;
    getChatClient().history(conversation, { signal: controller.signal })
      .then((history) => {
        if (!controller.signal.aborted && revision.current === version) {
          setLocalStream(undefined);
          applyHistory(history);
        }
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted && revision.current === version) {
          setTranscript({ key, error: asError(cause) });
          setChecking({ key, error: asError(cause) });
        }
      });
    return () => {
      controller.abort();
      keyRef.current = undefined;
      revision.current += 1;
      abortRef.current?.abort();
      abortRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, applyHistory]);

  // Poll persisted A2A state while observing a remote turn, including in a second
  // tab. Only our own live stream pauses polling; "working" on reload does not.
  useLiveTranscript(refreshTranscript, { enabled: Boolean(key), isBusy: Boolean(key && localStream === key) });

  const run = useCallback(
    async (text: string, hitl?: Record<string, unknown>, displayParts?: ChatPart[]) => {
      if (!conversation || !key || !text.trim() || abortRef.current) return false;

      // Lock synchronously before the first await, including preflight. Two clicks
      // in one render must not start two submissions.
      const controller = new AbortController();
      abortRef.current = controller;
      const sendRevision = ++revision.current;
      setLocalStream(key);
      setNotice(null);
      const previousQuestion = pendingRef.current?.key === key ? pendingRef.current : null;
      let admitted = false;
      let fresh: ChatHistory;
      try {
        fresh = await getChatClient().history(conversation, { signal: controller.signal });
        if (controller.signal.aborted || keyRef.current !== key) return false;
        applyHistory(fresh);
        const busy = fresh.turn && ["submitted", "working"].includes(fresh.turn.state);
        const staleAnswer = JSON.stringify(previousQuestion?.request) !== JSON.stringify(fresh.awaitingReply);
        if (busy || staleAnswer) {
          setNotice({ key, text: busy
            ? "Session busy. Following the active turn; your message has not been sent."
            : "The pending question changed. Review the conversation before sending your answer." });
          return false;
        }
        if (beforeSendRef.current) await beforeSendRef.current();
        if (controller.signal.aborted || keyRef.current !== key) return false;
        admitted = true;
      } catch (cause: unknown) {
        if (!controller.signal.aborted && keyRef.current === key) setChecking({ key, error: asError(cause) });
        return false;
      } finally {
        // Once admitted below the same controller will own the stream.
        if (!admitted) {
          if (abortRef.current === controller) abortRef.current = null;
          setLocalStream((current) => current === key ? undefined : current);
        }
      }

      setObserved({ key, history: { ...fresh, turn: undefined } });
      setLastAttempt({ key, text });

      /*
       * Every event goes through the machine, and only through the machine.
       *
       * Guarded on the key so a turn that outlives a conversation switch cannot
       * write progress into the transcript the reader is now looking at. The
       * guard is inside the dispatcher rather than at each call site because
       * there are six of them and one missing check is a bug nobody sees.
       */
      const dispatch = (event: TurnEvent) => {
        if (revision.current !== sendRevision || keyRef.current !== key) return;
        setTurn((current) => current.key === key ? nextTurn(current, event) : current);
      };

      // Minted here so the transport can send it and the optimistic message can be
      // filed under it — see this file's header for why that matters.
      const messageId = randomId();

      /*
       * The question this message answers, if the conversation is holding one.
       *
       * Cleared optimistically: a turn that resumes is no longer parked, and leaving
       * the notice up while the agent works on the answer would tell the reader they
       * still owe one. If the send is refused the question is still there, and the
       * refusal path below reads it back rather than guessing.
       */
      const answering = pendingRef.current?.key === key ? pendingRef.current : null;
      if (answering) setPending(null);

      // From idle rather than from whatever the last turn ended as: `start` is the
      // machine's only reset, and beginning it anywhere else would carry the
      // previous turn's error into the retry that was meant to clear it.
      taskRef.current = undefined;
      setTurn(nextTurn(IDLE_TURN, { type: "start", key }));
      setTranscript((current) =>
        appendLocal(current, key, {
          id: messageId,
          role: "user",
          // HITL decisions still carry fallback prose on the wire, but the transcript
          // renders the structured decision instead of showing protocol text as if it
          // were a new chat message.
          parts: displayParts ?? [{ kind: "text", text }],
          createdAt: new Date().toISOString(),
        }),
      );

      // The deployment configures how long a stream may go QUIET, not how long
      // an answer may take, so the timer resets on every event rather than
      // capping the turn's total duration. Without it a stream that stops
      // emitting leaves the turn spinning forever.
      /*
       * Whether the turn failed without the server ever taking the message.
       *
       * A message refused outright — the commonest reason being a question already
       * pending — was never accepted by anything, so leaving the reader's optimistic
       * copy on screen would claim a turn that does not exist and vanish on the next
       * reload. It is taken back off, and the failure says why. A turn that fails
       * *after* starting is a different thing and keeps its message: it happened.
       */
      let refused = false;
      let terminalSeen = false;
      let conflict = false;
      let accepted = false;

      const { streamTimeoutMs } = runtimeConfig();
      let idleTimer: ReturnType<typeof setTimeout> | undefined;
      let timedOut = false;
      const awaitNextEvent = () => {
        clearTimeout(idleTimer);
        idleTimer = setTimeout(() => {
          timedOut = true;
          controller.abort();
        }, streamTimeoutMs);
      };

      try {
        awaitNextEvent();

        for await (const event of getChatClient().send({
          conversation,
          text,
          messageId,
          // A conversation holding a question is answered by naming its turn. Read
          // from the ref rather than from the render's closure because `run` is
          // rebuilt only when the address changes, so the value captured when the
          // handler was made is older than the question.
          taskId: answering?.request.taskId,
          hitl,
          signal: controller.signal,
        })) {
          if (controller.signal.aborted) break;
          awaitNextEvent();

          switch (event.type) {
            case "message":
              setTranscript((current) =>
                withMessages(current, key, (messages) =>
                  upsert(messages, event.message),
                ),
              );
              dispatch({ type: "content" });
              break;
            case "delta":
              setTranscript((current) =>
                withMessages(current, key, (messages) =>
                  appendDelta(messages, event.messageId, event.text),
                ),
              );
              dispatch({ type: "content" });
              break;
            case "status":
              terminalSeen = !["submitted", "working"].includes(event.state);
              if (event.taskId) {
                taskRef.current = event.taskId;
                accepted = true;
                const taskId = event.taskId;
                setLastAttempt((current) => current?.key === key ? { ...current, taskId } : current);
              }
              // A turn that parks leaves a question behind it. Recorded here rather
              // than derived from the turn's phase, because the turn ends and the
              // question does not.
              if (event.state === "input_required" && event.taskId) {
                const askedTaskId = event.taskId;
                const asked = event.awaiting ?? {
                  kind: "unknown" as const,
                  taskId: askedTaskId,
                };
                setPending({ key, request: asked });
              }
              dispatch({ type: "status", state: event.state, taskId: event.taskId });
              break;
            case "error":
              conflict = !accepted && event.error instanceof ApiError && event.error.status === 409;
              if (!conflict) dispatch({ type: "error", error: event.error });
              refused = true;
              break;
          }
        }
      } catch (cause: unknown) {
        // A transport that throws rather than yielding an error event is still
        // a failed turn, not a crashed page.
        if (!controller.signal.aborted) {
          conflict = !accepted && cause instanceof ApiError && cause.status === 409;
          if (!conflict) dispatch({ type: "error", error: asError(cause) });
          refused = true;
        }
      } finally {
        clearTimeout(idleTimer);
        // Reported here rather than in the catch because aborting breaks the
        // loop without throwing, and a timeout is a failed turn either way —
        // distinct from a user-initiated stop, which is not a failure.
        if (timedOut) {
          dispatch({
            type: "timeout",
            error: new Error(
              `The agent stopped responding after ${Math.round(streamTimeoutMs / 1000)}s.`,
            ),
          });
        }
        const ownsStream = abortRef.current === controller;
        if (ownsStream) abortRef.current = null;
        // Nothing was ever filed under this message, so it is taken back off screen
        // — see `refused`. `taskRef` is the test for "the server started a turn",
        // because the first status frame is what names the task.
        if (refused && !taskRef.current) {
          setTranscript((current) =>
            withMessages(current, key, (messages) =>
              messages.filter((existing) => existing.id !== messageId),
            ),
          );
          // A refusal usually *is* a question still standing, and this turn may
          // have cleared the notice on its way in. Put back what the server says
          // rather than what this render assumed.
          if (answering) setPending(answering);
        }
        // Settle the local transport. Without a terminal frame, persisted task
        // state determines whether the session remains busy.
        dispatch({ type: "settle" });
        if (ownsStream && keyRef.current === key && cancelling.current !== controller) {
          if (!terminalSeen || conflict) {
            // Losing the stream does not cancel the backend task. Hold admission
            // closed until its persisted state is known, then poll it to completion.
            setChecking({ key });
            if (conflict) setNotice({ key, text: "Session busy. Following the active turn; your message has not been sent." });
            await refreshTranscript();
          }
          setLocalStream((current) => current === key ? undefined : current);
        }
      }
      return !conflict && (!refused || accepted);
    },
    // Keyed on the address, not the ref object — the ref is rebuilt every render,
    // and depending on it would rebuild `run` (and so the composer's handler) each
    // time. `conversation` is read through that same render's closure, which is
    // correct because a change of address changes the key with it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, applyHistory, refreshTranscript],
  );

  const cancel = useCallback(async () => {
    if (!conversation || !key) return;
    const controller = abortRef.current;
    cancelling.current = controller;
    controller?.abort();
    abortRef.current = null;
    revision.current += 1;
    setChecking({ key });
    try {
      const taskId = taskRef.current;
      if (taskId) await getChatClient().cancel(conversation, taskId);
      if (keyRef.current !== key) return;
      setTurn((current) => current.key === key ? nextTurn(current, { type: "cancel" }) : current);
      await refreshTranscript();
    } catch (cause: unknown) {
      if (keyRef.current === key) setChecking({ key, error: asError(cause) });
    } finally {
      setLocalStream((current) => current === key ? undefined : current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, refreshTranscript]);

  /**
   * Gives up the question the conversation is holding.
   *
   * Unlike `cancel`, this is not best-effort: it is the *only* way out of a parked
   * turn, so a failure has to be reported rather than swallowed. The controller
   * makes that safe — `CancelTask` records the cancellation locally even when the
   * runtime has no record of the task, precisely so a conversation holding a
   * question can always be recovered — and the state is re-read afterwards rather
   * than assumed, because assuming it worked is how a page comes to offer a
   * composer that is still refused.
   */
  const dismissQuestion = useCallback(async () => {
    const parked = pendingRef.current;
    if (!conversation || !key || parked?.key !== key) return;

    setDismissing(true);
    try {
      await getChatClient().cancel(conversation, parked.request.taskId);
      const history = await getChatClient().history(conversation);
      applyHistory(history);
      setTurn((current) => (current.key === key ? IDLE_TURN : current));
    } catch (cause: unknown) {
      setTurn({ key, phase: "failed", error: asError(cause) });
    } finally {
      setDismissing(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, applyHistory]);

  const mine = transcript?.key === key ? transcript : null;
  const serverTurn = observed && observed.key === key ? observed.history.turn : undefined;
  const activeTurn = useMemo(() => localStream !== key && serverTurn && key
    ? { key, taskId: serverTurn.taskId,
        phase: nextTurn(nextTurn(IDLE_TURN, { type: "start", key }),
          { type: "status", state: serverTurn.state }).phase,
        error: serverTurn.error }
    : turn.key === key ? turn : IDLE_TURN, [localStream, key, serverTurn, turn]);
  const isCheckingTask = Boolean(key) && (observed?.key !== key || checking?.key === key);
  const myQuestion = pending?.key === key ? pending : null;

  const canRetry = Boolean(lastAttempt && lastAttempt.key === key &&
    lastAttempt.taskId === activeTurn.taskId && !isCheckingTask && activeTurn.phase === "failed");
  const retry = useCallback(() => canRetry && lastAttempt
    ? run(lastAttempt.text) : Promise.resolve(false), [canRetry, lastAttempt, run]);

  /**
   * Sends a structured answer to the question this conversation is holding.
   *
   * The semantic transcript record and wire payload are written from the same
   * choices. The runtime still receives fallback prose, but the reader sees the
   * question and answer as one compact record rather than a protocol message.
   */
  const answerQuestion = useCallback(
    async (answers: readonly string[][]) => {
      const parked = pendingRef.current;
      if (!parked || parked.key !== key || parked.request.kind !== "ask_user") return;
      await run(
        answerText(answers),
        askUserAnswer(parked.request.requestId, answers),
        [
          {
            kind: "ask_user",
            interaction: {
              questions: parked.request.questions,
              answers: answers.map((answer) => [...answer]),
              askedBy: parked.request.askedBy,
            },
          },
        ],
      );
    },
    [key, run],
  );

  const answerToolApproval = useCallback(
    async (decisions: readonly ToolApprovalDecision[]) => {
      const parked = pendingRef.current;
      if (!parked || parked.key !== key || parked.request.kind !== "tool_approval") return;
      await run(
        toolApprovalText(parked.request.tools, decisions),
        toolApprovalAnswer(decisions),
        [
          {
            kind: "tool_approval",
            approval: {
              tools: parked.request.tools,
              decisions: [...decisions],
              askedBy: parked.request.askedBy,
            },
          },
        ],
      );
    },
    [key, run],
  );

  return useMemo(
    () => ({
      messages: mine && "messages" in mine ? mine.messages : NO_MESSAGES,
      isLoadingHistory: Boolean(key) && mine === null,
      refreshTranscript,
      historyError: mine && "error" in mine ? mine.error : undefined,
      isCheckingTask,
      sessionNotice: checking && checking.key === key
        ? checking.error
          ? `Could not confirm session state. Retrying before allowing another message: ${checking.error.message}`
          : "Checking the active turn…"
        : isActive(activeTurn.phase) && localStream !== key
          ? "Session busy. Following the active turn."
          : notice && notice.key === key ? notice.text : undefined,
      turnError: activeTurn.error,
      turnState: turnStateOf(activeTurn.phase),
      turnPhase: activeTurn.phase,
      phase: isActive(activeTurn.phase)
        ? ("streaming" as const)
        : isDismissing
          ? ("streaming" as const)
          : ("idle" as const),
      pendingQuestion: myQuestion?.request,
      send: run,
      cancel,
      dismissQuestion,
      answerQuestion,
      answerToolApproval,
      retry,
      canRetry,
    }),
    [
      mine,
      key,
      checking,
      isCheckingTask,
      notice,
      localStream,
      refreshTranscript,
      activeTurn,
      myQuestion,
      isDismissing,
      run,
      cancel,
      dismissQuestion,
      answerQuestion,
      answerToolApproval,
      retry,
      canRetry,
    ],
  );
}

/** Applies an update to the transcript, but only if it is still the right one. */
function withMessages(
  current: Transcript | null,
  key: string,
  update: (messages: ChatMessage[]) => ChatMessage[],
): Transcript | null {
  if (!current || current.key !== key || !("messages" in current)) {
    return current;
  }
  return { key, messages: update(current.messages) };
}

/**
 * Adds a message this client wrote itself, even before history has arrived.
 *
 * Preflight may finish before the initial read. A local message must still have
 * a transcript to land in; subsequent reads merge it by message id.
 */
function appendLocal(
  current: Transcript | null,
  key: string,
  message: ChatMessage,
): Transcript {
  if (current?.key === key && "messages" in current) {
    return { key, messages: upsert(current.messages, message) };
  }
  return { key, messages: [message] };
}

/**
 * The conversation as the server has it, with anything this client added kept.
 *
 * Order is history first, then the local additions in the order they were made:
 * the local ones are always newer, because the only way to have one is to have
 * just sent it. Identity is the message id, which is the same id the transport
 * was told to send — so a message the server has already recorded is recognised
 * as the one on screen rather than appended a second time.
 */
function mergeHistory(
  current: Transcript | null,
  key: string,
  messages: ChatMessage[],
): Transcript {
  if (current?.key !== key || !current || !("messages" in current)) {
    return { key, messages };
  }
  const known = new Set(messages.map((message) => message.id));
  const extra = current.messages.filter((message) => !known.has(message.id));
  return extra.length === 0 ? { key, messages } : { key, messages: [...messages, ...extra] };
}

/** Adds a message, or replaces one already delivered under the same id. */
function upsert(messages: ChatMessage[], next: ChatMessage): ChatMessage[] {
  const index = messages.findIndex((message) => message.id === next.id);
  if (index === -1) return [...messages, next];
  const copy = messages.slice();
  copy[index] = next;
  return copy;
}

/** Appends streamed text to a message already on screen. */
function appendDelta(
  messages: ChatMessage[],
  messageId: string,
  chunk: string,
): ChatMessage[] {
  return messages.map((message) => {
    if (message.id !== messageId) return message;
    const parts = message.parts.slice();
    const first = parts[0];
    if (first?.kind === "text") {
      parts[0] = { ...first, text: first.text + chunk };
    } else {
      parts.push({ kind: "text", text: chunk });
    }
    return { ...message, parts };
  });
}

function asError(cause: unknown): Error {
  return cause instanceof Error ? cause : new Error(String(cause));
}
