import { Alert, Skeleton, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ChatTranscript } from "@/components/chat/ChatTranscript";
import { useSessionTranscript } from "@/api/hooks/useSessionTranscript";
import { useSession } from "@/api";
import { useShareToken } from "@/api/shareToken";

const { Text } = Typography;

/** `namespace__NS__name`, as the controller identifies a session's agent. */
const AGENT_ID_SEPARATOR = "__NS__";

/**
 * A conversation someone shared, opened by the link they sent.
 *
 * ## What a share is
 *
 * A capability. The owner creates a token from their own conversation, and whoever holds
 * it can read that conversation — the backend resolves the token to the owner and answers
 * as though the owner had asked, while keeping the visitor's identity for the record. The
 * visitor still signs in as themselves: a share widens what one account may read, it does
 * not replace authentication, and an unauthenticated request with a token is refused.
 *
 * ## Why this page exists
 *
 * The two halves of this feature had been apart for a while. Tokens could be created,
 * listed and revoked from the conversation's own dialog, and the backend honoured them on
 * every session route — but nothing in the app could *spend* one, so the tokens were
 * strings with no way to use them and the dialog was offering a share that did not work.
 *
 * ## Read-only, deliberately
 *
 * A share can be marked writable, and the backend then allows a visitor to send into the
 * conversation. This page does not offer that even for a writable share: the affordance
 * would be a composer that works for some links and not others, with the difference
 * invisible until it failed. What the share is is stated instead.
 */
export function SharedSessionPage() {
  const theme = useTheme();
  const { sessionId, token } = useParams<{ sessionId: string; token: string }>();

  // Before the two reads below, and that ordering is load-bearing: effects run in the
  // order their hooks were called, so registering here is what puts the token on the
  // first request rather than on a retry.
  useShareToken(sessionId, token);

  const session = useSession(sessionId);
  // Not `useChat`: that is addressed by AgentInstance and speaks to the A2A
  // gateway, which knows nothing about sessions. A share token names a session, so
  // this reads the session's stored turns instead.
  const chat = useSessionTranscript(sessionId);

  const agentRef = session.data?.agent_id?.replace(AGENT_ID_SEPARATOR, "/");
  // The conversation's own name when it has one: a session created from a first
  // message carries it, and an unnamed one should not be titled with an empty string.
  const title = session.data?.name?.trim() || "Shared conversation";

  return (
    <PageFrame
      title={title}
      description={
        agentRef
          ? `A conversation with ${agentRef}, shared with you to read.`
          : "A conversation shared with you to read."
      }
    >
      {/* Said on the page, not only in the URL. A reader who was sent a link has no
          other way to know that what they are looking at is somebody else's
          conversation, or why there is nowhere to reply. */}
      <Alert
        type="info"
        showIcon
        data-testid="shared-session-notice"
        title="Shared with you, read-only"
        description={
          session.data?.share_read_only === false
            ? "The link you opened allows replies elsewhere; this view shows the conversation as it stands."
            : "You are reading this conversation through a share link. Replying is not part of a share."
        }
        css={{ marginBottom: theme.space(4) }}
      />

      {session.error ? (
        <Alert
          type="error"
          showIcon
          data-testid="shared-session-error"
          title="This share could not be opened"
          // The backend's own wording: an expired or revoked token and a conversation
          // that no longer exists are different problems for the person who sent it.
          description={session.error.message}
        />
      ) : session.isLoading ? (
        <Skeleton active paragraph={{ rows: 6 }} data-testid="shared-session-loading" />
      ) : (
        <div data-testid="shared-session-transcript">
          <ChatTranscript chat={chat} sessionId={sessionId} />
        </div>
      )}

      <Text
        type="secondary"
        css={{ display: "block", marginTop: theme.space(4), fontSize: 12 }}
      >
        Shares can be revoked by whoever created them, and this link stops working when
        they are.
      </Text>
    </PageFrame>
  );
}
