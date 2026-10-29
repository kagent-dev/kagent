import { Alert, Skeleton, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ChatTranscript } from "@/components/chat/ChatTranscript";
import { useAgentInstanceShareToken } from "@/api/shareToken";
import { useAgentInstance } from "@/api";
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
 * The gRPC interceptor validates the instance share token, and the A2A gateway reads
 * that instance as the share's owner. That is required because instances are scoped
 * to their creator; reading one as the visitor would find nothing.
 */
export function SharedAgentPage() {
  const theme = useTheme();
  const { id, token } = useParams<{
    id: string;
    token: string;
  }>();

  // Before the read below, and that ordering is load-bearing: the chat client reads
  // the registered token when it builds a call, and registering after the first
  // request has gone means an unauthenticated read that looks like success.
  useAgentInstanceShareToken(id, token);

  const instance = useAgentInstance(id);
  const conversation = id && instance.data?.agent ? { id, agent: instance.data.agent } : undefined;

  const chat = useChat(conversation);
  const error = instance.error ?? chat.historyError;

  // Keep up with messages the owner sends while this page is open.
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
        title="Shared with you, read-only"
        description="You are reading this conversation through a share link. Shared links do not allow replies."
        css={{ marginBottom: theme.space(4) }}
      />

      {error ? (
        <Alert
          type="error"
          showIcon
          data-testid="shared-agent-error"
          title="This share could not be opened"
          // The backend's own wording: a revoked token, an expired one and a
          // conversation that no longer exists are different problems for whoever
          // sent the link.
          description={error.message}
        />
      ) : instance.isLoading || chat.isLoadingHistory ? (
        <Skeleton active paragraph={{ rows: 6 }} data-testid="shared-agent-loading" />
      ) : (
        // Bound the transcript height so long conversations scroll within it.
        <div
          data-testid="shared-agent-transcript"
          css={{
            display: "flex",
            flexDirection: "column",
            minHeight: 0,
            height: `calc(100vh - ${theme.layout.headerHeight}px - ${theme.space(48)})`,
          }}
        >
          <ChatTranscript chat={chat} sessionId={id} recovery={instance.data?.recovery} readOnly />
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
