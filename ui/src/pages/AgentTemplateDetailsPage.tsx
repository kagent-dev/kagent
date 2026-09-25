import { useMemo, useState } from "react";
import {
  Alert,
  Button,
  Modal,
  Skeleton,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import toast from "react-hot-toast";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTheme } from "@emotion/react";
import { Pencil } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { AgentTemplateForm } from "@/components/agent-template-form/AgentTemplateForm";
import { hasUnshownSpecFields } from "@/components/agent-template-form/unshownFields";
import {
  draftFromTemplate,
  draftProblems,
  labelsFromDraft,
  specFromDraft,
  type AgentTemplateDraft,
} from "@/components/agent-template-form/agentTemplateDraft";
import { agentTemplatesTab } from "@/router/routes";
import {
  apiClient,
  useAgentConversations,
  isNotFound,
  useAgentTemplate,
  useInvalidateAgentTemplates,
  type Agent,
  useAgentsAcrossNamespaces,
} from "@/api";
import { pairRevisionCondition } from "./agentTemplateRevision";

const { Text, Paragraph } = Typography;

const TAB_PARAM = "tab";


export function AgentTemplateDetailsPage() {
  const navigate = useNavigate();
  const theme = useTheme();
  const { namespace, name } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  const template = useAgentTemplate(namespace, name);
  const invalidateTemplates = useInvalidateAgentTemplates();

  const [isSubmitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();

  /*
   * The draft is *derived* from the loaded template until the reader edits it.
   *
   * Carried over unchanged from the edit page this replaces, and for the reason it was
   * written there: seeding it from an effect would be a `setState` inside one, and it
   * would re-seed on every revalidation, discarding whatever had been typed. That is
   * the bug an edit form is most likely to have and least likely to notice.
   *
   * The edit is stamped with the template it belongs to, so opening a different
   * template shows that template rather than the previous one's half-finished changes.
   */
  const ref = template.data?.ref;
  const [edited, setEdited] = useState<{ ref: string; draft: AgentTemplateDraft }>();
  const loaded = useMemo(
    () => (template.data ? draftFromTemplate(template.data) : undefined),
    [template.data],
  );
  const draft = edited && edited.ref === ref ? edited.draft : loaded;
  const setDraft = (next: AgentTemplateDraft) =>
    setEdited({ ref: ref ?? "", draft: next });

  /*
   * Edit mode is keyed by the template too, for the same reason the draft is: a page
   * that stayed in edit mode across a navigation would open the *next* template ready
   * to be changed, which nobody asked for.
   */
  const [editingRef, setEditingRef] = useState<string>();
  const isEditing = ref !== undefined && editingRef === ref;

  /*
   * Whether the draft differs from what was loaded.
   *
   * Compared by value rather than tracked with a flag: a reader who types a character
   * and deletes it again has not changed anything, and being asked to confirm a
   * discard of nothing teaches them to click through the prompt.
   */
  const isDirty =
    isEditing &&
    draft !== undefined &&
    loaded !== undefined &&
    JSON.stringify(draft) !== JSON.stringify(loaded);

  const [isConfirmingDiscard, setConfirmingDiscard] = useState(false);

  const activeTab = searchParams.get(TAB_PARAM) === "agents" ? "agents" : "details";
  const setTab = (tab: string) => {
    const next = new URLSearchParams(searchParams);
    if (tab === "details") next.delete(TAB_PARAM);
    else next.set(TAB_PARAM, tab);
    setSearchParams(next, { replace: true });
  };

  const missing = template.error !== undefined && isNotFound(template.error);

  const agents = useAgentsAcrossNamespaces(namespace ? [namespace] : undefined);
  const pairs = useMemo(() => (agents.data?.agents ?? []).filter(agent =>
    agent.resource.spec.templateRef?.name === name), [agents.data, name]);

  /** Leaves edit mode, discarding the draft. Asks first when there is one to lose. */
  function stopEditing() {
    if (isDirty) {
      setConfirmingDiscard(true);
      return;
    }
    discardEdit();
  }

  function discardEdit() {
    setEditingRef(undefined);
    setEdited(undefined);
    setError(undefined);
    setConfirmingDiscard(false);
  }

  async function save(): Promise<void> {
    if (!draft || !template.data) return;
    setSubmitting(true);
    setError(undefined);
    try {
      const updated = await apiClient.agentBuildingBlocks.updateAgentTemplate({
        namespace: template.data.namespace,
        name: template.data.name,
        resource: {
          metadata: {
            ...template.data.resource.metadata,
            name: template.data.name,
            namespace: template.data.namespace,
            labels: labelsFromDraft(draft),
          },
          // Merged onto the spec that was read, so skills, plugins and prompt data
          // sources survive an edit that never mentioned them.
          spec: specFromDraft(draft, template.data.resource.spec),
        },
      });
      // Re-read before leaving edit mode, so the read-only view below is the saved
      // copy. The other order shows the values that were just replaced, which reads
      // as a save that did not take. The sweep reaches the list and the agents derived
      // from it, which this page's own read does not.
      // Swallowed: `refresh` rethrows, so an unguarded re-read here would land in the
      // catch below and report a save that succeeded as one that failed.
      await template.refresh().catch(() => {});
      await invalidateTemplates();
      toast.success(`Agent template ${updated.name} saved`);
      setEditingRef(undefined);
      setEdited(undefined);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSubmitting(false);
    }
  }

  async function remove(): Promise<void> {
    if (!template.data) return;
    await apiClient.agentBuildingBlocks.removeAgentTemplate(
      template.data.namespace,
      template.data.name,
    );
  }

  /** Back to the list — this page is about an object that is gone. */
  async function afterDelete(): Promise<void> {
    await invalidateTemplates();
    // See the note on the same navigation after a create: the list narrows on `ns`, and
    // the bare `/agent-templates` route is a redirect that carries no query string.
    navigate(`${agentTemplatesTab}&ns=${encodeURIComponent(namespace ?? "")}`);
  }

  const problems = draft ? draftProblems(draft, { isCreate: false }) : [];

  const pairColumns = useMemo<ColumnsType<Agent>>(
    () => [
      {
        /*
         * A link, because the row *is* an agent.
         *
         * This tab answers "what is built from this template", and each answer is a
         * (template, harness) pair — which is exactly what an agent is here, and which
         * already has an address. Leaving it as text made the tab a dead end: it names
         * the thing a reader wants and gives them no way to reach it, so they go back to
         * the agents list and find it again by hand.
         *
         * The harness may arrive qualified as `namespace/name`; the route wants the bare
         * name, and the template's own namespace is the pair's.
         */
        title: "Agent",
        key: "harness",
        render: (_, row) => {
          const href = agentPageUrl({
            namespace: row.namespace, name: row.name,
          });
          const label = (
            <span css={{ fontFamily: theme.font.mono, fontSize: 13 }}>{row.name}</span>
          );
          return href ? (
            <Link to={href} data-testid={`template-agent-link-${row.name}`}>
              {label}
            </Link>
          ) : (
            label
          );
        },
      },
      {
        /*
         * The earliest failed controller stage for this pair, or Ready when none failed.
         * Looking only for Ready hides compiler failures: structured output on a Codex
         * or Claude harness, for example, stops at Compatible=False and has no Ready
         * condition to display. The controller's reason and message are carried verbatim
         * so the UI cannot drift from the compiler's compatibility decision.
         */
        title: "Revision state",
        key: "state",
        render: (_, row) => {
          const conditions = row.resource.status?.conditions ?? [];
          const condition = pairRevisionCondition(conditions);
          if (!condition) {
            return (
              <Tooltip title="The controller has recorded no Ready condition for this pair yet. That is not a failure — a pair it has not observed looks exactly like this.">
                <Tag>Not reported</Tag>
              </Tooltip>
            );
          }
          return (
            <Space orientation="vertical" size={2}>
              <Tag
                color={
                  condition.status === "True"
                    ? "success"
                    : condition.type === "Ready"
                      ? "warning"
                      : "error"
                }
              >
                {condition.status === "True"
                  ? "Ready"
                  : (condition.reason ?? `${condition.type} failed`)}
              </Tag>
              {condition.message ? (
                <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                  {condition.message}
                </Text>
              ) : null}
            </Space>
          );
        },
      },
      {
        title: "Prepared revision",
        key: "revision",
        render: (_, row) => {
          // The revision an agent would be cut from *now*. `desiredRevision` without a
          // successful one means the pair is still being prepared, which is a different
          // state from having none — so they are shown as different things.
          const successful = row.resource.status?.latestSuccessfulRevision;
          const desired = row.resource.status?.desiredRevision;
          if (successful) {
            return (
              <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
                {successful.slice(0, 12)}
              </Text>
            );
          }
          return (
            <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
              {desired ? `preparing ${desired.slice(0, 12)}` : "none yet"}
            </Text>
          );
        },
      },
      {
        /*
         * Counted, and only because the server can narrow it.
         *
         * The agent_template / harness filters resolve through the prepared revision
         * to count conversations for this exact pair.
         *
         * One read per row is affordable *here* and nowhere else: a template has a
         * handful of pairs. The same per-row read on the agents list would be one
         * request per row.
         */
        title: "Conversations",
        key: "conversations",
        width: 160,
        render: (_: unknown, pair: Agent) => (
          <PairConversationCount
            namespace={template.data?.namespace}
            name={pair.name}
          />
        ),
      },
    ],
    [theme, template.data?.namespace, template.data?.name],
  );

  return (
    <PageFrame
      title={name ? `Agent template ${name}` : "Agent template"}
      description={
        namespace && !missing
          ? `What this agent does, in the ${namespace} namespace.`
          : undefined
      }
      actions={
        <Space size={8}>
          {template.data && !isEditing ? (
            <Button
              icon={<Pencil size={14} />}
              onClick={() => setEditingRef(ref)}
              data-testid="template-edit"
            >
              Edit
            </Button>
          ) : null}
          {/*
            In the header rather than at the foot of the page.

            It used to sit below a rule under the form, which put a destructive action
            somewhere a reader only reaches by scrolling past everything else — and made
            it read as a footnote rather than as one of this page's actions. Outlined for
            the same reason: a text button next to "Edit" and "Back" reads as a link, and
            a link is what people click while meaning to navigate.

            More prominent means the confirmation is doing more work than before, so the
            measured consequence it carries matters more, not less — see the description
            below, which is read off a delete actually performed against a cluster.
          */}
          {template.data ? (
            <DeleteResourceButton
            kind="agent template"
            name={template.data.name}
            onDelete={remove}
            onDeleted={afterDelete}
            label="Delete template"
            outlined
            description="Agents referencing this template cannot prepare new configuration until it is restored or replaced. Existing conversations and last successful revisions are retained."
            />
          ) : null}
          <Button onClick={() => navigate(agentTemplatesTab)}>Back to templates</Button>
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 900 }}>
        {missing ? (
          <Alert
            type="warning"
            showIcon
            title="This agent template does not exist"
            description={`No template ${name ?? ""} was found in ${namespace ?? "that namespace"}. It may have been deleted.`}
            data-testid="template-not-found"
          />
        ) : template.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this agent template"
            description={template.error.message}
            data-testid="template-load-error"
            action={
              <Button size="small" onClick={() => void template.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {error ? (
          <Alert
            type="error"
            showIcon
            title="Could not save the agent template"
            description={error}
            data-testid="template-save-error"
          />
        ) : null}

        {template.isLoading ? (
          <Skeleton active paragraph={{ rows: 8 }} data-testid="template-loading" />
        ) : null}

        {draft && template.data ? (
          <>
            {isDirty ? <Tag color="warning" data-testid="template-unsaved">Unsaved changes</Tag> : null}
            {agents.error ? <Alert type="error" title="Could not load Agents using this template" description={agents.error.message} /> : null}
            <Tabs
              activeKey={activeTab}
              onChange={setTab}
              data-testid="template-tabs"
              items={[
                {
                  key: "details",
                  label: "Details",
                  children: (
                    <Space
                      orientation="vertical"
                      size="middle"
                      css={{ display: "flex" }}
                    >
                      <div data-testid="template-details">
                        {/* The same component in both modes. See this page's note on
                            why a separate read-only view is the wrong shape. */}
                        <AgentTemplateForm
                          draft={draft}
                          onChange={setDraft}
                          isCreate={false}
                          namespace={template.data.namespace}
                          readOnly={!isEditing}
                          hasUnshownFields={hasUnshownSpecFields(
                            template.data.resource.spec,
                          )}
                        />
                      </div>

                      {isEditing ? (
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
                            onClick={() => void save()}
                            data-testid="template-submit"
                          >
                            Save template
                          </Button>
                          <Button onClick={stopEditing} data-testid="template-stop-editing">
                            Cancel
                          </Button>
                        </div>
                      ) : null}
                    </Space>
                  ),
                },
                {
                  key: "agents",
                  label: `Agents (${pairs.length})`,
                  children: (
                    <Space
                      orientation="vertical"
                      size="middle"
                      css={{ display: "flex" }}
                    >
                      <Paragraph
                        css={{ margin: 0, color: theme.color.textMuted, fontSize: 12 }}
                      >
                        Agents that directly reference this reusable template. Child-template references can also reuse it.
                      </Paragraph>

                      <Table<Agent>
                        data-testid="template-agents-table"
                        rowKey={(row) => row.name}
                        columns={pairColumns}
                        dataSource={pairs}
                        pagination={false}
                        locale={{
                          emptyText:
                            "No Agent directly references this template.",
                        }}
                      />
                    </Space>
                  ),
                },
              ]}
            />
          </>
        ) : null}
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
        onOk={discardEdit}
        onCancel={() => setConfirmingDiscard(false)}
        data-testid="template-discard-modal"
      >
        <Paragraph css={{ margin: 0 }} data-testid="template-discard-body">
          This template has edits that have not been saved. Leaving edit mode throws them
          away; the template on the cluster is unchanged either way.
        </Paragraph>
      </Modal>
    </PageFrame>
  );
}


function PairConversationCount({
  namespace,
  name,
}: {
  namespace: string | undefined;
  name: string | undefined;
}) {
  const theme = useTheme();
  const conversations = useAgentConversations(namespace, name);

  if (conversations.isLoading) {
    return <Skeleton.Input active size="small" style={{ width: 60, height: 18 }} />;
  }
  if (conversations.error) {
    return (
      <Tooltip title={conversations.error.message}>
        <Text
          css={{ color: theme.color.warning, fontSize: 12 }}
          data-testid="template-pair-conversations"
        >
          could not read
        </Text>
      </Tooltip>
    );
  }

  const total = conversations.data?.all.length ?? 0;
  const label = total === 1 ? "1 conversation" : `${total} conversations`;

  // A count taken from a narrowed read says so when the read was narrowed. Without
  // this, a reader refused `all_creators` sees a number that is their own share of
  // the conversations and reads it as the total.
  const refused = conversations.data?.widerReadRefused;

  return refused ? (
    <Tooltip title={`Yours only — the wider read was refused: ${refused}`}>
      <Text
        css={{ color: theme.color.textMuted, fontSize: 12 }}
        data-testid="template-pair-conversations"
      >
        {label} (yours)
      </Text>
    </Tooltip>
  ) : (
    <Text
      css={{ color: theme.color.textMuted, fontSize: 12 }}
      data-testid="template-pair-conversations"
    >
      {label}
    </Text>
  );
}
