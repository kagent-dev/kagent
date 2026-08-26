import { useMemo, useState } from "react";
import { Alert, Button, Select, Space, Typography } from "antd";
import toast from "react-hot-toast";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTheme } from "@emotion/react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { AgentTemplateForm } from "@/components/agent-template-form/AgentTemplateForm";
import {
  emptyDraft,
  draftProblems,
  labelsFromDraft,
  specFromDraft,
} from "@/components/agent-template-form/agentTemplateDraft";
import { paths } from "@/router/routes";
import { apiClient, useAgentTemplates, useNamespaces } from "@/api";

const { Text } = Typography;

/**
 * Create an agent template.
 *
 * The page owns the draft and the write; `AgentTemplateForm` owns the fields. That
 * split is what lets the same form be used here and inside the agent-create page's
 * inline panel — the two differ only in what a save *means*, and that decision
 * belongs to the surface, not the fields.
 */
export function AgentTemplateNewPage() {
  const navigate = useNavigate();
  const theme = useTheme();
  const [searchParams] = useSearchParams();
  const namespaces = useNamespaces();

  const fallbackNamespace = useMemo(() => {
    const names = (namespaces.data ?? []).map((entry) => entry.name);
    return names.includes("kagent") ? "kagent" : (names[0] ?? "kagent");
  }, [namespaces.data]);

  const namespace = searchParams.get("namespace") ?? fallbackNamespace;
  /*
   * The list this page is about to navigate to.
   *
   * Held only so it can be re-read after a create: the list is cached, so landing on
   * it without invalidating shows a page that does not contain the thing just made —
   * which reads as a create that silently failed. The same defect exists on three
   * other create pages and is recorded in `playwright/DEFERRED.md`.
   */
  const templates = useAgentTemplates(namespace);
  const [draft, setDraft] = useState(() => emptyDraft(namespace));
  const [isSubmitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();

  // The namespace comes from the URL or from the cluster, and the draft follows it
  // until the reader picks one — after which their choice stands.
  const effective = draft.namespace || namespace;

  async function create(): Promise<void> {
    setSubmitting(true);
    setError(undefined);
    try {
      const created = await apiClient.agentBuildingBlocks.createAgentTemplate({
        namespace: effective,
        name: draft.name.trim(),
        resource: {
          metadata: {
            name: draft.name.trim(),
            namespace: effective,
            labels: labelsFromDraft(draft),
          },
          // No existing spec to merge onto: this is a create.
          spec: specFromDraft(draft),
        },
      });
      await templates.refresh();
      toast.success(`Agent template ${created.name} created`);
      navigate(`${paths.agentTemplates}?namespace=${encodeURIComponent(effective)}`);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSubmitting(false);
    }
  }

  const problems = draftProblems({ ...draft, namespace: effective }, { isCreate: true });

  return (
    <PageFrame
      title="New agent template"
      description="What an agent does. A harness supplies the runtime; an agent is one of each."
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 860 }}>
        {error ? (
          <Alert
            type="error"
            showIcon
            title="Could not create the agent template"
            description={error}
            data-testid="template-create-error"
          />
        ) : null}

        <Space size={8}>
          <Text css={{ color: theme.color.textMuted }}>Namespace</Text>
          <div data-testid="template-form-namespace">
            <Select
              css={{ minWidth: 220 }}
              value={effective}
              loading={namespaces.isLoading}
              onChange={(value: string) =>
                // The model configurations and harnesses on offer are all
                // same-namespace, so changing it changes what the form can select.
                setDraft({ ...draft, namespace: value, modelConfig: "" })
              }
              options={(namespaces.data ?? []).map((entry) => ({
                value: entry.name,
                label: entry.name,
              }))}
            />
          </div>
        </Space>

        <AgentTemplateForm
          draft={{ ...draft, namespace: effective }}
          onChange={setDraft}
          isCreate
          namespace={effective}
        />

        {/*
          The submit gets its own line, above a rule.

          It used to sit directly under the last field of a long form, reading as one
          more row of it — and the form's own "Add a label" button is a small primary
          button too, so the two were competing at the same weight for very different
          consequences.
        */}
        <div
          css={{
            display: "flex",
            gap: theme.space(2),
            paddingTop: theme.space(5),
            borderTop: `1px solid ${theme.color.border}`,
          }}
        >
          <Button
            type="primary"
            loading={isSubmitting}
            disabled={problems.length > 0}
            onClick={() => void create()}
            data-testid="template-submit"
          >
            Create template
          </Button>
          <Button onClick={() => navigate(paths.agentTemplates)}>Cancel</Button>
        </div>
      </Space>
    </PageFrame>
  );
}
