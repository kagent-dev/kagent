import { useMemo, useState } from "react";
import { Form, Input, Space } from "antd";
import { useTheme } from "@emotion/react";
import type { CreatePromptTemplateRequest } from "@/api";
import { SubmitError } from "@/components/common/SubmitError";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import { paths } from "@/router/routes";
import { FragmentEditor } from "./FragmentEditor";
import {
  type PromptDraft,
  emptyPromptDraft,
  promptDraftIssues,
  promptPayloadFrom,
} from "./promptDraft";

interface PromptFormProps {
  onSubmit: (payload: CreatePromptTemplateRequest) => Promise<void>;
  /** Called after a successful save, once the guard has stood down; usually navigates. */
  onSaved: () => void;
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
  onSaved,
  initial,
  outcome = "created",
  submitLabel = "Create",
  identityLocked = false,
  onCancel,
}: PromptFormProps) {
  const theme = useTheme();
  const [draft, setDraft] = useState<PromptDraft>(initial ?? emptyPromptDraft);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const validation = useSubmitValidation(
    useMemo(() => promptDraftIssues(draft, { identityLocked }), [draft, identityLocked]),
  );

  /*
   * Whether the draft differs from what it started as, compared by value: somebody who
   * types a character and deletes it again has not changed anything, and being asked
   * to confirm a discard of nothing teaches them to click through the question. Row
   * ids are left out — they are the editor's own bookkeeping, not the library.
   *
   * The baseline is captured at mount rather than read from the `initial` prop, so a
   * created library counts as work too: its baseline is the empty draft, and a typed
   * name is something to lose.
   */
  const contents = (of: PromptDraft) =>
    JSON.stringify([of.namespace, of.name, of.rows.map((row) => [row.key, row.value])]);
  const [baseline] = useState(() => contents(draft));
  const isDirty = contents(draft) !== baseline;

  // Every way out asks first, Cancel included, so Cancel has no question of its own.
  const { blocker, release } = useUnsavedChangesGuard(isDirty);

  const update = (patch: Partial<PromptDraft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    // A previous failure described values that no longer hold.
    setFailure(undefined);
  };

  const submit = async () => {
    setFailure(undefined);
    if (!validation.attempt()) return;

    setSaving(true);
    try {
      await onSubmit(promptPayloadFrom(draft));
      release();
      onSaved();
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 720 }}>
      {/* Name before namespace: the namespace arrives filled in and is usually left
          alone, so leading with it puts a box to tab past in front of the name. */}
      <Form layout="vertical" component="div">
        <FormField
          label="Name"
          required
          {...validation.field("name")}
          help={identityLocked ? "You cannot change a library's name." : undefined}
        >
          <Input
            data-testid="prompt-name"
            placeholder="team-prompts"
            value={draft.name}
            // `readOnly` keeps the name at full contrast, so it is clear which library this is.
            readOnly={identityLocked}
            onChange={(event) => update({ name: event.target.value })}
            css={{ fontFamily: theme.font.mono }}
          />
        </FormField>

        <FormField
          label="Namespace"
          required
          {...validation.field("namespace")}
          help={identityLocked ? "Libraries stay in the namespace they were created in." : undefined}
        >
          <NamespaceSelect
            data-testid="prompt-namespace"
            value={draft.namespace}
            disabled={identityLocked}
            onChange={(namespace) => update({ namespace })}
          />
        </FormField>

        <FormField
          label="Fragments"
          required
          {...validation.field("fragments")}
          help={
            <span data-testid="prompt-fragments-note">
              {identityLocked
                ? /* `UpdatePromptTemplate` assigns the whole `data` map, so a removed row is deleted on save. */
                  "Each key is an include target. Saving replaces this library's fragments with what is below, so a fragment removed here is deleted."
                : "A fragment is a named piece of prompt text. Agents include it by key."}
            </span>
          }
        >
          <FragmentEditor
            rows={draft.rows}
            onChange={(rows) => update({ rows })}
            library={draft.name.trim() || undefined}
            disabled={saving}
          />
        </FormField>
      </Form>

      <FormActions
        submitLabel={submitLabel}
        onSubmit={() => void submit()}
        submitting={saving}
        cancel={onCancel ? { onClick: onCancel } : { to: paths.prompts }}
        testIdPrefix="prompt"
        blocked={!validation.valid}
        error={
          failure !== undefined ? (
            <SubmitError
              error={failure}
              what="prompt library"
              outcome={outcome}
              onRetry={() => void submit()}
              data-testid="prompt-submit-error"
            />
          ) : null
        }
      />

      <UnsavedChangesModal blocker={blocker} testIdPrefix="prompt" />
    </Space>
  );
}
