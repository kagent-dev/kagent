import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Tooltip } from "antd";
import { FileText, PanelRightClose, PanelRightOpen, Share2 } from "lucide-react";
import { useTheme } from "@emotion/react";
import { ChatComposer } from "@/components/chat/ChatComposer";
import { ShareDialog } from "@/components/chat/ShareDialog";
import { AgentRail } from "@/components/agent/AgentRail";
import { iconControlStyles } from "@/components/agent/controlStyles";
import { AgentContextPanel } from "@/components/chat/AgentContextPanel";
import { ConversationDetailsModal } from "@/components/chat/ConversationDetailsModal";
import { ResizableAside } from "@/components/chat/ResizableAside";
import { ChatTranscript } from "@/components/chat/ChatTranscript";
import { isLifecycleBusy } from "@/components/chat/lifecycleReading";
import { paths } from "@/router/routes";
import {
  apiClient,
  useAgentInstance,
  useAgentInstances,
  useChat,
  type AgentInstanceOperation,
  type AgentInstanceState,
} from "@/api";
import { autoTitleFrom } from "@/components/agent-instances/instanceLabels";
import { useLiveTranscript } from "@/api/hooks/useLiveTranscript";

/**
 * How often the instance is re-read while it is doing something.
 *
 * A lifecycle operation on this cluster takes seconds rather than milliseconds —
 * a template's actor reached ready in about twelve — so a second is fast enough to
 * catch a stage and slow enough that a long answer is not a hundred round trips.
 */
const LIFECYCLE_POLL_MS = 1_000;

/**
 * The conversation with one agent.
 *
 * ## There is no session id here
 *
 * An `AgentInstance` *is* the conversation. The A2A gateway files every task under
 * the instance as the task's `contextId`, and `ListTasks` for the instance is the
 * transcript — so `/agents/:namespace/:id/chat` is the whole address, and there is
 * nothing to put in a session segment.
 *
 * That is also why "New chat" in the rail *creates* rather than navigates: another
 * conversation with the same agent is another instance of the same
 * `(Harness, AgentTemplate)` pair, and the siblings of this instance are the other
 * conversations you have had with it.
 *
 * ## Sharing
 *
 * A share is over the instance, because the instance is the conversation. That
 * needed a server-side change to be honest and now has one: the gRPC interceptor
 * used to resolve `X-Share-Token` only through `GetSessionShareByToken`, producing a
 * context naming a *session*, while the A2A gateway authorises on the instance — so
 * a link created here would have been refused when opened. The interceptor now tries
 * both kinds of share and the gateway reads the instance as the share's owner.
 */
/** Where the agent panel's open state is remembered, per reader. */
const CONTEXT_OPEN = "kagent.chat.agentPanel.open";

