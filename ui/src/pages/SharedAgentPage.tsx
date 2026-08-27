import { Alert, Skeleton, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ChatComposer } from "@/components/chat/ChatComposer";
import { ChatTranscript } from "@/components/chat/ChatTranscript";
import { useAgentInstanceShareToken } from "@/api/shareToken";
import { useChat } from "@/api/hooks/useChat";
import { useLiveTranscript } from "@/api/hooks/useLiveTranscript";
import { shortInstanceId } from "@/components/agent-instances/instanceLabels";

const { Text } = Typography;

/**
 * A conversation somebody shared, opened by the link they sent.
 *
 * ## What a share is
 *
 * A capability. The owner creates a token from their own conversation, and whoever
 * holds it can read that conversation — the controller resolves the token to the
 * owner and answers as though the owner had asked, while keeping the visitor's own
 * identity for the record. The visitor still signs in as themselves: a share widens
 * what one account may read, it does not replace authentication, and an
 * unauthenticated request carrying a token is refused.
 *
 * ## Why this page could not exist until now
 *
 * `AgentInstanceService` has carried `CreateAgentInstanceShare` all along, but
 * nothing on the read path honoured what it minted: the gRPC interceptor resolved
 * `X-Share-Token` through `GetSessionShareByToken` and built a share context naming
 * a *session*, while the A2A gateway authorises on the instance. A link created from
 * the dialog would have been refused here.
 *
 * The interceptor now tries both kinds of share, and the gateway reads the instance
 * as the share's owner when the token names that instance — which it has to, because
 * an instance is scoped to its creator and reading it as the visitor finds nothing.
 *
 * ## Replying, when the share allows it
 *
 * A share is `READ_ONLY` or `READ_WRITE`, and the backend accepts a send from the
 * visitor only on the second. This page used to offer no composer at all, on the
 * reasoning that one which worked for some links and not others would fail invisibly.
 * The cost of that was worse: the owner could tick "Also allow replies", hand over a
 * link, and the person opening it had no way to reply — a permission granted and
 * silently ignored.
 *
 * So the link a read-write share produces carries `?reply`, and this page offers a
 * composer when it sees it. **That is a hint about what to draw, never a permission.**
 * The controller resolves the token and refuses a send the share does not allow, so a
 * hand-edited address gets a composer whose first message is refused in the
 * controller's own words — which is the same thing that would happen without the hint,
 * only now it is the forger who sees it rather than the invited guest.
 */
export function SharedAgentPage() {
  const theme = useTheme();
  const { namespace, id, token } = useParams<{
    namespace: string;
    id: string;
    token: string;
  }>();

  // Before the read below, and that ordering is load-bearing: the chat client reads
  // the registered token when it builds a call, and registering after the first
  // request has gone means an unauthenticated read that looks like success.
  useAgentInstanceShareToken(namespace, id, token);

  // Present, not truthy: `?reply` carries no value.
  const [searchParams] = useSearchParams();
  const mayReply = searchParams.has("reply");

  const conversation = namespace && id ? { namespace, id } : undefined;






  const chat = useChat(conversation);

  /*
   * The owner is writing to this conversation too, so it has to keep up.
   *
   * A visitor sending through a share and the owner sending from their own page are two
   * people in one conversation, and neither was told about the other: each side read the
   * transcript once and then showed it unchanged until a reload.
   */
  useLiveTranscript(chat.refreshTranscript, {
    enabled: Boolean(conversation),
    isBusy: chat.phase === "streaming",
  });

  return (
    <PageFrame
      title={id ? `Conversation ${shortInstanceId(id)}` : "Shared conversation"}
      description="A conversation with an agent, shared with you to read."
    >
      {/* Said on the page, not only in the URL. A reader who was sent a link has no
          other way to know that what they are looking at is somebody else's
          conversation, or why there is nowhere to reply. */}
      <Alert
        type="info"
        showIcon
        data-testid="shared-agent-notice"
        title={mayReply ? "Shared with you" : "Shared with you, read-only"}
        description={
          mayReply
            ? "You are reading this conversation through a share link that allows replies. Anything you send is recorded as the owner's, because a share answers as them."
            : "You are reading this conversation through a share link. This one does not allow replies."
        }
        css={{ marginBottom: theme.space(4) }}
      />

      {chat.historyError ? (
        <Alert
          type="error"
          showIcon
          data-testid="shared-agent-error"
          title="This share could not be opened"
          // The backend's own wording: a revoked token, an expired one and a
          // conversation that no longer exists are different problems for whoever
          // sent the link.
          description={chat.historyError.message}
        />
      ) : chat.isLoadingHistory ? (
        <Skeleton active paragraph={{ rows: 6 }} data-testid="shared-agent-loading" />
      ) : (
        // A column with a bounded height, so the transcript inside it has something to
        // scroll within and the composer stays at the foot rather than being pushed
        // down the page by a long conversation.
        <div
          data-testid="shared-agent-transcript"
          css={{
            display: "flex",
            flexDirection: "column",
            minHeight: 0,
            height: `calc(100vh - ${theme.layout.headerHeight}px - ${theme.space(48)})`,
          }}
        >
          <ChatTranscript chat={chat} sessionId={id} />
          {mayReply ? (
            <div css={{ marginTop: theme.space(4), display: "grid", gap: theme.space(1) }}>
              <ChatComposer
                send={chat.send}
                isStreaming={chat.phase === "streaming"}
                onCancel={chat.cancel}
              />

            </div>
          ) : null}
        </div>
      )}

      <Text
        type="secondary"
        css={{ display: "block", marginTop: theme.space(4), fontSize: 12 }}
      >
        Shares can be revoked by whoever created them, and this link stops working
        when they are.
      </Text>
    </PageFrame>
  );
}
