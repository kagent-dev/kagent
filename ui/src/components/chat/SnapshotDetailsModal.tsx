import { useState } from "react";
import { Button, Descriptions, Input, Modal, Popconfirm, Space, Tag, Tooltip, Typography } from "antd";
import { Eraser, GitFork, Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import toast from "react-hot-toast";
import {
  apiClient,
  canForkFrom,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  type Checkpoint,
  type CheckpointState,
} from "@/api";
import { relativeAge } from "@/components/agent-instances/instanceLabels";
import { ExtensionSlot } from "@/appExtensions/ExtensionSlot";

const { Paragraph, Text } = Typography;

/** What a snapshot is called on screen, for the one case the controller sends nothing. */
function snapshotLabel(checkpoint: Checkpoint): string {
  return checkpoint.name || `Snapshot ${checkpoint.id.slice(0, 8)}`;
}

/**
 * How far the controller has got, as a pill.
 *
 * The raw state rides along as an attribute so a test asserts on the state rather than
 * on its wording — the wording may change, the state may not. Same bargain `StateTag`
 * makes for an instance.
 */
const STATE_APPEARANCE: Record<CheckpointState, { label: string; tone: string }> = {
  ready: { label: "Ready", tone: "success" },
  creating: { label: "Saving", tone: "processing" },
  deleting: { label: "Deleting", tone: "warning" },
  failed: { label: "Failed", tone: "error" },
  unspecified: { label: "Not reported", tone: "default" },
  unknown: { label: "Unrecognised", tone: "default" },
};

/**
 * One snapshot's record, and everything that can be done to it.
 *
 * ## Why the divider no longer carries the buttons
 *
 * The line across the transcript held a Fork and a Remove, which made a mark on the
 * conversation into a toolbar — and left nowhere to put a third thing. Naming a
 * snapshot is that third thing, so the line went back to being a line and the actions
 * moved somewhere they can be read before they are pressed.
 *
 * There is no loading branch here. Unlike the conversation record this does not read
 * anything: the chat already holds the list these come from, so a caller that can open
 * this modal is a caller already holding the row.
 */
export function SnapshotDetailsModal({
  checkpoint,
  instanceId,
  onClose,
  onFork,
  onDelete,
  onRenamed,
}: {
  checkpoint: Checkpoint;
  /** The conversation it was taken in, which the extension point is keyed by. */
  instanceId: string;
  onClose: () => void;
  /** Opens a new conversation holding the transcript up to here. */
  onFork: (checkpointId: string) => void | Promise<void>;
  /** Releases the snapshot. Irreversible, so this is confirmed first. */
  onDelete: (checkpointId: string) => void | Promise<void>;
  /**
   * The record as it stands after a rename.
   *
   * Handed over rather than left to a re-read: the caller owns which snapshot is open,
   * and this is the one moment that record changes under it.
   */
  onRenamed: (renamed: Checkpoint) => void | Promise<void>;
}) {
  const theme = useTheme();
  const [isRenaming, setRenaming] = useState(false);
  const [isBusy, setBusy] = useState(false);
  const label = snapshotLabel(checkpoint);
  const forkable = canForkFrom(checkpoint);
  const state = STATE_APPEARANCE[checkpoint.state];

  async function act(run: () => void | Promise<void>) {
    setBusy(true);
    try {
      await run();
      onClose();
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open
      onCancel={onClose}
      footer={null}
      width={720}
      title="Snapshot details"
      // On the body rather than on the `Modal`: antd forwards an unknown prop to its
      // own wrapper, and which one it lands on is not this app's to promise.
      data-testid="snapshot-details-modal"
    >
      <div data-testid="snapshot-details-body">
        <Descriptions
          bordered
          size="small"
          column={2}
          data-testid="snapshot-details-fields"
          items={[
            {
              key: "name",
              label: "Name",
              span: 2,
              children: (
                <Space size={4}>
                  <Text data-testid="snapshot-details-name">{label}</Text>
                  <Tooltip title="Rename this snapshot">
                    <Button
                      type="text"
                      size="small"
                      icon={<Pencil size={13} />}
                      onClick={() => setRenaming(true)}
                      data-testid="snapshot-details-rename"
                      aria-label="Rename this snapshot"
                    />
                  </Tooltip>
                </Space>
              ),
            },
            {
              key: "id",
              label: "Snapshot ID",
              span: 2,
              children: (
                <Text copyable css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
                  {checkpoint.id}
                </Text>
              ),
            },
            {
              key: "headTaskId",
              label: "Turn",
              children: (
                <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
                  {checkpoint.headTaskId}
                </Text>
              ),
            },
            {
              key: "state",
              label: "State",
              children: (
                <Tag
                  color={state.tone}
                  data-testid="snapshot-details-state"
                  data-state={checkpoint.state}
                >
                  {state.label}
                </Tag>
              ),
            },
            {
              key: "createdAt",
              label: "Taken",
              span: 2,
              children: checkpoint.createdAt ? (
                <Text>
                  {checkpoint.createdAt}{" "}
                  <Text css={{ color: theme.color.textMuted }}>
                    ({relativeAge(checkpoint.createdAt)})
                  </Text>
                </Text>
              ) : (
                <Text css={{ color: theme.color.textMuted }}>Not reported</Text>
              ),
            },
            // Only when there is one: an empty "Failure" row on a healthy snapshot
            // reads as something the controller failed to send.
            ...(checkpoint.failure
              ? [
                  {
                    key: "failure",
                    label: "Failure",
                    span: 2,
                    children: (
                      <Text css={{ color: theme.color.dangerText }}>{checkpoint.failure}</Text>
                    ),
                  },
                ]
              : []),
          ]}
        />

        <Space
          size={8}
          css={{ marginBlockStart: theme.space(4), display: "flex", flexWrap: "wrap" }}
        >
          <Tooltip
            title={
              forkable
                ? "Start a new chat holding the history up to this snapshot."
                : "A snapshot can only be forked once it is ready."
            }
          >
            {/* Wrapped, because antd drops a tooltip on a disabled button: the
                explanation matters most in the case that disables it. */}
            <span>
              <Button
                type="primary"
                icon={<GitFork size={14} />}
                disabled={!forkable || isBusy}
                loading={isBusy}
                data-testid="snapshot-details-fork"
                onClick={() => void act(() => onFork(checkpoint.id))}
                css={{
                  "&:hover:not(:disabled), &:focus-visible:not(:disabled)": {
                    background: theme.color.primaryHover,
                    borderColor: theme.color.primaryHover,
                  },
                  "&:active:not(:disabled)": {
                    background: theme.color.primaryHover,
                    opacity: 0.85,
                  },
                }}
              >
                Fork
              </Button>
            </span>
          </Tooltip>

          <Popconfirm
            title="Delete this snapshot?"
            description="The saved runtime goes with it and nothing brings it back. Chats already forked from here are kept."
            overlayStyle={{ maxWidth: 320 }}
            okText="Delete"
            okButtonProps={{ danger: true, "data-testid": "snapshot-details-delete-confirm" }}
            cancelText="Cancel"
            onConfirm={() => void act(() => onDelete(checkpoint.id))}
          >
            <Button
              danger
              icon={<Eraser size={14} />}
              disabled={isBusy}
              data-testid="snapshot-details-delete"
              css={{
                "&:hover:not(:disabled), &:focus-visible:not(:disabled)": {
                  background: theme.color.dangerBg,
                  borderColor: theme.color.dangerBorder,
                },
                "&:active:not(:disabled)": { background: theme.color.dangerBg, opacity: 0.85 },
              }}
            >
              Delete
            </Button>
          </Popconfirm>
        </Space>

        {/* Part of the record rather than something floating under it, which is what
            the rule and the shared padding are for. */}
        <div
          css={{
            marginBlockStart: theme.space(4),
            paddingBlockStart: theme.space(3),
            borderTop: `1px solid ${theme.color.border}`,
            // Nothing to separate when no extension contributes: the slot renders
            // null, and an empty rule under the buttons reads as a missing section.
            "&:empty": { display: "none" },
          }}
        >
          <ExtensionSlot
            id="app_agents_agentChat_snapshotDetails_footer"
            context={{ snapshotId: checkpoint.id, instanceId, label }}
          />
        </div>
      </div>

      {/* Mounted only while open, which is what seeds the box with the stored name
          without an effect to put it there — see `RenameConversationDialog`. */}
      {isRenaming ? (
        <RenameSnapshotDialog
          checkpoint={checkpoint}
          onClose={() => setRenaming(false)}
          onRenamed={onRenamed}
        />
      ) : null}
    </Modal>
  );
}

/**
 * Names a snapshot, or puts the generated name back.
 *
 * Validated with `conversationNameProblem` rather than a second copy of the same
 * rules: the controller enforces one limit on both fields, so two validators here
 * would be two chances to disagree with it. Surrounding whitespace is refused rather
 * than trimmed, and empty is valid — it is how a reader's own title is cleared.
 */
function RenameSnapshotDialog({
  checkpoint,
  onClose,
  onRenamed,
}: {
  checkpoint: Checkpoint;
  onClose: () => void;
  onRenamed: (renamed: Checkpoint) => void | Promise<void>;
}) {
  const theme = useTheme();
  const [draft, setDraft] = useState(checkpoint.name);
  const [isSaving, setSaving] = useState(false);
  const problem = conversationNameProblem(draft);

  async function save() {
    if (problem) return;
    setSaving(true);
    try {
      const renamed = await apiClient.agentInstances.checkpoints.rename(checkpoint.id, draft);
      // Awaited before the toast, so the modal behind this already shows the new name
      // by the time the reader is told it changed.
      await onRenamed(renamed);
      onClose();
      toast.success(`Snapshot renamed to “${snapshotLabel(renamed)}”`);
    } catch (cause: unknown) {
      // Deliberately not transient: the old name is still on screen, and a reader who
      // missed the message would believe it changed.
      toast.error(
        `Could not rename the snapshot: ${
          cause instanceof Error ? cause.message : String(cause)
        }`,
        { duration: Infinity },
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open
      title="Name this snapshot"
      okText="Save"
      okButtonProps={{ loading: isSaving, disabled: problem !== undefined }}
      cancelText="Cancel"
      onOk={() => void save()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Paragraph css={{ color: theme.color.textMuted, fontSize: 13 }}>
        A fork taken from this snapshot is given its name. Leave this empty to go back
        to the generated one.
      </Paragraph>
      {/* The id is on a wrapper this app owns: antd spreads unknown props onto its
          inner `<input>`, which is not somewhere a test can reason about. */}
      <div data-testid="snapshot-rename-input">
        <Input
          value={draft}
          autoFocus
          maxLength={MAX_CONVERSATION_NAME_LENGTH}
          showCount
          status={problem ? "error" : undefined}
          onChange={(event) => setDraft(event.target.value)}
          onPressEnter={() => void save()}
          aria-label="Snapshot name"
        />
      </div>
      {problem ? (
        <Text
          data-testid="snapshot-rename-problem"
          css={{ color: theme.color.dangerText, fontSize: 12 }}
        >
          {problem}
        </Text>
      ) : null}
    </Modal>
  );
}
