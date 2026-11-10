import { Alert, Card, Skeleton } from "antd";
import toast from "react-hot-toast";
import { useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { useHarnesses } from "@/api";
import { HarnessForm } from "@/components/harness-form/HarnessForm";
import { HarnessIntro } from "@/components/harness-form/HarnessFields";
import { paths } from "@/router/routes";

const harnessesTab = `${paths.agents}?tab=harnesses`;

export function HarnessNewPage() {
  const navigate = useNavigate();
  return (
    <PageFrame
      title="New harness"
      description="A harness is the runtime an agent runs on. Agents reference it by name."
    >
      <Card size="small" css={{ maxWidth: 720 }}>
        <HarnessIntro />
        <HarnessForm
          onSaved={(saved) => {
            toast.success(`Harness ${saved.name} created`);
            void navigate(harnessesTab);
          }}
          onCancel={() => void navigate(harnessesTab)}
        />
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
