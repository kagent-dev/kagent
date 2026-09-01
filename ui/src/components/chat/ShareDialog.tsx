import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Modal, Space, Switch, Table, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Copy, Trash2 } from "lucide-react";
import { apiClient, type AgentInstanceShare } from "@/api";
import { buildPath, paths } from "@/router/routes";

const { Text } = Typography;

/**
 * Share links for one conversation: create, list, revoke.
 *
 * ## What a share is over
 *
 * An `AgentInstance`. The instance *is* the conversation — the A2A gateway files
 * every task under it as the task's `contextId` — so sharing one hands somebody what
 * was said. The gRPC interceptor validates the instance token, and the A2A gateway
 * authorises that same instance as the share's owner.
 *
 * ## A token is shown once, and the list never shows one
 *
 * The controller stores only a digest — which is what keeps a database dump from
 * being a set of working links — so the token comes back from the create call and
 * from nowhere else. The list can therefore show that a share exists, when it was
 * made and what it allows, but never the link itself.
 *
 * This is not a limitation to work around: it is the reason the newly created link
 * is shown prominently and says it will not be shown again. A dialog that listed
 * tokens would be describing an API that does not exist.
 *
 * ## Read-only is the default, and stays the default
 *
 * `READ_ONLY` allows A2A get, list and subscribe; `READ_WRITE` also allows send and
 * cancel — so the second one lets a visitor talk *as* the owner. That is the right
 * way round for something that gives away access, so the switch starts off and has
 * to be turned on.
 */

/**
 * The address a share is sent as.
 *
 * Absolute, because it is going into somebody else's messages: a path would be
 * copied into a chat and pasted somewhere that has no idea what host it came from.
 * Built from the page's own origin, so a deployment behind any hostname produces
 * links to itself.
 */
/**
 * The link a reader sends somebody.
 *
 * A read-write share carries `?reply` so the page it opens knows to offer a composer.
 * That is a *hint*, not a permission: the controller resolves the token and refuses a
 * send the share does not allow, whatever the address claims. Without it the visitor
 * has no way to know a reply is allowed — the share's permission is readable only by
 * its owner — and the toggle that granted it would do nothing they could see.
 */
function shareLink(
  namespace: string,
  id: string,
  token: string,
  allowWrites: boolean,
): string {
  const path = buildPath(paths.sharedAgent, { namespace, id, token });
  return `${window.location.origin}${path}${allowWrites ? "?reply" : ""}`;
}

export function ShareDialog({
  conversation,
  open,
  onClose,
}: {
  /** The conversation being shared, which is an `AgentInstance`. */
  conversation: { namespace: string; id: string };
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
  const [allowWrites, setAllowWrites] = useState(false);
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

  const reload = useCallback(() => setReloadToken((count) => count + 1), []);

  useEffect(() => {
    if (!open) return;
    let active = true;

    apiClient.agentInstances.shares
      .list(conversation.namespace, conversation.id)
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
  }, [open, conversation.namespace, conversation.id, reloadToken]);

  async function create() {
    setCreating(true);
    setError(undefined);
    try {
      const created = await apiClient.agentInstances.shares.create(
        conversation.namespace,
        conversation.id,
        allowWrites ? "readWrite" : "readOnly",
      );
      setFreshLink(
        shareLink(conversation.namespace, conversation.id, created.token, allowWrites),
      );
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
      await apiClient.agentInstances.shares.revoke(conversation.namespace, shareId);
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
          Whoever opens the link can read this conversation. They sign in as
          themselves — a share widens what one account may read, it does not replace
          signing in.
        </Text>

        <Space size={8}>
          <Switch
            checked={allowWrites}
            onChange={setAllowWrites}
            data-testid="share-allow-writes"
          />
          <Text>Also allow replies</Text>
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
            Anything they send goes to this agent as you.
          </Text>
        </Space>

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
                  onClick={() => void navigator.clipboard?.writeText(freshLink)}
                >
                  Copy link
                </Button>
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
              title: "Allows",
              key: "permission",
              width: 130,
              render: (_, share) =>
                share.permission === "readWrite" ? "Read & write" : "Read only",
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
