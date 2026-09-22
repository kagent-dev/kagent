import { expect, type Page } from "@playwright/test";

import { expectListLoaded, loadApp, rowNamed, searchList } from "./app";
import { confirmDelete } from "./resource";

/**
 * Removes a row this run made, from a `test.afterEach`, without ever throwing.
 *
 * Every move a cleanup makes can fail — the navigation, the list read, the delete — and
 * each of them throws. Thrown from a cleanup, that replaces the failure the test was
 * actually reporting, *and* skips the delete underneath it: the run reports a timeout in
 * the cleanup while the resource stays on the cluster.
 *
 * A hook rather than a `finally`, which is the other half: a timed-out test has a closed
 * page, so everything in its `finally` throws before it can delete anything.
 *
 * So it warns and returns. A cleanup that could not run says so in the output, and the
 * test still reports what it found.
 */
export async function sweepUp(
  page: Page,
  list: string,
  name: string,
  /** Where that list lives, for the two that do not share their test-id prefix. */
  path = `/${list}`,
): Promise<void> {
  try {
    await loadApp(page, path);
    await searchList(page, list, name);
    // Asked only once the list has answered: a read still in flight has no rows either,
    // and taking that for "already gone" would leave it on the cluster.
    await expectListLoaded(page, list);
    if ((await rowNamed(page, name).count()) === 0) return;

    await confirmDelete(page, name);
    await expect(rowNamed(page, name)).toHaveCount(0, { timeout: 60_000 });
  } catch (error) {
    console.warn(`cleanup: ${name} may still be on the cluster — ${String(error)}`);
  }
}

/**
 * The same promise for a cleanup that is not a row on a list: run it, never throw.
 *
 * For the journeys whose resource is reached by its own address — a schedule's detail
 * page, a template's — where what has to happen is a navigation and a confirm rather
 * than a search.
 */
export async function sweepQuietly(
  what: string,
  remove: () => Promise<void>,
): Promise<void> {
  try {
    await remove();
  } catch (error) {
    console.warn(`cleanup: ${what} may still be on the cluster — ${String(error)}`);
  }
}
