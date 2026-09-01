/**
 * How several installed extensions combine into one answer.
 *
 * Two rules cover everything the framework composes, and which one applies is a
 * property of the capability rather than of the extensions:
 *
 * - **Additive** capabilities concatenate in array order. A nav item, a route, a
 *   form field, a table column, a context provider and a slot component are all
 *   things a page can have several of, so several extensions each get theirs.
 *   Those folds live in `hooks.ts`, next to the readers that need them.
 * - **Singular** capabilities — the ones where the application can only act on one
 *   answer, such as which component replaces the sidebar or what the document
 *   title is — are merged field by field with *later entries winning*. That is the
 *   rule an ordered array already implies: a distribution lists the extension whose
 *   opinion should prevail last, the same way the later of two stylesheets does.
 *
 * Both helpers here serve the second rule.
 */

/** The values that are actually present, in order. */
export function defined<T>(values: readonly (T | undefined)[]): T[] {
  return values.filter((value): value is T => value !== undefined);
}

/**
 * Several partial objects flattened into one, later entries winning field by field.
 *
 * Only one level deep, and deliberately: every object this is used on — a shell's
 * regions, a branding block, the agent links, the provider icon map — is a flat
 * table of independent choices, so an extension naming one of them should not have
 * to restate the others. Anything needing a second level (`navOverrides`, whose
 * values are themselves override objects) says so where it is merged.
 *
 * A field left `undefined` never overwrites: absent means "no opinion", which is
 * what lets a second extension change the header without also blanking the sidebar
 * the first one replaced.
 */
export function mergeDefined<T extends object>(
  parts: readonly (T | undefined)[],
): T {
  const merged: Record<string, unknown> = {};

  for (const part of parts) {
    if (!part) continue;
    for (const [key, value] of Object.entries(part)) {
      if (value !== undefined) merged[key] = value;
    }
  }

  return merged as T;
}
