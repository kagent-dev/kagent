import { test, expect } from "../fixtures/test";
import { agentChat, instances, loadPage, routes } from "../helpers/app";

/**
 * The round trip a snapshot makes: taken in a conversation, released from the page
 * that lists them.
 *
 * Both halves in one spec because neither is worth much alone. A create with no delete
 * leaves the thing this page exists for untested — a checkpoint pins a copy of the
 * conversation's runtime, and until this page there was no way to give it back.
 */
test("snapshots: taken in a chat, and deleted in bulk from the list", async ({ page }) => {
  await page.goto(agentChat(instances.ready));
  const mine = page.locator('[data-testid="chat-message"][data-role="user"]');
  await expect(mine.first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. two more boundaries, saved from the composer", async () => {
    for (const said of ["A turn to save.", "And another one."]) {
      const before = await mine.count();
      /*
       * Driven from the keyboard, both of them.
       *
       * The toaster sits bottom-right, which is where the composer keeps Send and
       * Checkpoint — so the toast confirming one checkpoint lands on the buttons and a
       * click is intercepted until it fades. Waiting that out would be a test timed off
       * an animation; pressing the control that already has focus is not, and it is a
       * path a reader takes anyway.
       */
      await page.getByTestId("chat-input").fill(said);
      await page.getByTestId("chat-input").press("Enter");
      await expect(mine).toHaveCount(before + 1, { timeout: 30_000 });
      await expect(page.getByTestId("chat-checkpoint")).toBeEnabled({ timeout: 30_000 });
      await page.getByTestId("chat-checkpoint").focus();
      await page.getByTestId("chat-checkpoint").press("Enter");
      await expect(page.getByTestId("chat-checkpoint")).toBeDisabled();
    }
  });

  const rows = page.getByTestId("snapshots-table").locator("tbody tr[data-row-key]");

  await test.step("2. the list has them, this conversation's and the seeded ones", async () => {
    await loadPage(page, routes.snapshots, { title: "Snapshots" });
    // Two saved above, plus the two the fixture seeds against other conversations.
    await expect(rows).toHaveCount(4, { timeout: 30_000 });
    await expect(page.getByTestId("snapshots-summary")).toContainText("4 snapshots");
  });

  await test.step("3. the search is the controller's, and the count is of what matched", async () => {
    // The count moving is the point: it is the controller's total for the filter, so a
    // page narrowing rows it had already been handed would leave it at four.
    await page.getByTestId("snapshots-filters-search").fill("nothing matches this");
    await expect(rows).toHaveCount(0);
    await expect(page.getByTestId("snapshots-summary")).toContainText("0 snapshots");

    // A name that belongs to one boundary only, so the count is unambiguous.
    await page.getByTestId("snapshots-filters-search").fill("Deploy summary");
    await expect(rows).toHaveCount(1);
    await expect(page.getByTestId("snapshots-summary")).toContainText("1 snapshot");

    await page.getByTestId("snapshots-filters-search").fill("");
    await expect(rows).toHaveCount(4);
  });

  await test.step("4. nothing picked, nothing to delete", async () => {
    await expect(page.getByTestId("snapshots-delete-selected")).toBeDisabled();
  });

  await test.step("5. the header box picks every row, and unpicks them", async () => {
    const selectAll = page.getByTestId("snapshots-table").locator("thead input[type=checkbox]");
    await selectAll.check();
    await expect(page.getByTestId("snapshots-delete-selected")).toBeEnabled();
    await selectAll.uncheck();
    await expect(page.getByTestId("snapshots-delete-selected")).toBeDisabled();
  });

  await test.step("6. two of them, deleted together and gone from the list", async () => {
    const boxes = page.getByTestId("snapshots-table").locator("tbody input[type=checkbox]");
    await boxes.nth(0).check();
    await boxes.nth(1).check();
    await page.getByTestId("snapshots-delete-selected").click();

    await expect(rows).toHaveCount(2, { timeout: 30_000 });
    await expect(page.getByTestId("snapshots-summary")).toContainText("2 snapshots");
  });

  await test.step("7. and one at a time, from the row itself", async () => {
    await rows.first().locator('[data-testid^="delete-"]').click();
    // The popover the shared delete control asks through.
    await page.locator(".ant-popconfirm").getByRole("button", { name: "Delete" }).click();

    await expect(rows).toHaveCount(1, { timeout: 30_000 });
  });
});
