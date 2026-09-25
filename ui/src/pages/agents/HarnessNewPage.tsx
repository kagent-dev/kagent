import { useState } from "react";
import { Alert, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { apiClient, useInvalidateHarnesses, useNamespaces } from "@/api";
import { HarnessFields } from "@/components/harness-form/HarnessFields";
import {
  emptyHarnessDraft,
  harnessDraftProblems,
  harnessSpecFromDraft,
} from "@/components/harness-form/harnessDraft";
import { paths } from "@/router/routes";

const { Paragraph } = Typography;


export function HarnessNewPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const invalidateHarnesses = useInvalidateHarnesses();

  const [namespace, setNamespace] = useState<string>();
  const [name, setName] = useState("");
  const [draft, setDraft] = useState(emptyHarnessDraft);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();

  const ready =
    Boolean(namespace) && name.trim() !== "" && harnessDraftProblems(draft).length === 0;

  async function create() {
    if (!namespace || !ready) return;
    setSaving(true);
    setFailure(undefined);
    try {
      await apiClient.agentBuildingBlocks.createHarness({
        namespace,
        name: name.trim(),
        resource: {
          metadata: { name: name.trim(), namespace },
          spec: harnessSpecFromDraft(draft),
        },
      });
      // Refreshes any list still on screen; SWR does not fetch a key with no mounted
      // subscriber, so the one navigated to re-reads on mount. Guarded, not load-bearing.
      await invalidateHarnesses().catch(() => {});
      navigate(`${paths.agents}?tab=harnesses`);
    } catch (cause: unknown) {
      // The controller's own words: its CEL messages name the field that was wrong,
      // which is more use than anything this form could say about it.
      setFailure(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <PageFrame
      title="New harness"
      description="A harness is the runtime an agent runs on. An Agent references it or embeds its spec."
    >
      <Card size="small" css={{ maxWidth: 720 }}>
        <Form layout="vertical">
          <Form.Item label="Namespace" required>
            <Select
              data-testid="harness-namespace"
              placeholder="Choose a namespace"
              value={namespace}
              onChange={setNamespace}
              loading={namespaces.isLoading}
              options={(namespaces.data ?? []).map((row) => ({
                value: row.name,
                label: row.name,
              }))}
            />
          </Form.Item>

          <Form.Item label="Name" required>
            <Input
              data-testid="harness-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="my-harness"
            />
          </Form.Item>

          <HarnessFields draft={draft} onChange={setDraft} />

          {failure ? (
            <Alert
              type="error"
              showIcon
              data-testid="harness-error"
              title="Could not create this harness"
              description={failure}
              css={{ marginBottom: theme.space(4) }}
            />
          ) : null}

          <Paragraph css={{ color: theme.color.textMuted, fontSize: 12 }}>
            A new harness is not ready straight away: the controller has to observe it
            first, so it appears as “not ready yet” until it has.
          </Paragraph>

          <Space size={8}>
            <Button
              type="primary"
              data-testid="harness-create"
              loading={saving}
              disabled={!ready}
              onClick={() => void create()}
            >
              Create harness
            </Button>
            <Button onClick={() => navigate(`${paths.agents}?tab=harnesses`)}>Cancel</Button>
          </Space>
        </Form>
      </Card>
    </PageFrame>
  );
}
