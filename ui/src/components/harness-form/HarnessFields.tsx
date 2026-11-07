import { useEffect, useRef } from "react";
import { AutoComplete, Form, Input, Select, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useWorkerPools } from "@/api";
import {
  DEFAULT_HARNESS_WORKER_POOL,
  HARNESS_ADAPTERS,
  HARNESS_IMAGE_PATTERN,
  type HarnessAdapter,
} from "@/api/domain/harnesses";
import type { HarnessDraft } from "./harnessDraft";

/** What a Harness and a WorkerPool are, for the pages and dialogs that create one. */
export function HarnessIntro() {
  const theme = useTheme();
  return (
    <Typography.Paragraph
      data-testid="harness-intro"
      css={{ color: theme.color.textMuted, fontSize: 13, marginBottom: theme.space(4) }}
    >
      A harness is the runtime an agent runs on: which adapter compiles its template, which
      image runs it, and where Substrate schedules it. Agents reference a harness by name, so
      many agents can share one. A worker pool is a set of Substrate machines that runs those
      agents.
    </Typography.Paragraph>
  );
}

/**
 * Fields for a platform-managed Harness resource.
 *
 * `namespace` is where the harness lives, which is where its worker pool must be. The
 * pools offered are the ones that namespace has; a typed name is accepted too, so a
 * caller who may not list pools is not blocked.
 */
export function HarnessFields({
  draft,
  onChange,
  namespace,
}: {
  draft: HarnessDraft;
  onChange: (next: HarnessDraft) => void;
  namespace?: string;
}) {
  const set = <K extends keyof HarnessDraft>(field: K, value: HarnessDraft[K]) =>
    onChange({ ...draft, [field]: value });
  const byo = draft.adapter === "byo";
  const image = draft.image.trim();
  const imageInvalid = image !== "" && !HARNESS_IMAGE_PATTERN.test(image);
  const pools = useWorkerPools(namespace);
  const poolNames = (pools.data ?? []).map((pool) => pool.name);
  const hasDefaultPool = poolNames.includes(DEFAULT_HARNESS_WORKER_POOL);

  // The chart installs `kagent-default`; preselect it once, and only where it exists, so a
  // field the user clears stays clear.
  const preselected = useRef(false);
  useEffect(() => {
    if (preselected.current || !hasDefaultPool) return;
    preselected.current = true;
    if (draft.workerPool === "") onChange({ ...draft, workerPool: DEFAULT_HARNESS_WORKER_POOL });
  }, [draft, hasDefaultPool, onChange]);

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
        required={byo}
        validateStatus={imageInvalid ? "error" : undefined}
        help={
          imageInvalid
            ? "Pin the image by digest — a tag is rejected by the cluster, because it can move under a running agent."
            : byo
              ? "Required for bring-your-own. Pinned by sha256 digest, for example ghcr.io/example/runtime@sha256:…"
              : "Leave empty to use this release's built-in image. An override must be pinned by sha256 digest."
        }
      >
        <Input
          data-testid="harness-image"
          value={draft.image}
          onChange={(event) => set("image", event.target.value)}
          placeholder={byo ? "ghcr.io/example/runtime@sha256:…" : "Release default"}
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
        extra={
          pools.error
            ? `Could not list worker pools (${pools.error.message}). Type the name of one in this namespace.`
            : "The Substrate worker pool, in the harness's namespace, whose machines run this harness's agents."
        }
      >
        <AutoComplete
          data-testid="harness-worker-pool"
          value={draft.workerPool}
          onChange={(value: string) => set("workerPool", value)}
          options={poolNames.map((value) => ({ value, label: value }))}
          filterOption={(input, option) => Boolean(option?.value.toLowerCase().includes(input.toLowerCase()))}
          placeholder={DEFAULT_HARNESS_WORKER_POOL}
          notFoundContent={
            namespace && !pools.isLoading && !pools.error ? "No worker pools in this namespace" : undefined
          }
        />
      </Form.Item>

      <Form.Item
        label="Snapshot location"
        required
        extra="Object-store path where Substrate saves agent snapshots so agents can resume after restarts."
      >
        <Input
          data-testid="harness-snapshot"
          value={draft.snapshotLocation}
          onChange={(event) => set("snapshotLocation", event.target.value)}
          placeholder="gs://ate-snapshots/kagent/"
        />
      </Form.Item>
    </>
  );
}
