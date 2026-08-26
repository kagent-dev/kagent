import { useState } from "react";
import { Alert, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { apiClient, useNamespaces } from "@/api";
import {
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
} from "@/api/domain/harnesses";
import { paths } from "@/router/routes";

const { Text, Paragraph } = Typography;

/**
 * Creating a harness — the runtime half of an agent.
 *
 * This surface did not exist because the client offered no create, and a note in the
 * codebase said `HarnessService` was read-only. That was wrong: the service implements
 * create, update and delete, and always did. What was read-only was this application.
 *
 * The form is short because the CRD is strict, and every constraint below is one the
 * cluster enforces with CEL rather than something invented here:
 *
 * - exactly one adapter — `kagent`, `codex` or `claude`;
 * - `workload.image` pinned by sha256 digest, because a tag can move under a running
 *   agent and the CRD refuses one outright;
 * - a worker pool name, which is where this harness's Substrate Actors are scheduled.
 *
 * The admission selector is optional to the CRD and asked for here anyway, with a
 * warning when it is left empty: a harness that admits no templates is legal, runs
 * nothing, and gives no sign of why.
 */
export function HarnessNewPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();

  const [namespace, setNamespace] = useState<string>();
  const [name, setName] = useState("");
  const [adapter, setAdapter] = useState<HarnessAdapter>("kagent");
  const [image, setImage] = useState("");
  const [workerPool, setWorkerPool] = useState("");
  const [snapshotLocation, setSnapshotLocation] = useState("");
  const [selectorKey, setSelectorKey] = useState("");
  const [selectorValue, setSelectorValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();

  const imagePinned = HARNESS_IMAGE_PATTERN.test(image.trim());
  const admitsNothing = selectorKey.trim() === "" || selectorValue.trim() === "";
  // The snapshot location counts, because the CRD requires it. Left out of this
  // guard the form submitted happily and the controller answered "Invalid Harness",
  // which names neither the field nor what was wrong with it.
  const ready =
    Boolean(namespace) &&
    name.trim() !== "" &&
    imagePinned &&
    workerPool.trim() !== "" &&
    snapshotLocation.trim() !== "";

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
          spec: {
            // Exactly one, which is what the CRD's own rule requires.
            [adapter]: {},
            workload: { image: image.trim() },
            substrate: {
              workerPoolRef: { name: workerPool.trim() },
              snapshotPolicy: { location: snapshotLocation.trim() },
            },
            ...(admitsNothing
              ? {}
              : {
                  allowedAgentTemplates: {
                    selector: { matchLabels: { [selectorKey.trim()]: selectorValue.trim() } },
                  },
                }),
          },
        },
      });
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
      description="A harness is the runtime an agent runs on. It admits agent templates by label, and its Substrate Actors are scheduled onto a worker pool."
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

          <Form.Item
            label="Runtime adapter"
            required
            extra="Exactly one, which the CRD enforces. It decides how a template is compiled into something runnable."
          >
            <Select<HarnessAdapter>
              data-testid="harness-adapter"
              value={adapter}
              onChange={setAdapter}
              options={HARNESS_ADAPTERS.map((value) => ({ value, label: value }))}
            />
          </Form.Item>

          <Form.Item
            label="Workload image"
            required
            validateStatus={image.trim() !== "" && !imagePinned ? "error" : undefined}
            help={
              image.trim() !== "" && !imagePinned
                ? "Pin the image by digest — a tag is rejected by the cluster, because it can move under a running agent."
                : "Pinned by sha256 digest, for example ghcr.io/example/runtime@sha256:…"
            }
          >
            <Input
              data-testid="harness-image"
              value={image}
              onChange={(event) => setImage(event.target.value)}
              placeholder="ghcr.io/example/runtime@sha256:…"
            />
          </Form.Item>

          <Form.Item
            label="Worker pool"
            required
            extra="Where this harness's Substrate Actors are scheduled. A pool in the same namespace."
          >
            <Input
              data-testid="harness-worker-pool"
              value={workerPool}
              onChange={(event) => setWorkerPool(event.target.value)}
              placeholder="kagent-default"
            />
          </Form.Item>

          <Form.Item
            label="Snapshot location"
            required
            extra="Where Substrate stores runtime snapshots."
          >
            <Input
              data-testid="harness-snapshot"
              value={snapshotLocation}
              onChange={(event) => setSnapshotLocation(event.target.value)}
              placeholder="gs://snapshots/kagent/"
            />
          </Form.Item>

          <Form.Item label="Admits agent templates labelled">
            <Space size={8}>
              <Input
                data-testid="harness-selector-key"
                value={selectorKey}
                onChange={(event) => setSelectorKey(event.target.value)}
                placeholder="key"
              />
              <Text css={{ color: theme.color.textMuted }}>=</Text>
              <Input
                data-testid="harness-selector-value"
                value={selectorValue}
                onChange={(event) => setSelectorValue(event.target.value)}
                placeholder="value"
              />
            </Space>
            {admitsNothing ? (
              <Alert
                css={{ marginTop: theme.space(2) }}
                type="warning"
                showIcon
                data-testid="harness-admits-nothing"
                title="This harness will admit no templates"
                description="A harness with no selector admits none — the CRD says so. It will be created and will run nothing, with no sign on any page of why."
              />
            ) : null}
          </Form.Item>

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
