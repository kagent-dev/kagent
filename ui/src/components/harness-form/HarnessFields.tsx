import { Alert, Input, Select } from "antd";
import { useTheme } from "@emotion/react";
import { DEFAULT_HARNESS_WORKER_POOL, HARNESS_ADAPTERS, type HarnessAdapter } from "@/api/domain/harnesses";
import { FormField } from "@/components/form/FormField";
import { ListInput } from "@/components/form/ListInput";
import type { FieldValidation } from "@/components/form/useSubmitValidation";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import { HARNESS_ADAPTER_LABELS, adapterSupportsCommand, type HarnessDraft, type HarnessDraftField } from "./harnessDraft";

/** What a Harness is, for the pages and dialogs that create one. */
export function HarnessIntro() {
  const theme = useTheme();
  return (
    <Alert
      type="info"
      showIcon
      data-testid="harness-intro"
      title="A harness is where an agent runs — not what it does"
      description="It sets the adapter, the image and the worker pool. Many agents can share one."
      css={{ marginBottom: theme.space(4) }}
    />
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
  fieldFor,
}: {
  draft: HarnessDraft;
  onChange: (next: HarnessDraft) => void;
  pools: WorkerPoolChoices;
  fieldFor: (field: HarnessDraftField) => FieldValidation;
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
        extra="Decides how the agent template is compiled and which program runs it."
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
        {...fieldFor("image")}
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
            {...fieldFor("command")}
            extra={
              byo
                ? "Required for bring-your-own images. Spaces split command parts. Quotes keep spaces."
                : "Overrides the image entrypoint. Spaces split command parts. Quotes keep spaces."
            }
          >
            <ListInput
              id="harness-command"
              split="spaces"
              data-testid="harness-command"
              value={draft.command}
              onChange={(value) => set("command", value)}
              placeholder="/app/server"
            />
          </FormField>

          <FormField
            label="Arguments"
            required={false}
            htmlFor="harness-args"
            extra="Replaces the image arguments. Spaces split arguments. Quotes keep spaces."
          >
            <ListInput
              id="harness-args"
              split="spaces"
              data-testid="harness-args"
              value={draft.args}
              onChange={(value) => set("args", value)}
              placeholder="--port=8080"
            />
          </FormField>
        </>
      )}

      <FormField
        label="Worker pool"
        required
        htmlFor="harness-worker-pool"
        {...fieldFor("workerPool")}
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
        {...fieldFor("snapshotLocation")}
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
