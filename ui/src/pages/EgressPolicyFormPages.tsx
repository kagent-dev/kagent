import { useState } from "react";
import { Alert, Button, Card, Skeleton } from "antd";
import toast from "react-hot-toast";
import { Link, useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { EgressPolicyForm } from "@/components/egress-policy-form/EgressPolicyForm";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { draftFromPolicy, emptyPolicyDraft } from "@/components/egress-policy-form/egressPolicyDraft";
import { DEFAULT_NAMESPACE } from "@/components/common/resourceName";
import { apiClient, isNotFound, useEgressPolicy, useInvalidateEgressPolicies, type EgressPolicyResource } from "@/api";
import { buildPath, paths } from "@/router/routes";

// region Component

export function EgressPolicyNewPage() {
  const navigate = useNavigate();
  const invalidate = useInvalidateEgressPolicies();

  async function create(resource: EgressPolicyResource) {
    const { name, namespace = "" } = resource.metadata;
    const created = await apiClient.egressPolicies.create({ namespace, name, resource });
    await invalidate().catch(() => {});
    toast.success(egressCopy.form.created(created.name));
    await navigate(buildPath(paths.egressPolicyDetail, { namespace: created.namespace, name: created.name }));
  }

  return (
    <PageFrame title={egressCopy.form.newTitle} description={egressCopy.form.newDescription}>
      <EgressPolicyForm
        initial={emptyPolicyDraft(DEFAULT_NAMESPACE)}
        onSubmit={create}
        onCancel={() => void navigate(paths.egressPolicies)}
      />
    </PageFrame>
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
  const detail = namespace && name ? buildPath(paths.egressPolicyDetail, { namespace, name }) : paths.egressPolicies;

  async function save(resource: EgressPolicyResource) {
    if (!namespace || !name) throw new Error("An egress policy needs a namespace and a name.");
    await apiClient.egressPolicies.update({ namespace, name, resource });
    await invalidate().catch(() => {});
    toast.success(egressCopy.form.saved(name));
    await navigate(detail);
  }

  async function reload() {
    await policy.refresh();
    setGeneration((value) => value + 1);
  }

  return (
    <PageFrame
      title={egressCopy.form.editTitle(name ?? "")}
      description={missing ? undefined : egressCopy.form.editDescription}
      actions={
        <Link to={detail}>
          <Button>{egressCopy.details.back}</Button>
        </Link>
      }
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
        <Alert
          type="error"
          showIcon
          title={egressCopy.details.loadError}
          description={policy.error.message}
          data-testid="egress-edit-load-error"
          action={<Button size="small" onClick={() => void policy.refresh()}>Try again</Button>}
        />
      ) : policy.data ? (
        <EgressPolicyForm
          key={`${generation}-${policy.data.resource.metadata.resourceVersion ?? ""}`}
          initial={draftFromPolicy(policy.data)}
          existing={policy.data.resource}
          onSubmit={save}
          onCancel={() => void navigate(detail)}
          onReload={() => void reload()}
        />
      ) : (
        <Card css={{ maxWidth: 860 }}>
          <Skeleton active paragraph={{ rows: 8 }} data-testid="egress-edit-loading" />
        </Card>
      )}
    </PageFrame>
  );
}
