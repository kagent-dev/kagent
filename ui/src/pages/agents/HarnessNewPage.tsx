import { Alert, Card, Skeleton } from "antd";
import toast from "react-hot-toast";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { useHarnesses } from "@/api";
import { HarnessForm } from "@/components/harness-form/HarnessForm";
import { HarnessIntro } from "@/components/harness-form/HarnessFields";
import { copySource, useMountedBlank } from "@/components/table/RowActionsMenu";
import { CopyNotice } from "@/components/table/CopyNotice";
import { paths } from "@/router/routes";

const harnessesTab = `${paths.agents}?tab=harnesses`;

export function HarnessNewPage() {
  const navigate = useNavigate();
  const from = copySource(useSearchParams()[0]);
  const { namespace, name } = from ?? {};
  // No source, no read: a plain new harness has nothing to fetch.
  const source = useHarnesses(namespace, Boolean(from));
  const copyOf = source.data?.find((row) => row.namespace === namespace && row.name === name);
  const waiting = Boolean(from) && !source.error && !source.data;
  const mountedBlank = useMountedBlank(from && `${from.namespace}/${from.name}`, !waiting, Boolean(copyOf));
  return (
    <PageFrame
      title="New harness"
      description="A harness is the runtime an agent runs on. Agents reference it by name."
    >
      <Card size="small" css={{ maxWidth: 720 }}>
        <HarnessIntro />
        {/* A source that cannot be had still leaves a form to fill in: the alert says why it is blank. */}
        {namespace && (copyOf ? mountedBlank : source.error && !source.data) ? (
          <CopyNotice kind="harness" from={from} error={source.error?.message} loaded={Boolean(copyOf)} mountedBlank={mountedBlank} testId="harness-copy-error" />
        ) : namespace && source.data && !copyOf ? (
          <Alert type="warning" showIcon title="Harness not found" description={`No harness ${namespace}/${name} to copy. Starting from a blank form.`} data-testid="harness-copy-not-found" css={{ marginBottom: 16 }} />
        ) : null}
        {waiting ? (
          <Skeleton active paragraph={{ rows: 8 }} />
        ) : (
        <HarnessForm
          // Keyed on the requested source, so a late-arriving one never remounts an edited form.
          key={from ? `${from.namespace}/${from.name}` : "new"}
          copyOf={copyOf}
          onSaved={(saved) => {
            toast.success(`Harness ${saved.name} created`);
            void navigate(harnessesTab);
          }}
          onCancel={() => void navigate(harnessesTab)}
        />
        )}
      </Card>
    </PageFrame>
  );
}

/**
 * Edits one harness. `HarnessService` has no get, so the harness is found in its
 * namespace's list, which is the same read the harnesses tab makes.
 */
export function HarnessEditPage() {
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const harnesses = useHarnesses(namespace);
  const harness = harnesses.data?.find((row) => row.namespace === namespace && row.name === name);

  return (
    <PageFrame
      title={`Edit harness ${name ?? ""}`}
      description="Changes apply to every agent that references this harness."
    >
      {harnesses.error ? (
        <Alert
          type="error"
          showIcon
          title="Could not load this harness"
          description={harnesses.error.message}
          data-testid="harness-edit-load-error"
        />
      ) : harnesses.data && !harness ? (
        <Alert
          type="warning"
          showIcon
          title="Harness not found"
          description={`No harness ${namespace}/${name}.`}
          data-testid="harness-edit-not-found"
        />
      ) : harness ? (
        <Card size="small" css={{ maxWidth: 720 }}>
          <HarnessForm
            // Remounts on a reloaded resource, so the draft never edits a stale copy.
            key={harness.resource.metadata.resourceVersion ?? ""}
            harness={harness}
            onSaved={(saved) => {
              toast.success(`Harness ${saved.name} saved`);
              void navigate(harnessesTab);
            }}
            onCancel={() => void navigate(harnessesTab)}
          />
        </Card>
      ) : (
        <Card css={{ maxWidth: 720 }}>
          <Skeleton active paragraph={{ rows: 8 }} />
        </Card>
      )}
    </PageFrame>
  );
}
