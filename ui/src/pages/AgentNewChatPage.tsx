import { useMemo, useState } from "react";
import { Alert, Button, Space, Typography } from "antd";

const { Text } = Typography;
import { useTheme } from "@emotion/react";
import { useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentRail } from "@/components/agent/AgentRail";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { ChatComposer } from "@/components/chat/ChatComposer";
import { buildPath, paths } from "@/router/routes";
import { apiClient, useAgentConversations } from "@/api";

/**
 * A conversation with an agent that has not been created yet.
 *
 * ## Why this page exists at all
 *
 * "New chat" used to call `CreateAgentInstance` and navigate to the result, so the
 * instance existed the moment somebody clicked — and every visit that changed its mind
 * left an empty conversation behind for good. That is not hypothetical: the live
 * cluster carries nine of them, all unnamed, none with a single message, and the pool
 * ran out of workers twice during one afternoon's recording because of it. An instance
 * is not free — it holds a prepared revision, and deleting the last instance that
 * references a revision does not collect it.
 *
 * So the instance is created by the **first message**. Until then this is a page with a
 * composer and nothing behind it, which is exactly what the reader has.
 *
 * ## The two things this has to get right
 *
 * **The request id is minted once per draft, not once per send.** `CreateAgentInstance`
 * takes one for idempotency; minting a fresh one on a retry would turn a failed send
 * that actually succeeded into two conversations. It is reset only after a create
 * lands, when the next message belongs to a conversation that exists.
 *
 * **The message is handed to the chat page rather than sent here.** Sending would mean
 * a second copy of the turn machinery living on a page that has no transcript to put
 * the result in. The text travels in router state and `AgentChatPage` sends it once on
 * arrival — which also means the reader sees their own words in the transcript they
 * will keep reading, rather than watching them disappear from one page and reappear on
 * another.
 */
