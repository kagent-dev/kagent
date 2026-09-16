import { test, expect } from "../fixtures/test";
import { expectNoLoadFailure, loadApp } from "../helpers/app";

/**
 * The dashboard's recent list, asserted the same way on either backend.
 *
 * The claim is a property rather than a value: `conversationTitle` answers with the
 * name somebody gave a conversation, the title derived from its first message, or
 * "Untitled" beside the short id — never the bare id alone. That holds whatever is in
 * the list, so it is one of the few things worth asserting twice.
 *
 * What stays in `tests/dashboard.spec.ts` is what only fixtures can settle: that
 * Refresh re-reads, and that a conversation somebody named shows that name.
 */
test("dashboard: the recent list names conversations rather than showing ids", async ({
  page,
}) => {
  await loadApp(page, "/");
  await expectNoLoadFailure(page);

  const card = page.getByTestId("dashboard-recent-card");
  await expect(card).toBeVisible({ timeout: 30_000 });
  await expect(card).toContainText("Recent agent conversations");

  /*
   * A cluster may genuinely have no conversations yet where the fixtures always do, so
   * the count cannot be the claim. That one of the two good states is drawn can be: the
   * card renders a list, an empty state, or the unavailable notice, and exactly one of
   * the first two is what "the backend answered and was understood" looks like.
   *
   * Said this way rather than looping over whatever rows exist, because a shared spec
   * that asserts nothing when the list is empty is the way this folder rots.
   */
  await expect
    .poll(
      async () => {
        if ((await page.getByTestId("recent-agents-unavailable").count()) > 0)
          return "unavailable";
        if ((await page.getByTestId("recent-agents").count()) > 0) return "listed";
        if ((await page.getByTestId("recent-agents-empty").count()) > 0) return "empty";
        // The fourth state, and the reason this polls: while the read is in flight the
        // card draws none of the three. Counted once instead, this failed on the mock
        // backend and passed live, which is the wrong way round for a real defect.
        return "loading";
      },
      {
        message: "the recent card should settle on a list or an empty state",
        timeout: 30_000,
      },
    )
    .toMatch(/^(listed|empty)$/);

  const labels = await page
    .getByTestId("recent-agent")
    .locator("a")
    .evaluateAll((links) => links.map((link) => link.textContent?.trim() ?? ""));

  for (const label of labels) {
    expect(label, "a conversation should be listed by name, not by its id").not.toMatch(
      /^[0-9a-f]{8}$/,
    );
    expect(label, "a conversation should carry some label").not.toBe("");
  }
});
