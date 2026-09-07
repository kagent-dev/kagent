/**
 * What a prompt library looks like while somebody is writing it.
 *
 * Split from the form for the reason `modelDraft` is: the shape and the rules that
 * decide whether it can be sent are worth reading — and unit-testing — without a
 * component around them, and a component file that exports functions cannot
 * hot-reload.
 */

import type { CreatePromptTemplateRequest, PromptTemplateDetail } from "@/api";
import { DEFAULT_NAMESPACE, RFC1123_SUBDOMAIN } from "@/components/common/resourceName";
import {
  type FragmentRow,
  fragmentIssues,
  fragmentsToData,
  newFragmentRow,
  rowsFromData,
} from "./fragmentRows";

/**
 * A library being authored: where it lives, what it is called, what is in it.
 *
 * The rows rather than the `data` map the API takes, because a map cannot hold what
 * the editor has to — a half-typed key, two rows briefly sharing one, a blank row
 * waiting to be filled in.
 */
export interface PromptDraft {
  namespace: string;
  name: string;
  rows: FragmentRow[];
}

/** A new library: the default namespace, no name, and one row to type into. */
export function emptyPromptDraft(): PromptDraft {
  return { namespace: DEFAULT_NAMESPACE, name: "", rows: [newFragmentRow()] };
}

/** An existing library, as the form edits it. */
export function promptDraftFrom(detail: PromptTemplateDetail): PromptDraft {
  return {
    namespace: detail.namespace,
    name: detail.name,
    rows: rowsFromData(detail.data),
  };
}

/**
 * What stops this draft being sent, in the words the form shows.
 *
 * `identityLocked` drops the name and namespace rules rather than the fields: an edit
 * addresses a library that already exists, so those two are the resource's own and
 * cannot be wrong. Everything else applies to both, because the controller applies it
 * to both — `UpdatePromptTemplate` refuses an empty map exactly as `Create` does.
 */
export function promptDraftIssues(
  draft: PromptDraft,
  { identityLocked = false }: { identityLocked?: boolean } = {},
): string[] {
  const issues: string[] = [];

  if (!identityLocked) {
    if (!draft.namespace.trim()) issues.push("A namespace is required.");
    else if (!RFC1123_SUBDOMAIN.test(draft.namespace.trim())) {
      issues.push("The namespace must be a valid Kubernetes name.");
    }
    if (!draft.name.trim()) issues.push("A library name is required.");
    else if (!RFC1123_SUBDOMAIN.test(draft.name.trim())) {
      issues.push(
        "The name must use lowercase letters, numbers and hyphens, starting and ending with a letter or number.",
      );
    }
  }

  issues.push(...fragmentIssues(draft.rows));
  return issues;
}

/**
 * The request body, in the create shape.
 *
 * One shape for both callers, as the model form does: an update carries only `data` —
 * the ref addresses the library — so the edit caller takes the map out of this and
 * ignores the identity it already knows.
 */
export function promptPayloadFrom(draft: PromptDraft): CreatePromptTemplateRequest {
  return {
    namespace: draft.namespace.trim(),
    name: draft.name.trim(),
    data: fragmentsToData(draft.rows),
  };
}
