import { Alert, Skeleton } from "antd";
import { ReadError } from "@/components/common/ReadError";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { useHarnesses } from "@/api";
import { BackLink } from "@/components/form/BackLink";
import { HarnessForm } from "@/components/harness-form/HarnessForm";
import { HarnessIntro } from "@/components/harness-form/HarnessFields";
import { copySource, useMountedBlank } from "@/components/table/copyFrom";
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
    <>
      <BackLink to={harnessesTab} resource="Harnesses" />
      <PageFrame
        title="New harness"
        description="A harness is the runtime an agent runs on. Agents reference it by name."
      >
        <div css={{ maxWidth: 720 }}>
          <HarnessIntro />
          {/* A source that cannot be had still leaves a form to fill in: the alert says why it is blank. */}
          {from && <CopyNotice kind="harness" from={from} error={source.error?.message} notFound={Boolean(source.data) && !copyOf} loaded={Boolean(copyOf)} mountedBlank={mountedBlank} testId="harness-copy" />}
          {waiting ? (
            <Skeleton active paragraph={{ rows: 8 }} />
          ) : (
            <HarnessForm
              // Keyed on the requested source, so a late-arriving one never remounts an edited form.
              key={from ? `${from.namespace}/${from.name}` : "new"}
              copyOf={copyOf}
              onSaved={() => void navigate(harnessesTab)}
              cancel={{ to: harnessesTab }}
            />
          )}
        </div>
      </PageFrame>
    </>
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
    <>
      <BackLink to={harnessesTab} resource="Harnesses" />
      <PageFrame
        title={`Edit harness ${name ?? ""}`}
        description="Changes apply to every agent that references this harness."
      >
        {harnesses.error ? (
          <ReadError
            resource="this harness"
            error={harnesses.error}
            testId="harness-edit-load-error"
            onRetry={harnesses.refresh}
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
          <div css={{ maxWidth: 720 }}>
            <HarnessForm
              // Remounts on a reloaded resource, so the draft never edits a stale copy.
              key={harness.resource.metadata.resourceVersion ?? ""}
              harness={harness}
              onSaved={() => void navigate(harnessesTab)}
              cancel={{ to: harnessesTab }}
            />
          </div>
        ) : (
          <div css={{ maxWidth: 720 }}>
            <Skeleton active paragraph={{ rows: 8 }} />
          </div>
        )}
      </PageFrame>
    </>
  );
}