export function AgentNewChatPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { namespace, agentTemplate, harness } = useParams();

  const conversations = useAgentConversations(namespace, agentTemplate, harness);
  const rows = useMemo(() => conversations.data?.all ?? [], [conversations.data]);

  const [isCreating, setCreating] = useState(false);
  const [error, setError] = useState<Error>();
  /*
   * The message the failed attempt was carrying.
   *
   * Kept so a retry does not ask the reader to type it again. The commonest failure
   * here is `ResourceExhausted: no free workers available`, which is about the cluster
   * rather than about anything they wrote — and losing their message to it would be
   * the page punishing them for the pool being full.
   */
  const [lastAttempt, setLastAttempt] = useState<string>();

  /*
   * One id for this draft, however many times sending is attempted.
   *
   * Held in state rather than minted at send time so a retry after a failure that
   * actually reached the controller is recognised as the same request instead of
   * making a second conversation.
   */
  const [requestId] = useState(() => crypto.randomUUID());

  async function startWith(text: string): Promise<void> {
    if (!namespace || !agentTemplate || !harness) return;
    setCreating(true);
    setError(undefined);
    setLastAttempt(text);
    try {
      const created = await apiClient.agentInstances.create({
        namespace,
        harness,
        agentTemplate,
        requestId,
      });
      // Refreshed before leaving, so the rail on the page being navigated to already
      // lists this conversation rather than filling it in a moment later.
      await conversations.refresh();
      navigate(
        buildPath(paths.agentChat, { namespace: created.namespace, id: created.id }),
        // The message the conversation was created *for*. Sent by the chat page on
        // arrival; see this file's note on why it is not sent here.
        { replace: true, state: { initialMessage: text } },
      );
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause : new Error(String(cause)));
      setCreating(false);
    }
  }

  return (
    // No heading. The rail names the agent immediately to the left, and the empty
    // state below says what sending will do — a title repeating both pushed the
    // composer down for nothing.
    <PageFrame>
      <div
        data-testid="agent-surface"
        css={{
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
        }}
      >
        {namespace ? (
          <AgentRail
            agentRef={{ namespace }}
            agentTitle={{
              primary: agentTemplate ?? namespace,
              secondary: harness ? `on ${harness}` : namespace,
            }}
            agentHref={agentPageUrl({ namespace, agentTemplate, harness })}
            // From the URL, since there is no conversation here to read it from.
            agentPair={{ namespace: namespace ?? "", agentTemplate, harness }}
            instances={{ ...conversations, data: rows }}
          />
        ) : null}

        {/* Centred in the space it has rather than sitting at the top of it: this page
            is two lines and a box, and pinned to the top they read as the beginning of
            a page whose content failed to load. */}
        <Space
          orientation="vertical"
          size="middle"
          css={{
            display: "flex",
            flex: 1,
            minWidth: 0,
            justifyContent: "center",
            /*
             * Exactly the room there is, so nothing scrolls.
             *
             * It was `min-height: 70vh`, which with the header above it and this page's
             * own padding came to more than the window — so a page whose entire content
             * is one centred box had a scrollbar with nothing to scroll to. The same
             * measurement the conversation itself uses, for the same reason.
             */
            height: `calc(100vh - ${theme.layout.headerHeight}px - ${theme.space(12)})`,
          }}
        >
          {error ? (
            <Alert
              type="error"
              showIcon
              data-testid="new-chat-error"
              title="Could not start this conversation"
              // The controller's own words. `ResourceExhausted: no free workers
              // available` is the one a reader can act on — it is the pool, not their
              // message, and it is the failure this page's whole reason for existing
              // makes rarer.
              description={
                <span css={{ display: "grid", gap: theme.space(2) }}>
                  <span>{error.message}</span>
                  {/* A worker frees the moment another conversation finishes, so the
                      same request often succeeds seconds later. The retry carries the
                      message that failed and reuses the same request id, so a failure
                      that actually reached the controller cannot become a second
                      conversation. */}
                  {lastAttempt ? (
                    <span>
                      <Button
                        size="small"
                        loading={isCreating}
                        onClick={() => void startWith(lastAttempt)}
                        data-testid="new-chat-retry"
                      >
                        Try again
                      </Button>
                    </span>
                  ) : null}
                </span>
              }
            />
          ) : null}

          <div
            data-testid="new-chat-empty"
            css={{
              display: "grid",
              placeItems: "center",
              alignContent: "center",
              gap: theme.space(2),
              /* Enough to sit clear of the page's top, not enough to push the composer
                 toward the fold. It was 240, which left a gulf under two lines of
                 text and made the box the reader types into feel like an afterthought
                 at the bottom of an empty page. */
              minHeight: 120,
              paddingBlockEnd: theme.space(2),
              textAlign: "center",
            }}
          >
            {/* Which agent, said here as well as in the rail: this is the page's own
                subject, and a reader who has collapsed the rail would otherwise have
                nothing on screen naming what they are about to talk to. */}
            {agentTemplate && harness ? (
              <Text
                data-testid="new-chat-agent"
                css={{ fontSize: 16, color: theme.color.text }}
              >
                {agentTemplate}{" "}
                <Text css={{ color: theme.color.textMuted }}>on {harness}</Text>
              </Text>
            ) : null}
            <Text css={{ color: theme.color.textMuted, fontSize: 14 }}>
              Send a message to start a new conversation.
            </Text>
          </div>

          {/*
            Bounded, and lit.

            The box stretched to whatever width the window had, which on a wide screen
            put the send button a long way from the words being typed and made the one
            thing this page is for look like a page-wide banner. A measure is what a
            single input wants; the same one a paragraph wants, for the same reason.

            The glow says "start here" without another line of text asking for
            attention. It is the brand colour rather than a rainbow — the page already
            carries that colour in its background wash, and two unrelated palettes on
            one screen read as decoration rather than emphasis.
          */}
          <div
            data-testid="new-chat-composer"
            /*
             * The whole panel is the target, not just the field inside it.
             *
             * The panel is drawn as one control and is mostly padding, so a click an
             * inch from the text landed on a `div` and did nothing — the reader has to
             * aim at the line itself to start typing, which is not what a box this size
             * looks like it is asking for.
             *
             * `onMouseDown` rather than `onClick`, and the default prevented: by the
             * time a click completes the browser has already moved focus to whatever
             * was pressed, so focusing afterwards is a second move and the caret
             * flickers. Anything that takes focus on its own — the send button, the
             * field — is left alone.
             */
            onMouseDown={(event) => {
              const target = event.target as HTMLElement;
              if (target.closest("button, textarea, input, a")) return;
              event.preventDefault();
              event.currentTarget.querySelector("textarea")?.focus();
            }}
            css={{
              cursor: "text",
              width: "100%",
              maxWidth: 640,
              marginInline: "auto",
              borderRadius: theme.radius.lg,
              padding: theme.space(2),
              /*
               * One surface, not a box inside a glowing box.
               *
               * The first version wrapped the composer in a lit panel and left the
               * field's own border on, so there were two purple outlines a few pixels
               * apart with the send button stranded outside the inner one — three
               * shapes where a reader sees one control. This *is* the control: it
               * carries the edge and the glow, and the field and button inside it are
               * transparent so they read as parts of it rather than things sitting on
               * it.
               */
              background: theme.color.bgElevated,
              border: `1px solid ${theme.color.primary}59`,
              /* A ring close in, and a wider one further out. The near ring is what
                 makes the edge read as lit rather than merely coloured; the far one is
                 the glow, kept weak enough to be noticed without being looked at. */
              boxShadow: `0 0 0 4px ${theme.color.primary}1A, 0 10px 36px -18px ${theme.color.primary}99`,
              transition: "border-color 200ms ease, box-shadow 200ms ease",
              "&:focus-within": {
                borderColor: `${theme.color.primary}A6`,
                boxShadow: `0 0 0 5px ${theme.color.primary}2E, 0 14px 44px -16px ${theme.color.primary}CC`,
              },
              /* The field is the surface, so it brings no edge, fill or ring of its
                 own — including the one antd draws on focus, which would reappear
                 inside the border above. */
              "& textarea": {
                background: "transparent",
                border: "none",
                boxShadow: "none",
                paddingInline: theme.space(2),
                resize: "none",
              },
              "& textarea:focus": { boxShadow: "none", border: "none" },
            }}
          >
            <ChatComposer
              send={startWith}
              isStreaming={isCreating}
              variant="inviting"
              disabled={!namespace || !agentTemplate || !harness}
            />
          </div>
        </Space>
      </div>
    </PageFrame>
  );
}
