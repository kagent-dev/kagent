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
  useAgentTemplates,
  type AgentTemplateHarnessStatus,
} from "@/api";

const { Text, Paragraph } = Typography;

const TAB_PARAM = "tab";

/**
 * One agent template: what it is, what runs it, and how to stop it existing.
 *
 * ## Why this is a details page and not the edit form
 *
 * Because clicking a row to *look* at something and landing in a page of inputs with
 * Save waiting makes editing the default and reading the deliberate act, which is the
 * wrong way round. Every field being an input also says the values are provisional
 * when they are the cluster's. So reading is the page, and editing is a mode of it.
 *
 * The fields are the *same component* either way — `AgentTemplateForm` with
 * `readOnly` — deliberately, and not as a convenience. Two renderings of one spec
 * drift, and the one nobody edits is the one that quietly stops showing a field the
 * CRD gained; a reader would then see a template that looks complete and is not.
 *
 * ## The two tabs
 *
 * **Details** is the spec. **Agents** is `status.harnesses[]`: one row per
 * `(template, harness)` pair, which is the durable runnable thing — a template on its
 * own does nothing, and an `AgentInstance` is one conversation with a pair rather
 * than the agent itself. That list is free here because the same field drives the
 * "Runs on" column on the templates list.
 *
 * ## What is deliberately missing
 *
 * A conversation count per pair. The column is present and says so rather than being
 * left out, because a tab that answers "what runs this" without answering "is anything
 * actually using it" should say which question it is not answering. See the column
 * itself for what the API can and cannot do here — it is not what it was thought to be.
 *
 * ## Admission is on the Details tab, not behind Edit
 *
 * "No harness will run this template" is the single most important fact about a
 * template and the one nothing about the template itself reveals. A reader who has to
 * press Edit to discover it is a reader who will not discover it.
 */
