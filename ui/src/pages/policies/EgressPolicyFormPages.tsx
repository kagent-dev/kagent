import { useState } from "react";
import { ReadError } from "@/components/common/ReadError";
import { Alert, Card, Skeleton } from "antd";
import { useNavigate, useParams } from "react-router-dom";
import { BackLink } from "@/components/form/BackLink";
import { toastSaved } from "@/components/form/saveToast";
import { PageFrame } from "@/components/Structure/PageFrame";
import { EgressPolicyForm } from "@/components/egress-policy-form/EgressPolicyForm";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { draftFromPolicy, emptyPolicyDraft } from "@/components/egress-policy-form/egressPolicyDraft";
import { DEFAULT_NAMESPACE } from "@/components/common/resourceName";
import { apiClient, isNotFound, useEgressPolicy, useInvalidateEgressPolicies, type EgressPolicyResource } from "@/api";
import { buildPath, egressPoliciesTab, paths } from "@/router/routes";

// region Component

export function EgressPolicyNewPage() {
  const navigate = useNavigate();
  const invalidate = useInvalidateEgressPolicies();

  async function create(resource: EgressPolicyResource) {
    const { name, namespace = "" } = resource.metadata;
    const created = await apiClient.egressPolicies.create({ namespace, name, resource });
    await invalidate().catch(() => {});
    toastSaved("created", egressCopy.kind, created.name);
    return created;
  }

  return (
    <>
      <BackLink to={egressPoliciesTab} resource={egressCopy.page.title} />
      <PageFrame title={egressCopy.form.newTitle} description={egressCopy.form.newDescription}>
        <EgressPolicyForm
          initial={emptyPolicyDraft(DEFAULT_NAMESPACE)}
          onSubmit={create}
          onSaved={(created) => void navigate(buildPath(paths.egressPolicyDetail, { namespace: created.namespace, name: created.name }))}
          cancelTo={egressPoliciesTab}
        />
      </PageFrame>
    </>
  );
}

export function EgressPolicyEditPage() {
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const policy = useEgressPolicy(namespace, name);
  const invalidate = useInvalidateEgressPolicies();
  // Bumped after a stale save, so the form remounts on the reloaded policy.
  const [generation, setGeneration] = useState(0);
  const missing = policy.error !== undefined && isNotFound(policy.error);
  const detail = namespace && name ? buildPath(paths.egressPolicyDetail, { namespace, name }) : egressPoliciesTab;

  async function save(resource: EgressPolicyResource) {
    if (!namespace || !name) throw new Error("An egress policy needs a namespace and a name.");
    const updated = await apiClient.egressPolicies.update({ namespace, name, resource });
    await invalidate().catch(() => {});
    toastSaved("saved", egressCopy.kind, name);
    return updated;
  }

  async function reload() {
    await policy.refresh();
    setGeneration((value) => value + 1);
  }

  return (
    <>
    <BackLink to={egressPoliciesTab} resource={egressCopy.page.title} />
    <PageFrame
      title={egressCopy.form.editTitle(name ?? "")}
      description={missing ? undefined : egressCopy.form.editDescription}
    >
      {missing ? (
        <Alert
          type="warning"
          showIcon
          title={egressCopy.details.notFoundTitle}
          description={egressCopy.details.notFound(`${namespace}/${name}`)}
          data-testid="egress-edit-not-found"
        />
      ) : policy.error ? (
        <ReadError
          resource="this egress policy"
          error={policy.error}
          testId="egress-edit-load-error"
          title={egressCopy.details.loadError}
          onRetry={policy.refresh}
        />
      ) : policy.data ? (
        <EgressPolicyForm
          key={`${generation}-${policy.data.resource.metadata.resourceVersion ?? ""}`}
          initial={draftFromPolicy(policy.data)}
          existing={policy.data.resource}
          onSubmit={save}
          onSaved={() => void navigate(detail)}
          cancelTo={detail}
          onReload={() => void reload()}
        />
      ) : (
        <Card css={{ maxWidth: 860 }}>
          <Skeleton active paragraph={{ rows: 8 }} data-testid="egress-edit-loading" />
        </Card>
      )}
    </PageFrame>
    </>
  );
}
