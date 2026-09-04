import { Alert, Button, Skeleton, Space } from "antd";
import toast from "react-hot-toast";
import { Link, useNavigate, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { PromptForm } from "@/components/prompts/PromptForm";
import { promptDraftFrom } from "@/components/prompts/promptDraft";
import { buildPath, paths } from "@/router/routes";
import {
  apiClient,
  isNotFound,
  useInvalidatePrompts,
  usePrompt,
  type CreatePromptTemplateRequest,
} from "@/api";

/**
 * Change a prompt library's fragments.
 *
 * The edit half of the pair `PromptForm` serves: the form owns the fields and the
 * rules, this page owns the request and what happens afterwards.
 *
 * The library is read before the form is shown rather than the form appearing empty
 * and filling in: a form that renders blank invites somebody to type into a field
 * about to be overwritten, and on a slow read an empty fragment is indistinguishable
 * from one nobody has written yet.
 *
 * `UpdatePromptTemplate` carries only the fragment map — the ref addresses the
 * ConfigMap, which is why the form's identity fields are locked — so the identity in
 * the payload the form hands over is dropped here rather than sent.
 */
export function PromptEditPage() {
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const library = usePrompt(namespace, name);
  const invalidatePrompts = useInvalidatePrompts();

  // A 404 is a different answer from a failed request: the cluster says there is
  // nothing here to edit, so that branch offers a way back rather than a retry.
  const missing = library.error !== undefined && isNotFound(library.error);

  /** Where a save or a cancel lands: the library itself, which is what changed. */
  const detail =
    namespace && name
      ? buildPath(paths.promptDetail, { namespace, name })
      : paths.prompts;

  async function saveLibrary(payload: CreatePromptTemplateRequest): Promise<void> {
    /*
     * Thrown rather than returned. The route cannot match without both, so this is
     * unreachable — but returning would hand the form a save that wrote nothing and
     * said nothing: no toast, no navigation, no error, and a draft it has already
     * stopped guarding. A throw reaches the form's own failure path.
     */
    if (!namespace || !name) {
      throw new Error("A prompt library is addressed by both a namespace and a name.");
    }

    await apiClient.prompts.update(namespace, name, { data: payload.data });
    /*
     * Re-read before navigating, so the page that opens shows the saved fragments
     * rather than the ones just replaced — which would read as a save that did not
     * take.
     *
     * A key sweep rather than this library's own read: the list's key count and
     * fragment tags change with an edit too, and its key carries the namespace
     * filter, so refreshing one read would leave whichever list is behind this
     * stale.
     */
    await invalidatePrompts();
    toast.success(`Prompt library ${name} saved`);
    await navigate(detail);
  }

  return (
    <PageFrame
      title={name ? `Edit ${name}` : "Edit prompt library"}
      description={
        missing ? undefined : "Replaces this library's fragments with what you save."
      }
      actions={
        <Link to={detail}>
          <Button>Back to library</Button>
        </Link>
      }
    >
      <Space
        orientation="vertical"
        size="middle"
        css={{ display: "flex", maxWidth: 720 }}
      >
        {missing ? (
          <Alert
            type="warning"
            showIcon
            title="This prompt library does not exist"
            description={`No library named ${namespace}/${name} was found in the cluster. It may have been deleted.`}
            data-testid="prompt-edit-not-found"
            action={
              <Link to={paths.prompts}>
                <Button size="small">Back to libraries</Button>
              </Link>
            }
          />
        ) : library.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this prompt library"
            description={library.error.message}
            data-testid="prompt-edit-load-error"
            action={
              <Button size="small" onClick={() => void library.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {library.isLoading ? (
          <Skeleton active paragraph={{ rows: 6 }} data-testid="prompt-edit-loading" />
        ) : null}

        {library.data ? (
          <PromptForm
            initial={promptDraftFrom(library.data)}
            outcome="saved"
            submitLabel="Save changes"
            // The ref is how the controller addresses the library, so an edit cannot
            // rename one or move it to another namespace.
            identityLocked
            onSubmit={saveLibrary}
            // Handed a callback rather than left to the form's own link back to the
            // list, so Cancel returns to the library being edited — and so the form
            // can ask first when there is a draft to lose.
            onCancel={() => void navigate(detail)}
          />
        ) : null}
      </Space>
    </PageFrame>
  );
}