export function AgentChatPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { namespace, id } = useParams();
  const location = useLocation();

  const instance = useAgentInstance(namespace, id);
  /*
   * Every instance in this namespace, for the rail's list of sibling conversations.
   *
   * Read here rather than in the rail because this page is the one that creates and
   * deletes them, so it is the one that has to refresh the list afterwards — and a
   * rail reading its own copy would show a conversation this page had just removed.
   */
  const instances = useAgentInstances(namespace);

  const conversation = useMemo(
    () => (namespace && id ? { namespace, id } : undefined),
    [namespace, id],
  );
  /**
   * Resume a suspended conversation before any turn begins.
   *
   * Handed to `useChat` rather than wrapped around `send`, because a turn begins two
   * ways: the composer, and Retry. Retry starts one from inside the hook and never
   * sees a wrapper out here — and with conversations giving their workers back after
   * every turn, suspended is exactly the state a failed turn leaves behind, so Retry
   * is the likeliest button to be pressed against one.
   *
   * The state is read when the turn starts rather than closed over, and the resume is
   * awaited rather than fired alongside the send: sending into an instance that has
   * not finished resuming is the refusal this exists to avoid.
   */
  /*
   * What this page has just asked the conversation to become.
   *
   * Both changes it makes — resuming to send, and suspending when a turn ends — are
   * asynchronous, so the record still reports the old state for a second or two
   * afterwards. The rail's indicator went on showing that, which reads as the send or
   * the change not having happened. This is handed to the rail so the row answers
   * immediately, and cleared once the record agrees.
   */
  const [askedFor, setPendingState] = useState<AgentInstanceState>();
  /*
   * The operation this page has claimed but the record does not show yet.
   *
   * Separate from the state because they are different facts and the indicator draws
   * them differently: suspending is amber and travelling, suspended is grey and still.
   * Without this the button here jumped straight to grey while the same action from the
   * rail's row menu showed the amber step — the same request reported two ways.
   */
  const [pendingOperation] = useState<AgentInstanceOperation>();
  /* Derived, not cleared in an effect: once the record reports what was asked for there
     is nothing standing in for anything, and comparing here says that without a second
     render to undo the first. */
  const pendingState = askedFor === instance.data?.state ? undefined : askedFor;





  const resumeFirst = useCallback(async () => {
    if (instance.data?.state !== "suspended" || !namespace || !id) return;
    setPendingState("ready");
    try {
      await apiClient.agentInstances.resume(namespace, id);
      await instance.refresh();
    } catch (cause: unknown) {
      // Back to the truth: the turn is about to fail too, and a row claiming ready
      // would outlive the error that says otherwise.
      setPendingState(undefined);
      throw cause;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instance.data?.state, namespace, id]);

  const chat = useChat(conversation, resumeFirst);

  /*
   * The other side of a share writes here too.
   *
   * A read-write share makes this conversation something two people send to, and the
   * owner sitting on this page was the one who never saw the visitor's messages.
   */
  useLiveTranscript(chat.refreshTranscript, {
    enabled: Boolean(conversation),
    isBusy: chat.phase === "streaming",
  });

  /**
   * What to call this conversation when nobody has named it.
   *
   * Derived from the reader's first message, which is the only thing about a
   * conversation that says what it is *about* — and it is free here and nowhere
   * else. Titling a *list* of conversations this way would cost a `ListTasks` per
   * row, so a list falls back to the id; this page has the transcript in hand
   * because it is rendering it.
   *
   * Display only: it is not written back. A stored auto-title would be a name
   * nobody chose, indistinguishable from one somebody did, and the reader could
   * never tell whether clearing it would restore anything. The rename control on
   * the agent's page is where a name comes from.
   */
  const autoTitle = useMemo(() => {
    const firstFromReader = chat.messages.find((message) => message.role === "user");
    const said = firstFromReader?.parts.find((part) => part.kind === "text")?.text;
    return autoTitleFrom(said);
  }, [chat.messages]);

  const [isSharing, setSharing] = useState(false);
  const [isShowingDetails, setShowingDetails] = useState(false);

  /**
   * Starts another conversation with this agent.
   *
   * A new instance of the same pair — which is what a second conversation *is* — so
   * this needs the current instance loaded to copy the pair from. The rail's button
   * is disabled until then rather than creating something from a half-read record.
   */


  /*
   * Whether this agent can be talked to at all.
   *
   * The gateway refuses any call for an instance that is not `READY` — it answers
   * `UnsupportedOperation` naming the state — so a composer offered over a suspended
   * or failed agent is a box that swallows what the reader types. The state is said
   * instead.
   */
  const state = instance.data?.state;
  const isReady = state === "ready";
  /*
   * Suspended is not "cannot answer" — it is "not resumed yet".
   *
   * The gateway does refuse a message for a suspended instance, but the reader's
   * intention is unambiguous: they typed something and pressed send. Making them find
   * a Resume control first, having just been told the agent gave its worker back at
   * the end of the last turn, is a step the page can take for them — and with
   * that would be a detour on the way to every message.
   *
   * Only suspended. Creating, failed and deleting are states a message cannot be
   * carried through by resuming, so those still say so and disable the box.
   */
  const isSuspended = state === "suspended";
  const canSend = isReady || isSuspended;

  /*
   * Watching the instance while it is doing something.
   *
   * The lifecycle indicator reads `AgentInstance.operation`, which the controller
   * claims and clears as it works — so it only ever *changes* on a re-read, and a
   * page that read the record once at mount would show whichever operation was in
   * flight when it loaded, forever. So it is re-read while there is something to
   * see and left alone when there is not: a chat page open on an idle agent makes
   * no requests at all.
   *
   * Driven from a timer calling `refresh()` rather than from SWR's
   * `refreshInterval`, for the reason recorded against the substrate page: SWR
   * deduplicates a revalidation for `dedupingInterval` ms *after the response*, so
   * a short interval quietly becomes a long one and the page reports a poll rate
   * it is not keeping. And a tick that would overlap the one still in flight is
   * dropped rather than queued, because a slow read must not build a backlog.
   */
  const isAwaitingReply = Boolean(chat.pendingQuestion);

  /*
   * The agent panel's state, remembered per reader.
   *
   * Absent means open: a reader who has never touched it gets the context, which is
   * the more useful default for somebody meeting an agent for the first time.
   */
  const [isContextOpen, setContextOpen] = useState(
    () => window.localStorage.getItem(CONTEXT_OPEN) !== "false",
  );

  function toggleContext() {
    setContextOpen((open) => {
      window.localStorage.setItem(CONTEXT_OPEN, String(!open));
      return !open;
    });
  }

  /*
   * The message this conversation was created for, sent once on arrival.
   *
   * `AgentNewChatPage` creates the instance from the first message and hands the text
   * over in router state rather than sending it itself — there is no transcript on that
   * page to put the answer in. Sending it here means the reader sees their own words in
   * the conversation they are going to keep reading.
   *
   * Guarded by a ref *and* by clearing the history entry: a ref alone would re-send if
   * the component remounted, and clearing alone would re-send on a fast double render
   * before the navigation settled. Sending a message twice is not a cosmetic fault — it
   * is two turns, and the second is refused while the first is in flight.
   */
  const sentInitial = useRef(false);
  useEffect(() => {
    const pending = (location.state as { initialMessage?: string } | null)
      ?.initialMessage;
    /*
     * Held until the transcript has been read, which is not merely tidy.
     *
     * The history read owns the same abort controller the send does, and its cleanup
     * aborts whatever is in flight. Sending during mount — while that read is still
     * open, and while React is double-invoking effects in development — put the
     * reader's message on screen and then killed the request before it reached the
     * wire: a conversation showing what you typed, never answering, and no
     * `SendStreamingMessage` in the controller's log at all.
     */
    if (!pending || sentInitial.current || !conversation || chat.isLoadingHistory) return;
    sentInitial.current = true;
    /*
     * Sent before the history entry is cleared, not after.
     *
     * Clearing first navigates — a `replace` to the same path — and that re-render
     * aborted the stream the send had just opened: the reader's message appeared,
     * because the optimistic append had already happened, and nothing was ever put on
     * the wire. A conversation that shows what you typed and never answers, with no
     * `SendStreamingMessage` in the controller's log at all.
     *
     * The clear still has to happen, or a reload would send it again; it just belongs
     * after the turn is under way.
     */
    void chat.send(pending);
    // Keyed on the conversation, not on `chat`: the controller is rebuilt every render
    // and depending on it would re-run this on each one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [namespace, id, chat.isLoadingHistory]);
  const isBusy = isLifecycleBusy(instance.data, chat.turnPhase, isAwaitingReply);

  /*
   * The conversation list is deliberately not re-read when this one's state changes.
   *
   * The rail draws the open conversation from the live read above, so its indicator is
   * already current and nothing here is stale to a reader. Refreshing the list to make
   * the other readers of it agree sooner looked harmless and silently broke sending:
   * the re-render reaches `useChat`, whose history effect aborts the controller an
   * in-flight send is using, so the message went nowhere and nothing said so. Gating it
   * on an idle turn was not enough either, because the state moves once during the
   * resume that happens *before* the turn starts.
   *
   * That is a fragility in `useChat` — one effect owning another's abort — and worth
   * fixing there rather than worked around here. Until then the list catches up on its
   * next ordinary read, which costs nothing a reader can see.
   */

  /*
   * The page no longer suspends a conversation when its turn ends.
   *
   * It used to, and had to: nothing on the server gave a worker back, so a conversation
   * held one from creation until somebody suspended or deleted it, and a cluster with
   * more conversations than workers could not start another. The page made the
   * `SuspendAgentInstance` call nobody else was making.
   *
   * The server does it now — a completed A2A turn quiesces its own runtime and records
   * the snapshot boundary it stopped at. That is strictly better than what this did: it
   * happens whether or not a tab is open, which was this version's stated limitation and
   * the reason the pool could still fill up.
   *
   * Doing both would be worse than doing neither. The server's quiesce releases the
   * worker and deliberately leaves the instance logically `ready`; this suspended it
   * logically as well, so every conversation read as suspended and every message paid
   * for a resume first — a round trip added to each turn for something already done.
   */

  // Held in a ref so the interval is not torn down and rebuilt every time SWR
  // hands back a new function identity — which would reset the clock on each read.
  const refreshInstance = useRef(instance.refresh);
  useEffect(() => {
    refreshInstance.current = instance.refresh;
  });

  useEffect(() => {
    if (!isBusy) return;
    let inFlight = false;
    const timer = setInterval(() => {
      if (inFlight) return;
      inFlight = true;
      void refreshInstance.current().finally(() => {
        inFlight = false;
      });
    }, LIFECYCLE_POLL_MS);
    return () => clearInterval(timer);
  }, [isBusy]);

  /*
   * And once more when the agent goes quiet.
   *
   * The last poll before a turn ends can land while the controller is still
   * claiming an operation, so without this the indicator would keep the previous
   * reading until the reader did something else. Fired on the *transition* out of
   * busy rather than on every render, so an idle page still makes no requests.
   */
  const wasBusy = useRef(isBusy);
  useEffect(() => {
    if (wasBusy.current && !isBusy) void refreshInstance.current();
    wasBusy.current = isBusy;
  }, [isBusy]);

  return (
    <div data-testid="agent-surface">
      <div css={{
          display: "flex",
          gap: theme.space(6),
          /*
           * `flex-start`, not `stretch`.
           *
           * Stretched, each sidebar is as tall as the whole conversation — and a sticky
           * element as tall as its scroll container has nowhere to stick, so on a long
           * chat both rails scrolled away with the page. At `flex-start` they keep
           * their own height and stay put, which is the entire reason they are sticky.
           */
          alignItems: "flex-start",
        }}>
        {namespace && id ? (
          <AgentRail
            agentRef={{ namespace, id }}
            instance={instance.data}
            instances={instances}
            autoTitle={autoTitle}
            pendingState={pendingState}
            pendingOperation={pendingOperation}
            // Only the chat needs to know: deleting the conversation it is showing
            // leaves it on an address that no longer resolves.
            onDeleted={(target) => {
              if (target.id === id) navigate(paths.agents);
            }}
            /* In the rail's gutter, under the collapse toggle, rather than in a row of
               its own across the conversation. It is one icon; a whole row for it cost
               the transcript its width and left the three controls at the top of the
               page on different lines as soon as anything scrolled. */
            gutterActions={
              conversation ? (
                <>
                <Tooltip title="Share this conversation" placement="right">
                  <Button
                    type="text"
                    size="small"
                    icon={<Share2 size={16} aria-hidden />}
                    onClick={() => setSharing(true)}
                    aria-label="Share this conversation"
                    data-testid="chat-share"
                    css={iconControlStyles(theme)}
                  />
                </Tooltip>
                {/* Under Share, in the same gutter. It was an entry in the rail, which
                    meant leaving the conversation to read four facts about it and then
                    finding the way back — reference that costs a navigation is
                    reference nobody consults. */}
                <Tooltip title="Conversation details" placement="right">
                  <Button
                    type="text"
                    size="small"
                    icon={<FileText size={16} aria-hidden />}
                    onClick={() => setShowingDetails(true)}
                    aria-label="Conversation details"
                    data-testid="chat-details"
                    css={iconControlStyles(theme)}
                  />
                </Tooltip>
                </>
              ) : null
            }
          />
        ) : null}

        <section
          data-testid="chat-panel"
          css={{
            flex: 1,
            minWidth: 0,
            /*
             * A column, not a three-row grid.
             *
             * It was `grid-template-rows: auto 1fr auto`, which assumes exactly three
             * children — and this panel has a varying number of them, because the
             * notices above the transcript come and go with the conversation's state.
             * A fourth child pushed the `1fr` onto a different row, so the transcript
             * stopped being the part that grows and the composer moved: the flicker a
             * reader saw when clicking between conversations, worst on the short ones
             * where the composer is not pinned to the foot of the viewport anyway.
             *
             * As a column the transcript is the only thing that grows, whatever else
             * is on screen, so the composer stays where it is.
             */
            display: "flex",
            flexDirection: "column",
            /* Small, because the biggest gap in this column is the one between the last
               thing the agent said and the box you reply in — and the two are a
               conversation, not two separate parts of a page. The composer paints its
               own fade above itself, which is the separation that was wanted. */
            gap: theme.space(2),
            /*
             * The panel owns its height, and the transcript scrolls inside it.
             *
             * It used to be `min-height: 64vh` with the page doing the scrolling, which
             * meant the composer sat at the end of the content — so its position was a
             * function of how much had been said. Clicking between conversations moved
             * it by whatever the difference in transcript length happened to be, and
             * loading one moved it twice: once for the skeleton, once for the messages.
             * That is the flicker.
             *
             * Fixed to the viewport instead: the header, this page's own padding, and
             * nothing else. The transcript is the part that scrolls, so the message box
             * is in the same place in every conversation and stays there while one
             * loads.
             */
            height: `calc(100vh - ${theme.layout.headerHeight}px - ${theme.space(12)})`,
          }}
        >
          {/* The panel's own controls. What the agent is doing sits at the leading
              edge, where a reader's eye already is for the conversation below it;
              sharing is a capability of one conversation, so it is offered at the
              corner the panel opens from. */}

          {instance.error ? (
            <Alert
              type="error"
              showIcon
              data-testid="chat-instance-error"
              title="Could not load this conversation"
              description={
                instance.error.code === "NotFound"
                  ? /*
                     * `NotFound` here does not mean "gone".
                     *
                     * `GetAgentInstance` resolves through `GetAgentInstanceForUser`
                     * with the caller's id in the WHERE clause, so somebody else's
                     * conversation answers not-found rather than refused — the two
                     * are indistinguishable on the wire, by design. Reporting only
                     * the controller's wording would tell a reader their
                     * conversation had been deleted when it is simply not theirs.
                     */
                    `${instance.error.message} It may also belong to someone else: a conversation is scoped to whoever created it, and one you cannot read is reported the same way as one that does not exist.`
                  : instance.error.message
              }
            />
          ) : null}



          {/* The controller's own precondition, said rather than discovered. A
              suspended agent is resumable from the agents list, which is why the
              state is named rather than the page simply refusing. */}
          {instance.data && !canSend ? (
            <Alert
              type="warning"
              showIcon
              data-testid="chat-not-ready"
              title={`This conversation is ${state}, so the agent cannot answer`}
              description={
                instance.data.failure?.message ||
                "The A2A gateway accepts messages only for a ready conversation. Resume it from the agent's page, or start another conversation."
              }
            />
          ) : null}

          {/* The transcript owns its own scrolling now, so this passes it the room to
              do it in and nothing else. */}
          <ChatTranscript chat={chat} sessionId={id} />

          {/* Stuck to the foot of the viewport rather than the foot of the page. The
              page is what scrolls, so a composer in normal flow would be below the
              fold for the whole of a long conversation — the one control the reader
              always needs, permanently out of reach. The fade above it is so text
              does not appear to end abruptly at its top edge. */}
          <div
            css={{
              position: "sticky",
              bottom: 0,
              flexShrink: 0,
              paddingBlockEnd: theme.space(2),
              background: `linear-gradient(to bottom, transparent, ${theme.color.bg} ${theme.space(4)})`,
              display: "grid",
              // Tight: the status line belongs to the box above it and reads as its
              // state rather than as a separate thing under it.
              gap: theme.space(1),
            }}
          >
            <ChatComposer
              send={chat.send}
              isStreaming={chat.phase === "streaming"}
              onCancel={chat.cancel}
              // Disabled rather than hidden: a missing composer reads as a rendering
              // fault, where a disabled one with the state named above it explains
              // itself. A conversation holding a question keeps its composer, because
              // the box is how the question is answered — the answer names the parked
              // turn and resumes it.
              disabled={!canSend}
            />

          </div>
        </section>

        {/*
          What the agent is, beside what it said.
          
          Collapsible and remembered, like the rail: it is reference rather than
          navigation, so a reader following a long answer should be able to put it away
          — and find it away next time rather than having to close it on every
          conversation.
          
          Rendered only once the instance has loaded: the panel's whole content is
          derived from the template that instance names, so an empty one would be a
          frame around nothing.
        */}
        {instance.data ? (
          <>
            {/* One control that stays put and changes its icon, mirroring the rail's
                across the transcript. */}
            <div
              css={{
                flexShrink: 0,
                position: "sticky",
                top: theme.layout.headerHeight + 24,
                alignSelf: "start",
                marginInlineEnd: isContextOpen ? -theme.space(3) : 0,
                transition: "margin-inline-end 180ms ease",
              }}
            >
              <Button
                type="text"
                size="small"
                css={iconControlStyles(theme)}
                icon={
                  isContextOpen ? (
                    <PanelRightClose size={16} aria-hidden />
                  ) : (
                    <PanelRightOpen size={16} aria-hidden />
                  )
                }
                onClick={toggleContext}
                aria-label={
                  isContextOpen ? "Hide the agent panel" : "Show the agent panel"
                }
                data-testid={
                  isContextOpen ? "chat-context-collapse" : "chat-context-expand"
                }
              />
            </div>

            {/* Slides rather than vanishing, for the same reason the rail does: an
                unmounted panel makes the transcript jump its whole width in one frame,
                which reads as a layout fault rather than as something closing. */}
            <div
              css={{
                flexShrink: 0,
                width: isContextOpen ? 248 : 0,
                overflow: "hidden",
                // Sticky on the wrapper, not on the panel inside it: a sticky element
                // travels within its parent's box, and this wrapper is exactly as tall
                // as the panel. See the rail, which had the same fault.
                position: "sticky",
                top: theme.layout.headerHeight + 24,
                alignSelf: "start",
                /* Hidden for real once closed rather than clipped to zero width — a
                   child of a zero-width box still has a bounding box. Delayed by the
                   width transition when closing, immediate when opening. */
                visibility: isContextOpen ? "visible" : "hidden",
                transition: `width 180ms ease, visibility 0s linear ${isContextOpen ? "0s" : "180ms"}`,
              }}
              aria-hidden={!isContextOpen}
            >
              <ResizableAside
                testId="chat-context-aside"
                handleTestId="chat-context-handle"
                label="Resize the agent panel"
                defaultWidth={248}
              >
                <AgentContextPanel agent={instance.data} />
              </ResizableAside>
            </div>
          </>
        ) : null}
      </div>

      <ConversationDetailsModal
        instance={instance}
        open={isShowingDetails}
        onClose={() => setShowingDetails(false)}
      />

      {conversation ? (
        <ShareDialog
          conversation={conversation}
          open={isSharing}
          onClose={() => setSharing(false)}
        />
      ) : null}
    </div>
  );
}

