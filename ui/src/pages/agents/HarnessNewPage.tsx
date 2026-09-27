import { useState } from "react";
import { Alert, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { apiClient, useInvalidateHarnesses, useNamespaces } from "@/api";
import {
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
} from "@/api/domain/harnesses";
import { paths } from "@/router/routes";

const { Paragraph } = Typography;


export function HarnessNewPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const invalidateHarnesses = useInvalidateHarnesses();

  const [namespace, setNamespace] = useState<string>();
  const [name, setName] = useState("");
  const [adapter, setAdapter] = useState<HarnessAdapter>("kagent");
  const [image, setImage] = useState("");
  const [command, setCommand] = useState<string[]>([]);
  const [args, setArgs] = useState<string[]>([]);
  const [workerPool, setWorkerPool] = useState("");
  const [snapshotLocation, setSnapshotLocation] = useState("");
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();

  const byo = adapter === "byo";
  const imagePinned = HARNESS_IMAGE_PATTERN.test(image.trim());
  // The snapshot location counts, because the CRD requires it. Left out of this
  // guard the form submitted happily and the controller answered "Invalid Harness",
  // which names neither the field nor what was wrong with it.
  const ready =
    Boolean(namespace) &&
    name.trim() !== "" &&
    imagePinned &&
    (!byo || command.length > 0) &&
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
            workload: {
              image: image.trim(),
              ...(command.length > 0 ? { command } : {}),
              ...(args.length > 0 ? { args } : {}),
            },
            substrate: {
              workerPoolRef: { name: workerPool.trim() },
              snapshotPolicy: { location: snapshotLocation.trim() },
            },
          },
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
            label="Command"
            required={byo}
            extra={
              byo
                ? "Required for bring-your-own images. Press Enter after each part."
                : "Overrides the image entrypoint. Press Enter after each part."
            }
          >
            <Select
              mode="tags"
              data-testid="harness-command"
              value={command}
              onChange={setCommand}
              open={false}
              suffixIcon={null}
              placeholder="/app/server"
            />
          </Form.Item>

          <Form.Item label="Arguments" extra="Overrides the image arguments. Press Enter after each one.">
            <Select
              mode="tags"
              data-testid="harness-args"
              value={args}
              onChange={setArgs}
              open={false}
              suffixIcon={null}
              placeholder="--port=8080"
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