export function AgentTemplateDetailsPage() {
  const navigate = useNavigate();
  const theme = useTheme();
  const { namespace, name } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();

  const template = useAgentTemplate(namespace, name);
  /*
   * The list this page returns to.
   *
   * Held so a delete can invalidate it before navigating: the list is cached, and
   * landing on it without re-reading shows the template that was just removed — which
   * reads as a delete that silently failed.
   */
  const templates = useAgentTemplates(namespace);

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

  /*
   * Every agent this template is half of, one row per harness that admits it.
   *
   * Built from `admittingHarnesses` rather than straight off
   * `status.harnesses[]`, because those are two different claims and only the first
   * is guaranteed. `admittingHarnesses` is what the service resolved and what the
   * "Runs on" column already trusts; `status.harnesses[]` is the *detail* the
   * controller recorded for each pair, and it can be absent while admission is not —
   * a pair the controller has admitted but not yet reconciled has no conditions and
   * no revision. Reading only the second renders "no agent exists for this template"
   * over a template that plainly has one, which is the wrong answer stated
   * confidently.
   *
   * So admission decides the rows and the status supplies what it has.
   */
  const pairs: AgentTemplateHarnessStatus[] = useMemo(() => {
    if (!template.data) return [];
    const reported = new Map(
      (template.data.resource.status?.harnesses ?? []).map((entry) => [
        entry.harness,
        entry,
      ]),
    );
    const rows = template.data.admittingHarnesses.map(
      (harness) => reported.get(harness) ?? { harness },
    );
    // A pair the controller reported that admission no longer lists is a real state —
    // a template whose labels have just changed — and dropping it would hide an agent
    // that still exists.
    for (const [harness, entry] of reported) {
      if (!template.data.admittingHarnesses.includes(harness)) rows.push(entry);
    }
    return rows;
  }, [template.data]);

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
      // as a save that did not take.
      await template.refresh();
      await templates.refresh();
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

  /** Back to the list, having re-read it — this page is about an object that is gone. */
  async function afterDelete(): Promise<void> {
    await templates.refresh();
    // See the note on the same navigation after a create: the list narrows on `ns`, and
    // the bare `/agent-templates` route is a redirect that carries no query string.
    navigate(`${agentTemplatesTab}&ns=${encodeURIComponent(namespace ?? "")}`);
  }

  const problems = draft ? draftProblems(draft, { isCreate: false }) : [];

  const pairColumns = useMemo<ColumnsType<AgentTemplateHarnessStatus>>(
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
            namespace: template.data?.namespace,
            agentTemplate: template.data?.name,
            harness: bareHarnessName(row.harness),
          });
          const label = (
            <span css={{ fontFamily: theme.font.mono, fontSize: 13 }}>{row.harness}</span>
          );
          return href ? (
            <Link to={href} data-testid={`template-agent-link-${bareHarnessName(row.harness)}`}>
              {label}
            </Link>
          ) : (
            label
          );
        },
      },
      {
        /*
         * The controller's own `Ready` condition for this pair. Its reason is carried
         * verbatim — `ActorTemplatePending` and `Ready` are what the cluster says, and
         * paraphrasing them would make the message unsearchable against controller logs.
         */
        title: "Revision state",
        key: "state",
        render: (_, row) => {
          const ready = (row.conditions ?? []).find((entry) => entry.type === "Ready");
          if (!ready) {
            return (
              <Tooltip title="The controller has recorded no Ready condition for this pair yet. That is not a failure — a pair it has not observed looks exactly like this.">
                <Tag>Not reported</Tag>
              </Tooltip>
            );
          }
          return (
            <Tooltip title={ready.message}>
              <Tag color={ready.status === "True" ? "success" : "warning"}>
                {ready.status === "True" ? "Ready" : (ready.reason ?? "Not ready")}
              </Tag>
            </Tooltip>
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
          const successful = row.latestSuccessfulRevision;
          const desired = row.desiredRevision;
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
         * This was a seam, on the reasoning that `ListAgentInstances` could not answer
         * "conversations with *this* template". Two corrections since. An instance does
         * carry labels — its template's, copied at create — but `match_labels` still
         * cannot answer it: admission labels are shared by construction, so filtering on
         * one returns every template that harness admits. What closed the seam is the
         * `agent_template` / `harness` filter that landed, which resolves through the
         * prepared revision.
         *
         * One read per row is affordable *here* and nowhere else: a template has a
         * handful of pairs. The same per-row read on the agents list would be one
         * request per row.
         */
        title: "Conversations",
        key: "conversations",
        width: 160,
        render: (_: unknown, pair: AgentTemplateHarnessStatus) => (
          <PairConversationCount
            namespace={template.data?.namespace}
            agentTemplate={template.data?.name}
            harness={bareHarnessName(pair.harness)}
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
            // The count the Agents tab already has, in the prompt where
            // the decision is actually made.
            //
            // `pairs` counts harnesses that admit this template, and under
            // this model a (template, harness) pair *is* an agent — so the
            // noun is right. What it must not claim is that those agents
            // keep running: deleting the template retires the pair, which
            // is precisely what stops new conversations being started. The
            // things that keep running are the conversations already open,
            // each holding a prepared revision the collector retains for
            // it. Measured on a cluster, not inferred from the schema.
            description={
              <span
                css={{ display: "inline-block", maxWidth: 320 }}
                data-testid="template-delete-consequence"
              >
                {pairs.length === 0
                  ? "No harness admits this template, so no agent was ever built from it. "
                  : pairs.length === 1
                    ? "1 agent is built from this template. Conversations already open with it keep working; no new one can be started. "
                    : `${pairs.length} agents are built from this template. Conversations already open with them keep working; no new ones can be started. `}
                This cannot be undone.
              </span>
            }
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
            {/*
              What the controller made of it, which is the half a form cannot show:
              whether a harness admits it, and whether a revision was prepared. Above
              the tabs, because it is true of the template rather than of either tab —
              and on screen whether or not the reader has pressed Edit.
            */}
            <Space size={8} wrap data-testid="template-admission-status">
              <Text css={{ color: theme.color.textMuted }}>Runs on</Text>
              {template.data.admittingHarnesses.length > 0 ? (
                template.data.admittingHarnesses.map((harness) => (
                  <Tag key={harness} color="success">
                    {harness}
                  </Tag>
                ))
              ) : (
                <Tag color="warning">No harness — no agent can be created from it</Tag>
              )}
              {/*
                A draft is not lost by moving between tabs — it lives on this page, not
                in the tab — but a reader who wandered off mid-edit should be able to
                see that from wherever they are.
              */}
              {isDirty ? (
                <Tag color="warning" data-testid="template-unsaved">
                  Unsaved changes
                </Tag>
              ) : null}
            </Space>

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
                        An agent is this template paired with a harness. The pair is the
                        durable runnable thing — a conversation is one instance cut from
                        it — so this is every agent that exists because of this template.
                      </Paragraph>

                      <Table<AgentTemplateHarnessStatus>
                        data-testid="template-agents-table"
                        rowKey={(row) => row.harness}
                        columns={pairColumns}
                        dataSource={pairs}
                        pagination={false}
                        locale={{
                          emptyText:
                            "No harness admits this template, so no agent exists for it. A harness admits templates through a label selector — add the label it selects on from the Details tab.",
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

/**
 * How many conversations are open with one agent.
 *
 * A count, not a list: the question this column answers is "is anything actually using
 * this?", which the pair list cannot — a pair exists the moment a harness admits the
 * template, whether or not anyone has ever talked to it.
 *
 * A failed read says so rather than rendering zero. Zero and "could not tell" are
 * different answers, and the one that matters here is the one a reader would act on: a
 * template that looks unused is a template someone deletes.
 */
function PairConversationCount({
  namespace,
  agentTemplate,
  harness,
}: {
  namespace: string | undefined;
  agentTemplate: string | undefined;
  harness: string | undefined;
}) {
  const theme = useTheme();
  const conversations = useAgentConversations(namespace, agentTemplate, harness);

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

/** `namespace/name` → `name`; a pair's status may carry either form. */
function bareHarnessName(harness: string | undefined): string | undefined {
  if (!harness) return undefined;
  const slash = harness.indexOf("/");
  return slash === -1 ? harness : harness.slice(slash + 1);
}
