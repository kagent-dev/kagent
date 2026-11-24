import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Modal, Space, Table, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Copy, Trash2 } from "lucide-react";
import { apiClient, type AgentInstanceShare } from "@/api";
import { withBasePath } from "@/env";
import { buildPath, paths } from "@/router/routes";
import { copyText } from "@/components/common/copyText";

const { Text } = Typography;

/** Build an absolute link for read-only access to a conversation. */
function shareLink(id: string, token: string): string {
  const path = buildPath(paths.sharedAgent, { id, token });
  return `${window.location.origin}${withBasePath(path)}`;
}

export function ShareDialog({
  conversation,
  open,
  onClose,
}: {
  /** The conversation being shared, which is an `AgentInstance`. */
    conversation: { id: string };
  open: boolean;
  onClose: () => void;
}) {
  const theme = useTheme();

  /**
   * `undefined` until the list has been read once.
   *
   * Derived rather than paired with an `isLoading` flag, because the flag would have
   * to be set from inside the effect that starts the read — a synchronous `setState`
   * in an effect, which cascades a render for something a value already answers.
   * "Not read yet" and "read, and empty" are different states and this is the
   * difference.
   */
  const [shares, setShares] = useState<AgentInstanceShare[]>();
  const [error, setError] = useState<string>();
  const [isCreating, setCreating] = useState(false);
  const [reloadToken, setReloadToken] = useState(0);

  /**
   * The link just created, held only until the dialog closes.
   *
   * The one moment the token exists to be shown: the controller keeps a digest, so
   * nothing can produce it again. Deliberately not persisted anywhere — a token in
   * storage is a credential outliving the page that was trusted with it.
   */
  const [freshLink, setFreshLink] = useState<string>();

  /** Which link was copied, and whether it worked. Kept against the link so a
      newly created one resets it. */
  const [copied, setCopied] = useState<{ link: string; ok: boolean }>();

  const reload = useCallback(() => setReloadToken((count) => count + 1), []);

  useEffect(() => {
    if (!open) return;
    let active = true;

    apiClient.agentInstances.shares
      .list(conversation.id)
      .then((next) => {
        if (!active) return;
        setShares(next);
        setError(undefined);
      })
      .catch((cause: unknown) => {
        if (!active) return;
        // A `NotFound` here means "not yours" as much as "not there": the controller
        // checks the instance belongs to the caller before listing. Said plainly
        // rather than shown as an empty list, which would read as "no links" to
        // somebody who has some.
        setShares([]);
        setError(cause instanceof Error ? cause.message : String(cause));
      });

    return () => {
      active = false;
    };
  }, [open, conversation.id, reloadToken]);

  async function create() {
    setCreating(true);
    setError(undefined);
    try {
      const created = await apiClient.agentInstances.shares.create(conversation.id);
      setFreshLink(shareLink(conversation.id, created.token));
      reload();
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setCreating(false);
    }
  }

  async function revoke(shareId: string) {
    setError(undefined);
    try {
      await apiClient.agentInstances.shares.revoke(shareId);
      // The link on screen may be the one just revoked, and a copy button for a dead
      // link is worse than none.
      setFreshLink(undefined);
      reload();
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return (
    <Modal
      open={open}
      onCancel={() => {
        // The token is gone when the dialog closes, because it is gone everywhere:
        // keeping it on screen across an open would imply it could be recovered.
        setFreshLink(undefined);
        onClose();
      }}
      footer={null}
      title="Share this conversation"
    >
      {/* The handle is on the body, not on `Modal`. antd forwards unknown props to
          `ant-modal-root` -- a portal wrapper with no box of its own -- so a testid
          there reports hidden for a dialog plainly on screen. */}
      <Space
        orientation="vertical"
        size="middle"
        css={{ display: "flex" }}
        data-testid="share-dialog"
      >
        <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
          Whoever opens the link can read this conversation after signing in.
          Shared links are read-only and do not allow replies.
        </Text>

        <Button
          type="primary"
          loading={isCreating}
          onClick={() => void create()}
          data-testid="share-create"
        >
          Create a link
        </Button>

        {/* The one moment the token exists. Said, not implied. */}
        {freshLink ? (
          <Alert
            type="success"
            showIcon
            data-testid="share-fresh-link"
            title="Copy this link now — it cannot be shown again"
            description={
              <Space orientation="vertical" size={4} css={{ display: "flex" }}>
                <Text
                  css={{
                    fontFamily: theme.font.mono,
                    fontSize: 12,
                    wordBreak: "break-all",
                  }}
                >
                  {freshLink}
                </Text>
                <Button
                  size="small"
                  icon={<Copy size={13} />}
                  data-testid="share-copy-fresh-link"
                  onClick={() => {
                    void copyText(freshLink)
                      .catch(() => false)
                      .then((ok) => setCopied({ link: freshLink, ok }));
                  }}
                >
                  {copied?.link === freshLink && copied.ok ? "Copied" : "Copy link"}
                </Button>
                {copied?.link === freshLink && !copied.ok ? (
                  <Text type="danger" data-testid="share-copy-failed">
                    Could not copy. Select the link above and copy it yourself.
                  </Text>
                ) : null}
              </Space>
            }
          />
        ) : null}

        {error ? (
          <Alert
            type="error"
            showIcon
            data-testid="share-error"
            title="Could not do that"
            description={error}
          />
        ) : null}

        <Table<AgentInstanceShare>
          size="small"
          rowKey="id"
          loading={shares === undefined}
          dataSource={shares ?? []}
          pagination={false}
          data-testid="share-list"
          locale={{ emptyText: "No share links yet." }}
          columns={[
            {
              title: "Link",
              key: "id",
              // The token is not here and cannot be: only its digest is stored. What
              // a row identifies is the *share*, which is what revoking takes.
              render: (_, share) => (
                <Text
                  css={{
                    fontFamily: theme.font.mono,
                    fontSize: 12,
                    wordBreak: "break-all",
                  }}
                >
                  {share.id}
                </Text>
              ),
            },
            {
              title: "",
              key: "actions",
              width: 48,
              render: (_, share) => (
                <Tooltip title="Revoke this link">
                  <Button
                    type="text"
                    size="small"
                    danger
                    icon={<Trash2 size={13} />}
                    data-testid={`revoke-share-${share.id}`}
                    aria-label={`Revoke share ${share.id}`}
                    onClick={() => void revoke(share.id)}
                  />
                </Tooltip>
              ),
            },
          ]}
        />
      </Space>
    </Modal>
  );
}
