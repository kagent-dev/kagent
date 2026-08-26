import { useEffect, useRef, useState } from "react";
import { Alert, Button, Empty, Skeleton, Tag, Tooltip } from "antd";
import { ChevronDown } from "lucide-react";
import { useTheme } from "@emotion/react";
import type { ChatController, ChatTurnPhase } from "@/api";
import { AskUserPrompt } from "./AskUserPrompt";
import { ChatMessageItem } from "./ChatMessageItem";

/**
 * Turn phases worth naming on screen. The rest are transient enough to skip.
 *
 * Keyed on the machine's phase rather than on the A2A task state, which is what
 * separates "asked, nothing back yet" from "the answer is arriving" — the two the
 * transport spells the same way, and the two a reader most wants told apart.
 */
const PHASE_LABEL: Partial<Record<ChatTurnPhase, string>> = {
  sending: "Sent",
  working: "Working…",
  streaming: "Answering…",
  awaiting_input: "Waiting for you",
  canceled: "Cancelled",
};

/**
 * The conversation itself: history, the turn in flight, and whatever went wrong.
 *
 * Scrolls to the newest message as content arrives, because a stream that fills
 * in below the fold reads as nothing happening.
 */
export function ChatTranscript({
  chat,
  sessionId,
}: {
  chat: ChatController;
  /**
   * The conversation being shown, handed to each message.
   *
   * Only used by contributions at the per-message point: a message id means something
   * to this client, while anything asking a backend about the work behind a message is
   * keyed by the conversation and the turn.
   */
  sessionId?: string;
}) {
  const theme = useTheme();
  const bottomRef = useRef<HTMLDivElement>(null);
  /** The box that scrolls, which is this component's own — see the observer below. */
  const scrollRef = useRef<HTMLDivElement>(null);
  const [isAtBottom, setAtBottom] = useState(true);
  /** How far short of the end still counts as being at it. */
  const AT_BOTTOM_SLACK = 32;
  /*
   * The same fact as `isAtBottom`, readable from a listener.
   *
   * The observers below run outside React's render, so they cannot see the state — a
   * closure over it would hold whatever was true when the effect was set up.
   */
  const pinnedRef = useRef(true);

  /*
   * A return to the foot the reader asked for, still travelling.
   *
   * A smooth scroll passes through every position between where they were and the
   * foot, and each one fires the scroll listener. Read as ordinary scrolling, those
   * intermediate positions say the reader is nowhere near the bottom and unset the
   * pin — which cancels the very journey they requested, and leaves the button back
   * on screen if the animation lands short. So a requested return owns the pin until
   * it arrives, or until the reader takes the scroll back with a gesture of their own.
   */
  const returningRef = useRef(false);

  /*
   * Following the stream, but only when the reader is already at the foot.
   *
   * This transcript is not its own scroll container — it could only become one by
   * being given a height, and the only height available is a guess at the shell's
   * chrome. So the page scrolls and the composer is pinned by the panel around this
   * component.
   *
   * Which leaves following. Doing it unconditionally is right at the bottom and wrong
   * the moment the reader scrolls up to re-read something: yanking them back down
   * mid-sentence is the most irritating thing a chat can do. So it is conditional on a
   * sentinel at the foot being in view, and when it is not, a button appears that takes
   * them back.
   */
  /*
   * Whether the reader is at the foot, measured from the scroll position.
   *
   * This was an IntersectionObserver watching a one-pixel sentinel, and it was never
   * reliable: scrolled hard to the end, the sentinel would sometimes not report as
   * intersecting at all, so the button stayed on screen telling a reader already at the
   * bottom to go there. Sub-pixel layout and a sentinel with no height is too fine an
   * edge to ask that question on.
   *
   * The scroll position answers it directly and cannot disagree with itself. The
   * tolerance is what a reader means by "at the bottom" — a line or so short still
   * counts, and without it the button flickers on the last pixel of a smooth scroll.
   */
  useEffect(() => {
    const box = scrollRef.current;
    if (!box) return;
    const atBottom = () =>
      box.scrollHeight - box.scrollTop - box.clientHeight <= AT_BOTTOM_SLACK;
    const measure = () => {
      const now = atBottom();
      if (returningRef.current) {
        if (!now) return;
        returningRef.current = false;
      }
      pinnedRef.current = now;
      setAtBottom(now);
    };
    /*
     * The reader's own gesture ends a requested return, whatever it was doing.
     *
     * Without this a return that never arrives — a conversation still growing as fast
     * as the scroll closes on it — would hold the pin forever and the reader could not
     * scroll away. These are the gestures that mean "I am steering now"; a programmatic
     * scroll fires none of them.
     */
    const release = () => {
      returningRef.current = false;
    };
    measure();
    box.addEventListener("scroll", measure, { passive: true });
    box.addEventListener("wheel", release, { passive: true });
    box.addEventListener("touchstart", release, { passive: true });
    box.addEventListener("keydown", release);
    /*
     * A conversation that grows keeps the reader at the foot if that is where they were.
     *
     * Content arrives after layout — a code block, an image, a message still streaming
     * — and each time it does the foot moves further down without anybody scrolling.
     * Measuring alone reported the reader as having left a conversation they had not
     * moved in, and offered them a button back to where they already thought they were.
     *
     * So growth re-pins rather than re-measuring. Only while they were at the foot: a
     * reader who has scrolled up to re-read something is left exactly where they are,
     * which is the whole reason this is conditional.
     */
    const resize = new ResizeObserver(() => {
      if (pinnedRef.current) box.scrollTop = box.scrollHeight;
      else measure();
    });
    resize.observe(box);
    // The content, not only the box: the box's own size rarely changes, and it is the
    // conversation inside it that grows.
    if (box.firstElementChild) resize.observe(box.firstElementChild);
    return () => {
      box.removeEventListener("scroll", measure);
      box.removeEventListener("wheel", release);
      box.removeEventListener("touchstart", release);
      box.removeEventListener("keydown", release);
      resize.disconnect();
    };
    // Re-run once the transcript is on screen: on the first render this component is a
    // loading skeleton and the ref is null, so a mount-only effect attached nothing at
    // all and the button never appeared.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chat.isLoadingHistory, chat.historyError]);

  /*
   * A conversation opens at its end, whatever the reader was doing a moment ago.
   *
   * Following is conditional on being at the foot, which is right for every later
   * message and wrong for the first paint: the transcript arrives scrolled to the top,
   * so the condition is already false by the time it could be consulted, and a reader
   * opening a long conversation landed at its beginning with a button inviting them to
   * the part they wanted. Anchoring once, unconditionally, is what a chat does.
   */
  const hasAnchored = useRef(false);
  useEffect(() => {
    const box = scrollRef.current;
    if (!box || hasAnchored.current || chat.messages.length === 0) return;
    hasAnchored.current = true;
    box.scrollTop = box.scrollHeight;
    pinnedRef.current = true;
    setAtBottom(true);
  }, [chat.messages.length]);

  useEffect(() => {
    // Scrolling the box itself rather than asking a sentinel to bring itself into
    // view: `scrollIntoView` walks up to whichever ancestor it decides is scrollable
    // and can move the page instead of the transcript.
    if (isAtBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
    // `isAtBottom` is deliberately not a dependency: it changes as a *result* of
    // scrolling, and reacting to it would scroll again the instant the reader reached
    // the bottom by hand.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chat.messages, chat.turnPhase]);

  if (chat.isLoadingHistory) {
    return (
      <div data-testid="chat-loading">
        <Skeleton active paragraph={{ rows: 4 }} />
      </div>
    );
  }

  if (chat.historyError) {
    return (
      <Alert
        type="error"
        showIcon
        data-testid="chat-history-error"
        title="Could not load this conversation"
        description={chat.historyError.message}
      />
    );
  }

  const statusLabel = PHASE_LABEL[chat.turnPhase];

  return (
    <div
      ref={scrollRef}
      /*
       * The transcript scrolls, not the page.
       *
       * It was the surface around it that owned this, which left every surface
       * mounting a transcript to arrange its own scrolling — and the shared
       * conversation, which never did, could not follow a turn to the bottom at all.
       * Owning it here means the box, the sentinel that decides whether the reader is
       * at the foot of it, and the button that takes them back are one thing.
       */
      css={{
        position: "relative",
        flex: 1,
        minHeight: 0,
        overflowY: "auto",
        // Clear of the messages, which run to the right edge — the reader's own align
        // that way, so an unpadded bar sits on top of them.
        paddingInlineEnd: theme.space(3),
        scrollbarWidth: "thin",
        scrollbarColor: `${theme.color.border} transparent`,
        "&::-webkit-scrollbar": { width: 10 },
        "&::-webkit-scrollbar-track": { background: "transparent" },
        "&::-webkit-scrollbar-thumb": {
          background: theme.color.border,
          borderRadius: 999,
          border: "3px solid transparent",
          backgroundClip: "content-box",
        },
        "&:hover::-webkit-scrollbar-thumb": { background: theme.color.textMuted },
      }}
    >
    <div
      data-testid="chat-transcript"
      css={{
        display: "grid",
        gap: theme.space(4),
        /*
         * Messages sit at the *bottom* of the space, not the top.
         *
         * The transcript is the part of the panel that grows, so a short conversation
         * left its two or three messages at the top with a band of empty space between
         * the last thing said and the box used to reply — the two furthest apart when
         * they are most closely related. Growing downward from the foot is what every
         * conversation does, and it also means a new message appears next to the
         * composer rather than a screen away from it.
         *
         * `min-height: 100%` so this fills the scroll container it is inside: without
         * it the grid is only as tall as its rows and there is nothing to align within.
         */
        alignContent: "end",
        minHeight: "100%",
      }}
    >
      {chat.messages.length === 0 ? (
        <Empty
          data-testid="chat-empty"
          description="No messages yet. Ask the agent something."
        />
      ) : (
        chat.messages.map((message) => (
          <ChatMessageItem key={message.id} message={message} sessionId={sessionId} />
        ))
      )}

      {statusLabel ? (
        <Tag
          data-testid="chat-status"
          data-state={chat.turnState}
          data-phase={chat.turnPhase}
          css={{ justifySelf: "start" }}
        >
          {statusLabel}
        </Tag>
      ) : null}

      {chat.pendingQuestion ? (
        /*
         * The conversation is holding a question, and that has to be said.
         *
         * Rendered as something answerable rather than as a notice, because the
         * question already appears twice above — as the tool call's JSON and as the
         * agent's prose — and neither can end the turn. `info` and deliberately not
         * `error`: nothing went wrong. The agent called a tool that asks the reader
         * something and its turn parked in `input_required`, a state the controller
         * keeps non-terminal on purpose. Colouring it red would be a visible lie
         * about a turn that worked.
         */
        <AskUserPrompt
          request={chat.pendingQuestion}
          isBusy={chat.phase === "streaming"}
          onAnswer={(answers) => void chat.answerQuestion(answers)}
          onDismiss={() => void chat.dismissQuestion()}
        />
      ) : null}

      {chat.turnError && !chat.pendingQuestion ? (
        <Alert
          type="error"
          showIcon
          data-testid="chat-turn-error"
          title="The agent could not finish this turn"
          description={chat.turnError.message}
          action={
            <Button size="small" onClick={() => void chat.retry()}>
              Retry
            </Button>
          }
        />
      ) : null}

      {/*
        The scroll target, and nothing more than that now.

        It used to be 112px tall, and had to be: the composer was `position: sticky;
        bottom: 0` inside the same scrolling column, so it floated over the foot of the
        conversation and the last message landed underneath it. The clearance had to sit
        between the conversation and the scroll target, because padding after this
        element scrolls away with it and changes nothing.

        The panel now gives the transcript its own scroll box with the composer below it
        rather than over it, so there is nothing left to clear — and the reserved space
        became exactly what a reader sees as a gap between the last answer and the box
        they reply in. A sentinel still has to exist for the scroll effect to aim at.
      */}
      <div ref={bottomRef} css={{ height: 1 }} />

    </div>

      {/*
        Anchored to the foot of the transcript, which is what scrolls.

        It was `position: fixed` against the viewport, from when the page was the scroll
        container — so it floated at a measured distance from the bottom of the window
        and had no relationship to the box it belonged to. Sticky inside the scroll box
        keeps it over the conversation on every surface, including a shared one whose
        page is a different shape.
      */}
      {isAtBottom ? null : (
        <div
          css={{
            position: "sticky",
            bottom: theme.space(3),
            display: "flex",
            justifyContent: "center",
            // No height of its own, so it does not add a row to the bottom of the
            // conversation while it is visible and take one away when it is not.
            height: 0,
            zIndex: 5,
          }}
        >
          <Tooltip title="Scroll to bottom">
            <Button
              shape="circle"
              data-testid="chat-scroll-bottom"
              aria-label="Scroll to bottom"
              icon={<ChevronDown size={15} />}
              onClick={() => {
                const box = scrollRef.current;
                if (!box) return;
                /*
                 * Said before it is done, because the reader has already said it.
                 *
                 * Waiting for the scroll listener to notice leaves the button on screen
                 * for the length of a smooth scroll, over a conversation that is
                 * visibly travelling to where the button would take it — and if the
                 * animation lands a fraction short, it stays. The listener still has
                 * the last word: scroll away again and it comes straight back.
                 */
                returningRef.current = true;
                pinnedRef.current = true;
                setAtBottom(true);
                /*
                 * The foot is a moving target, so the pin is what actually arrives.
                 *
                 * `scrollHeight` is read once, here, and a message that lands while the
                 * animation is running puts the real foot further down than the target
                 * — measured at 45px short on both engines, which is more than the
                 * tolerance, so the button came back. The pin above is what closes that:
                 * the resize observer re-pins on every growth, so the journey ends at
                 * whatever the foot has become rather than where it was when asked.
                 */
                box.scrollTo({ top: box.scrollHeight, behavior: "smooth" });
              }}
              css={{
                transform: "translateY(-100%)",
                boxShadow: `0 6px 18px -6px ${theme.color.bg}`,
              }}
            />
          </Tooltip>
        </div>
      )}
    </div>
  );
}
