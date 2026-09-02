import { useMemo, useState } from "react";
import { Alert, Button, Form, Input, Modal, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import type { CreatePromptTemplateRequest } from "@/api";
import { RFC1123_SUBDOMAIN } from "@/components/common/resourceName";
import { SubmitError } from "@/components/common/SubmitError";
import { paths } from "@/router/routes";
import { FragmentEditor } from "./FragmentEditor";
import {
  type PromptDraft,
  emptyPromptDraft,
  promptDraftIssues,
  promptPayloadFrom,
} from "./promptDraft";

const { Text, Paragraph } = Typography;

interface PromptFormProps {
  onSubmit: (payload: CreatePromptTemplateRequest) => Promise<void>;
  initial?: PromptDraft;
  outcome?: "created" | "saved";
  submitLabel?: string;
  /**
   * The name and namespace address the ConfigMap the library is stored as, so an edit
   * cannot move or rename one. Locked, they are shown rather than hidden: which
   * library is being changed is the first thing to be sure of.
   */
  identityLocked?: boolean;
  /**
   * What Cancel does when the form is not a page of its own.
   *
   * Given, the form asks before calling it if there is a draft to lose — the caller
   * that stays on the page is the caller whose Cancel throws work away invisibly.
   * Omitted, Cancel is a link back to the library list, which is a navigation the
   * browser can undo.
   */
  onCancel?: () => void;
}

/**
 * The fields of a prompt library, for creating one or changing one.
 *
 * Shared by the create page and the detail page's edit mode, on the same division of
 * labour the model form uses: the form owns the fields, the rules and whether a
 * submit is allowed through; the caller owns the request and what happens afterwards.
 *
 * Shared rather than written twice because the two surfaces are the same fields, and
 * two renderings of one thing drift — the one nobody edits is the one that quietly
 * stops offering something the API gained.
 */
export function PromptForm({
  onSubmit,
  initial,
  outcome = "created",
  submitLabel = "Create library",
  identityLocked = false,
  onCancel,
}: PromptFormProps) {
  const theme = useTheme();
  const [draft, setDraft] = useState<PromptDraft>(initial ?? emptyPromptDraft);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const [isConfirmingDiscard, setConfirmingDiscard] = useState(false);

  const issues = useMemo(
    () => promptDraftIssues(draft, { identityLocked }),
    [draft, identityLocked],
  );

  /*
   * Whether the draft differs from what it started as, compared by value: somebody who
   * types a character and deletes it again has not changed anything, and being asked
   * to confirm a discard of nothing teaches them to click through the question. Row
   * ids are left out — they are the editor's own bookkeeping, not the library.
   */
  const contents = (of: PromptDraft) =>
    JSON.stringify(of.rows.map((row) => [row.key, row.value]));
  const isDirty = initial !== undefined && contents(draft) !== contents(initial);

  const update = (patch: Partial<PromptDraft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    // A previous failure described values that no longer hold.
    setFailure(undefined);
  };

  const submit = async () => {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.length > 0) return;

    setSaving(true);
    try {
      await onSubmit(promptPayloadFrom(draft));
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  const cancel = () => {
    if (isDirty) {
      setConfirmingDiscard(true);
      return;
    }
    onCancel?.();
  };

  /*
   * Whether to mark the identity fields. Only the create caller can get them wrong —
   * locked, they are the resource's own — so a locked form never shows an error on a
   * field the reader cannot change.
   */
  const checkIdentity = submitted && !identityLocked;
  const nameAccepted = RFC1123_SUBDOMAIN.test(draft.name.trim());

  return (
    <Space
      orientation="vertical"
      size="middle"
      css={{ display: "flex", maxWidth: 720 }}
    >
      <Form layout="vertical" component="div">
        <Form.Item
          label="Namespace"
          required={!identityLocked}
          validateStatus={
            checkIdentity && !draft.namespace.trim() ? "error" : undefined
          }
          help={
            checkIdentity && !draft.namespace.trim()
              ? "A namespace is required."
              : undefined
          }
        >
          <Input
            data-testid="prompt-namespace"
            placeholder="kagent"
            value={draft.namespace}
            disabled={identityLocked}
            onChange={(event) => update({ namespace: event.target.value })}
          />
        </Form.Item>

        <Form.Item
          label="Name"
          required={!identityLocked}
          validateStatus={checkIdentity && !nameAccepted ? "error" : undefined}
          help={
            checkIdentity && !draft.name.trim()
              ? "A library name is required."
              : checkIdentity && !nameAccepted
                ? "Lowercase letters, numbers and hyphens only."
                : undefined
          }
        >
          <Input
            data-testid="prompt-name"
            placeholder="team-prompts"
            value={draft.name}
            disabled={identityLocked}
            onChange={(event) => update({ name: event.target.value })}
            css={{ fontFamily: theme.font.mono }}
          />
        </Form.Item>
      </Form>

      <div>
        <Text strong css={{ display: "block", marginBottom: theme.space(1) }}>
          Fragments
        </Text>
        <Text
          data-testid="prompt-fragments-note"
          css={{
            display: "block",
            marginBottom: theme.space(3),
            color: theme.color.textMuted,
          }}
        >
          {identityLocked
            ? /* The replace semantics, said where they apply. `UpdatePromptTemplate`
                 assigns the library's whole `data` map, so a row removed here is a
                 fragment deleted on save, and nothing else on screen says so. */
              "Each key is an include target. Saving replaces this library's fragments with what is below, so a fragment removed here is deleted."
            : "Each key becomes an include target. An agent pulls one in with the tag shown beside it."}
        </Text>
        <FragmentEditor
          rows={draft.rows}
          onChange={(rows) => update({ rows })}
          library={draft.name.trim() || undefined}
          disabled={saving}
        />
      </div>

      {submitted && issues.length > 0 ? (
        <Alert
          type="error"
          showIcon
          title={
            identityLocked
              ? "These changes cannot be saved yet"
              : "This library cannot be created yet"
          }
          data-testid="prompt-form-errors"
          description={
            <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
              {issues.map((issue) => (
                <li key={issue}>{issue}</li>
              ))}
            </ul>
          }
        />
      ) : null}

      {failure !== undefined ? (
        <SubmitError
          error={failure}
          what="prompt library"
          outcome={outcome}
          onRetry={() => void submit()}
          data-testid="prompt-submit-error"
        />
      ) : null}

      <Space size={8}>
        <Button
          type="primary"
          data-testid="prompt-submit"
          loading={saving}
          onClick={() => void submit()}
        >
          {submitLabel}
        </Button>
        {onCancel ? (
          <Button onClick={cancel} data-testid="prompt-cancel">
            Cancel
          </Button>
        ) : (
          <Link to={paths.prompts}>
            <Button data-testid="prompt-cancel">Cancel</Button>
          </Link>
        )}
      </Space>

      {/*
        A controlled modal rather than `Modal.confirm`: antd 6's static methods cannot
        read context, and the warning they log is a failure in the browser suite.
      */}
      <Modal
        open={isConfirmingDiscard}
        title="Discard your changes?"
        okText="Discard"
        okButtonProps={{ danger: true }}
        cancelText="Keep editing"
        onOk={() => {
          setConfirmingDiscard(false);
          onCancel?.();
        }}
        onCancel={() => setConfirmingDiscard(false)}
        data-testid="prompt-discard-modal"
      >
        <Paragraph css={{ margin: 0 }} data-testid="prompt-discard-body">
          This library has edits that have not been saved. Leaving throws them away; the
          library on the cluster is unchanged either way.
        </Paragraph>
      </Modal>
    </Space>
  );
}
