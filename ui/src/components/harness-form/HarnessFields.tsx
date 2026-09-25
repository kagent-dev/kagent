import { Form, Input, Select } from "antd";
import {
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
} from "@/api/domain/harnesses";
import type { HarnessDraft } from "./harnessDraft";

/** The harness spec fields, shared by the harness page and an Agent's inline harness. */
export function HarnessFields({
  draft,
  onChange,
}: {
  draft: HarnessDraft;
  onChange: (next: HarnessDraft) => void;
}) {
  const set = <K extends keyof HarnessDraft>(field: K, value: HarnessDraft[K]) =>
    onChange({ ...draft, [field]: value });
  const byo = draft.adapter === "byo";
  const image = draft.image.trim();
  const imageInvalid = image !== "" && !HARNESS_IMAGE_PATTERN.test(image);

  return (
    <>
      <Form.Item
        label="Runtime adapter"
        required
        extra="Exactly one, which the CRD enforces. It decides how a template is compiled into something runnable."
      >
        <Select<HarnessAdapter>
          data-testid="harness-adapter"
          value={draft.adapter}
          onChange={(value) => set("adapter", value)}
          options={HARNESS_ADAPTERS.map((value) => ({ value, label: value }))}
        />
      </Form.Item>

      <Form.Item
        label="Workload image"
        required
        validateStatus={imageInvalid ? "error" : undefined}
        help={
          imageInvalid
            ? "Pin the image by digest — a tag is rejected by the cluster, because it can move under a running agent."
            : "Pinned by sha256 digest, for example ghcr.io/example/runtime@sha256:…"
        }
      >
        <Input
          data-testid="harness-image"
          value={draft.image}
          onChange={(event) => set("image", event.target.value)}
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
          value={draft.command}
          onChange={(value) => set("command", value)}
          open={false}
          suffixIcon={null}
          placeholder="/app/server"
        />
      </Form.Item>

      <Form.Item label="Arguments" extra="Overrides the image arguments. Press Enter after each one.">
        <Select
          mode="tags"
          data-testid="harness-args"
          value={draft.args}
          onChange={(value) => set("args", value)}
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
          value={draft.workerPool}
          onChange={(event) => set("workerPool", event.target.value)}
          placeholder="kagent-default"
        />
      </Form.Item>

      <Form.Item label="Snapshot location" required extra="Where Substrate stores runtime snapshots.">
        <Input
          data-testid="harness-snapshot"
          value={draft.snapshotLocation}
          onChange={(event) => set("snapshotLocation", event.target.value)}
          placeholder="gs://snapshots/kagent/"
        />
      </Form.Item>
    </>
  );
}
