import { Input, Select, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { DEFAULT_HARNESS_WORKER_POOL, HARNESS_ADAPTERS, type HarnessAdapter } from "@/api/domain/harnesses";
import { FormField } from "@/components/form/FormField";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import { HARNESS_ADAPTER_LABELS, adapterSupportsCommand, type HarnessDraft, type HarnessDraftField } from "./harnessDraft";

/** What a Harness and a WorkerPool are, for the pages and dialogs that create one. */
export function HarnessIntro() {
  const theme = useTheme();
  return (
    <Typography.Paragraph
      data-testid="harness-intro"
      css={{ color: theme.color.textMuted, marginBottom: theme.space(4) }}
    >
      A harness is the runtime an agent runs on: which adapter compiles its template, which
      image runs it, and where Substrate schedules it. Agents reference a harness by name, so
      many agents can share one. A worker pool is a set of Substrate machines that runs those
      agents.
    </Typography.Paragraph>
  );
}

/** The worker pools the harness's namespace has, as read by the form. */
export interface WorkerPoolChoices {
  names: string[];
  loading: boolean;
  error?: string;
}

/**
 * Fields for a platform-managed Harness resource. The worker pool is picked from the
 * namespace's pools; when they cannot be listed, a typed name is accepted instead.
 */
export function HarnessFields({
  draft,
  onChange,
  pools,
  errorFor,
}: {
  draft: HarnessDraft;
  onChange: (next: HarnessDraft) => void;
  pools: WorkerPoolChoices;
  errorFor: (field: HarnessDraftField) => string | undefined;
}) {
  const set = <K extends keyof HarnessDraft>(field: K, value: HarnessDraft[K]) =>
    onChange({ ...draft, [field]: value });
  const byo = draft.adapter === "byo";

  return (
    <>
      <FormField
        label="Runtime adapter"
        required
        htmlFor="harness-adapter"
        extra="How kagent runs the agent: its own runtime, Codex, Claude, or your own image."
      >
        <Select<HarnessAdapter>
          id="harness-adapter"
          data-testid="harness-adapter"
          value={draft.adapter}
          onChange={(value) => set("adapter", value)}
          options={HARNESS_ADAPTERS.map((value) => ({ value, label: HARNESS_ADAPTER_LABELS[value] }))}
        />
      </FormField>

      <FormField
        label="Workload image"
        required={byo}
        htmlFor="harness-image"
        error={errorFor("image")}
        extra={
          byo
            ? "Required for bring-your-own. Pinned by sha256 digest, for example ghcr.io/example/runtime@sha256:…"
            : "Leave empty to use this release's built-in image. An override must be pinned by sha256 digest."
        }
      >
        <Input
          id="harness-image"
          data-testid="harness-image"
          value={draft.image}
          onChange={(event) => set("image", event.target.value)}
          placeholder={byo ? "ghcr.io/example/runtime@sha256:…" : "Release default"}
        />
      </FormField>

      {adapterSupportsCommand(draft.adapter) && (
        <>
          <FormField
            label="Command"
            required={byo}
            htmlFor="harness-command"
            error={errorFor("command")}
            extra={
              byo
                ? "Required for bring-your-own images. Press Enter after each part."
                : "Overrides the image entrypoint. Press Enter after each part."
            }
          >
            <Select
              id="harness-command"
              mode="tags"
              data-testid="harness-command"
              value={draft.command}
              onChange={(value) => set("command", value)}
              open={false}
              suffixIcon={null}
              placeholder="/app/server"
            />
          </FormField>

          <FormField
            label="Arguments"
            required={false}
            htmlFor="harness-args"
            extra="Overrides the image arguments. Press Enter after each one."
          >
            <Select
              id="harness-args"
              mode="tags"
              data-testid="harness-args"
              value={draft.args}
              onChange={(value) => set("args", value)}
              open={false}
              suffixIcon={null}
              placeholder="--port=8080"
            />
          </FormField>
        </>
      )}

      <FormField
        label="Worker pool"
        required
        htmlFor="harness-worker-pool"
        error={errorFor("workerPool")}
        extra={
          pools.error
            ? `Could not list worker pools (${pools.error}). Type the name of one in this namespace.`
            : "The Substrate worker pool, in the harness's namespace, whose machines run this harness's agents."
        }
      >
        {pools.error ? (
          <Input
            id="harness-worker-pool"
            data-testid="harness-worker-pool"
            value={draft.workerPool}
            onChange={(event) => set("workerPool", event.target.value)}
            placeholder={DEFAULT_HARNESS_WORKER_POOL}
          />
        ) : (
          <ResourceSelect
            kind="worker pool"
            id="harness-worker-pool"
            data-testid="harness-worker-pool"
            names={pools.names}
            loading={pools.loading}
            allowTyped
            value={draft.workerPool || undefined}
            onChange={(value) => set("workerPool", value)}
            empty="No worker pools in this namespace"
          />
        )}
      </FormField>

      <FormField
        label="Snapshot location"
        required
        htmlFor="harness-snapshot"
        error={errorFor("snapshotLocation")}
        extra="Object-store path where Substrate saves agent snapshots so agents can resume after restarts."
      >
        <Input
          id="harness-snapshot"
          data-testid="harness-snapshot"
          value={draft.snapshotLocation}
          onChange={(event) => set("snapshotLocation", event.target.value)}
          placeholder="s3://ate-snapshots/kagent/"
        />
      </FormField>
    </>
  );
}
