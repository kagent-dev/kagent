import { Button } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ModelForm } from "@/components/model-form/ModelForm";
import { paths } from "@/router/routes";
import { apiClient, useModels, type CreateModelConfigRequest } from "@/api";

/**
 * Create a model configuration.
 *
 * The fields live in `ModelForm`, shared with the edit page. This page owns only the
 * request and where the user goes afterwards.
 */
export function ModelNewPage() {
  const navigate = useNavigate();
  // The list this page is about to navigate to.
  const models = useModels();

  async function createModel(payload: CreateModelConfigRequest): Promise<void> {
    await apiClient.models.create(payload);
    // Swallowed: this is the list re-read, not the write. It has already succeeded by
    // here, and `refresh` rethrows deliberately (see `useApiResource`) — so letting it
    // through reports a created resource as "not created", beside a Try again that
    // re-posts and comes back 409. The list shows its own read error on arrival.
    await models.refresh().catch(() => {});
    // Straight to the list, where the new configuration can be seen — the row is
    // better evidence than a message on the form the user is still looking at.
    await navigate(paths.models);
  }

  return (
    <PageFrame
      title="New model"
      description="A model configuration names a provider, a model, and where its credential lives."
      actions={
        <Link to={paths.models}>
          <Button>Back to models</Button>
        </Link>
      }
    >
      <ModelForm onSubmit={createModel} />
    </PageFrame>
  );
}
