import { Button } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ModelForm } from "@/components/model-form/ModelForm";
import { paths } from "@/router/routes";
import { apiClient, useInvalidateModels, type CreateModelConfigRequest } from "@/api";

/**
 * Create a model configuration.
 *
 * The fields live in `ModelForm`, shared with the edit page. This page owns only the
 * request and where the user goes afterwards.
 */
export function ModelNewPage() {
  const navigate = useNavigate();
  const invalidateModels = useInvalidateModels();

  async function createModel(payload: CreateModelConfigRequest): Promise<void> {
    await apiClient.models.create(payload);
    /*
     * A key sweep rather than a subscription. `useModels()` here meant the form read
     * every configuration on mount purely to have something to call `refresh` on, and
     * rendered none of it.
     *
     * Swallowed, because this is the list re-read rather than the write: the create has
     * already succeeded, and letting a failed re-read through reports a resource that
     * exists as "not created", beside a Try again that re-posts and comes back 409. The
     * list reports its own read failure on arrival.
     */
    await invalidateModels().catch(() => {});
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
