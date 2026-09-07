import { Button, Space } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { PromptForm } from "@/components/prompts/PromptForm";
import { paths } from "@/router/routes";
import { apiClient, useInvalidatePrompts, type CreatePromptTemplateRequest } from "@/api";

/**
 * Create a prompt library.
 *
 * The fields are `PromptForm`, which the detail page's edit mode renders too; this
 * page owns only the request and where the reader lands afterwards.
 */
export function PromptNewPage() {
  const navigate = useNavigate();
  const invalidatePrompts = useInvalidatePrompts();

  async function createLibrary(payload: CreatePromptTemplateRequest): Promise<void> {
    await apiClient.prompts.create(payload);
    // Re-read before navigating, so the list lands showing the new library rather
    // than the set that was cached without it.
    await invalidatePrompts();
    // Straight to the list, where the new library can actually be seen — a success
    // message on a form the user is still looking at proves less than the row itself.
    await navigate(paths.prompts);
  }

  return (
    <PageFrame
      title="New prompt library"
      description="A library is a named bag of prompt fragments agents can include by key."
      actions={
        <Link to={paths.prompts}>
          <Button>Back to libraries</Button>
        </Link>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        <PromptForm onSubmit={createLibrary} />
      </Space>
    </PageFrame>
  );
}
