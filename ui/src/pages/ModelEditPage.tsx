import { Alert, Button, Skeleton, Space } from "antd";
import toast from "react-hot-toast";
import { Link, useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ModelForm } from "@/components/model-form/ModelForm";
import { modelDraftFrom } from "@/components/model-form/modelDraft";
import { paths } from "@/router/routes";
import {
  apiClient,
  useModel,
  useModels,
  type CreateModelConfigRequest,
} from "@/api";

/**
 * Change a model configuration.
 *
 * Addressed per resource — `PUT /modelconfigs/{namespace}/{name}` — unlike agents,
 * which PUT to the collection. The two endpoints genuinely differ, so the identity
 * comes from the route here rather than from the payload.
 *
 * The configuration is read before the form is shown rather than the form appearing
 * empty and filling in: a form that renders blank invites someone to type into a
 * field about to be overwritten, and on a slow read an empty field is
 * indistinguishable from an unset one.
 */
export function ModelEditPage() {
  const navigate = useNavigate();
  const { namespace, name } = useParams();

  const model = useModel(namespace, name);
  const models = useModels();

  async function saveModel(payload: CreateModelConfigRequest): Promise<void> {
    if (!namespace || !name) return;

    await apiClient.models.update(namespace, name, payload);
    // Refreshed before navigating, so the list lands showing the new values rather
    // than the previous ones for a moment.
    await models.refresh();
    await model.refresh();
    toast.success(`Model configuration ${name} updated`);
    await navigate(paths.models);
  }

  return (
    <PageFrame
      title={name ? `Edit ${name}` : "Edit model"}
      description="Replaces the configuration's provider, model and credential reference."
      actions={
        <Link to={paths.models}>
          <Button>Back to models</Button>
        </Link>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {model.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this model configuration"
            description={model.error.message}
            data-testid="model-edit-load-error"
          />
        ) : null}

        {model.isLoading ? <Skeleton active paragraph={{ rows: 6 }} /> : null}

        {model.data && !model.data.canUpdate ? (
          <Alert
            type="info"
            showIcon
            title="You cannot edit this model configuration"
          />
        ) : null}

        {model.data?.canUpdate ? (
          <ModelForm
            initial={modelDraftFrom(model.data)}
            outcome="saved"
            submitLabel="Save changes"
            // The ref is how the controller addresses the resource, so an edit
            // cannot move a configuration.
            identityLocked
            onSubmit={saveModel}
          />
        ) : null}
      </Space>
    </PageFrame>
  );
}
