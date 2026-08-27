import { test, expect } from "../fixtures/test";
import { expectSettled, loadPage, routes } from "../helpers/app";

/**
 * The dashboard's recent list, which is conversations and now reads like it.
 *
 * The card was headed "Recently created agents" and listed `AgentInstance` rows, which
 * are conversations rather than agents — so the heading was wrong about what it held.
 * The rows were worse: each linked to a conversation under a bare eight-character id,
 * on the reasoning that "an agent has no name". A conversation does have one, and this
 * is the third surface to show it after the rail and the agent's own table.
 */
test("dashboard: recent conversations read as names, not as ids", async ({ page }) => {
  await loadPage(page, routes.dashboard);
  await expectSettled(page);

  const card = page.getByTestId("dashboard-recent-card");
  await expect(card).toBeVisible({ timeout: 30_000 });

  await test.step("1. the card says what it lists", async () => {
    await expect(card).toContainText("Recent agent conversations");
  });

  await test.step("2. and no row is a bare id", async () => {
    /*
     * The property, rather than a fixture's particular name.
     *
     * `conversationTitle` answers with the name somebody gave it, the title derived
     * from its first message, or "Untitled" beside the short id — never the short id
     * alone. So a link whose whole text is eight hex characters is the old behaviour,
     * whichever conversation happens to be recent enough to appear here.
     */
    const rows = page.getByTestId("recent-agent");
    await expect(rows.first()).toBeVisible();
    const labels = await rows.locator("a").evaluateAll((links) =>
      links.map((link) => link.textContent?.trim() ?? ""),
    );

    expect(labels.length).toBeGreaterThan(0);
    for (const label of labels) {
      expect(label, "a conversation should be listed by name, not by its id").not.toMatch(
        /^[0-9a-f]{8}$/,
      );
    }
  });

  await test.step("3. and a conversation somebody named shows that name", async () => {
    // The other half: "not an id" would also be satisfied by every row reading
    // "Untitled", which is true and useless. At least one of the fixtures' recent
    // conversations carries a name its reader chose.
    const labels = await page
      .getByTestId("recent-agent")
      .locator("a")
      .evaluateAll((links) => links.map((link) => link.textContent?.trim() ?? ""));

    expect(
      labels.some((label) => label !== "" && !label.startsWith("Untitled")),
      `at least one recent conversation should read as a chosen name; got ${JSON.stringify(labels)}`,
    ).toBe(true);
  });
});
