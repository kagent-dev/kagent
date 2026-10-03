import { useState } from "react";
import useSWR from "swr";
import { Alert, Button, Empty, Input, Modal, Popconfirm, Space, Table, Typography } from "antd";
import type { ChatConversationRef } from "@/api/chat/types";
import {
  deleteTaskPushConfig,
  listTaskPushConfigs,
  saveTaskPushConfig,
  taskAcceptsPushConfigs,
  type TaskPushConfig,
} from "@/api/chat/taskPushConfigs";

function callbackURL(value: string): boolean {
  try {
    const url = new URL(value);
    return (url.protocol === "https:" || url.protocol === "http:") && !url.username && !url.password && !url.hash;
  } catch {
    return false;
  }
}

export function TaskPushNotificationsModal({
  conversation,
  taskId,
  onClose,
}: {
  conversation: ChatConversationRef;
  taskId: string;
  onClose: () => void;
}) {
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<string>();
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState<string>();
  const [id, setId] = useState("");
  const [url, setURL] = useState("");
  const [token, setToken] = useState("");
  const [bearerCredential, setBearerCredential] = useState("");
  const { data: configs = [], error: readError, isLoading: loading, mutate } = useSWR(
    ["task-push-configs", conversation.id, taskId],
    () => listTaskPushConfigs(conversation, taskId),
  );
  const { data: editable, error: stateError, isLoading: stateLoading } = useSWR(
    ["task-push-editable", conversation.id, taskId],
    () => taskAcceptsPushConfigs(conversation, taskId),
  );

  function edit(config: TaskPushConfig) {
    setEditing(config.id);
    setId(config.id);
    setURL(config.url);
    setToken("");
    setBearerCredential("");
    setError(undefined);
  }

  function resetForm() {
    setEditing(undefined);
    setId("");
    setURL("");
    setToken("");
    setBearerCredential("");
  }

  async function save() {
    if (!editable) return;
    if (!callbackURL(url.trim())) {
      setError("Enter an absolute HTTP or HTTPS callback URL without credentials.");
      return;
    }
    if (/\s/.test(bearerCredential)) {
      setError("Bearer credential must not contain whitespace.");
      return;
    }
    setSaving(true);
    setError(undefined);
    try {
      await saveTaskPushConfig(conversation, taskId, { id: id.trim(), url: url.trim(), token, bearerCredential });
      resetForm();
      await mutate();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  async function remove(configId: string) {
    setDeleting(configId);
    setError(undefined);
    try {
      await deleteTaskPushConfig(conversation, taskId, configId);
      if (editing === configId) resetForm();
      await mutate();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setDeleting(undefined);
    }
  }

  return (
    <Modal open title="Task notifications" footer={null} onCancel={onClose} width={720} data-testid="task-push-modal">
      <Space orientation="vertical" size="middle" css={{ width: "100%" }}>
        <Typography.Text type="secondary" copyable={{ text: taskId }}>
          Task {taskId}
        </Typography.Text>
        <Typography.Paragraph type="secondary" css={{ marginBottom: 0 }}>
          Add a callback while the task is active. New callbacks are rejected after it finishes.
          Only future updates are sent when the task needs input or reaches a final state.
          Failed attempts are retried, so a
          receiver may see duplicates. Credentials and notification tokens are never shown again;
          enter new values when editing a callback.
        </Typography.Paragraph>
        {editable === false ? (
          <Alert type="info" showIcon title="This task has finished. You can view or remove its callbacks, but cannot add or edit them." />
        ) : null}
        {error || readError || stateError ? (
          <Alert
            type="error"
            showIcon
            title={error ?? (readError instanceof Error ? readError.message : stateError instanceof Error ? stateError.message : String(stateError))}
            data-testid="task-push-error"
          />
        ) : null}
        <Table
          size="small"
          rowKey="id"
          loading={loading}
          dataSource={configs}
          pagination={false}
          locale={{ emptyText: <Empty description="No active callbacks for this task" /> }}
          columns={[
            { title: "ID", dataIndex: "id", key: "id" },
            { title: "Callback URL", dataIndex: "url", key: "url", ellipsis: true },
            {
              title: "Actions",
              key: "actions",
              render: (_, config: TaskPushConfig) => (
                <Space>
                  <Button size="small" onClick={() => edit(config)} disabled={!editable}>Edit</Button>
                  <Popconfirm
                    title="Remove this callback?"
                    description="Pending updates will be canceled. An in-flight request may still arrive."
                    onConfirm={() => void remove(config.id)}
                  >
                    <Button size="small" danger loading={deleting === config.id}>Remove</Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
          data-testid="task-push-list"
        />
        {editable ? <Typography.Text strong>{editing ? `Edit ${editing}` : "Add callback"}</Typography.Text> : null}
        {editable ? <>
        <Input
          aria-label="Callback ID"
          placeholder="ID (optional; generated when blank)"
          value={id}
          disabled={Boolean(editing)}
          onChange={(event) => setId(event.target.value)}
        />
        <Input
          aria-label="Callback URL"
          placeholder="https://example.com/a2a/callback"
          value={url}
          onChange={(event) => setURL(event.target.value)}
          onPressEnter={() => void save()}
        />
        <Input.Password
          aria-label="Bearer credential"
          placeholder="Bearer credential (optional; enter again when editing)"
          value={bearerCredential}
          onChange={(event) => setBearerCredential(event.target.value)}
        />
        <Input.Password
          aria-label="Notification token"
          placeholder="Notification token (optional)"
          value={token}
          onChange={(event) => setToken(event.target.value)}
        />
        <Space>
          <Button type="primary" onClick={() => void save()} loading={saving || stateLoading} disabled={!url.trim() || !editable}>
            {editing ? "Save changes" : "Add callback"}
          </Button>
          {editing ? <Button onClick={resetForm}>Cancel edit</Button> : null}
        </Space>
        </> : null}
      </Space>
    </Modal>
  );
}
