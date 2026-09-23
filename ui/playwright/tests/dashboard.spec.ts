import { test, expect } from "../fixtures/test";
import { expectSettled, loadPage, routes } from "../helpers/app";
import { operationCalls, rpc } from "../helpers/mockCalls";
import { clickRefresh } from "../helpers/resource";

/**
 * The dashboard's recent list, on the fixtures.
 *
 * The card was headed "Recently created agents" and listed `AgentInstance` rows, which
 * are conversations rather than agents — so the heading was wrong about what it held.
 * The rows were worse: each linked to a conversation under a bare eight-character id,
 * on the reasoning that "an agent has no name".
 *
 * What is left here is what only a fixed backend can settle. That no row is a bare id
 * is a property of any backend, so it runs against both from `shared/dashboard.spec.ts`.
 */
test("dashboard: Refresh re-reads, and a named conversation shows its name", async ({
  page,
}) => {
  await loadPage(page, routes.dashboard);
  await expectSettled(page);

  const card = page.getByTestId("dashboard-recent-card");
  await expect(card).toBeVisible({ timeout: 30_000 });

  await test.step("1. the card says what it lists", async () => {
    await expect(card).toContainText("Recent agent conversations");

    // And Refresh confirms here too — wired up on the page being worked on and
    // forgotten on the four beside it is exactly how this goes wrong. Counted rather
    // than read off the toast, which lives two seconds.
    const before = await operationCalls(page, rpc.listAgentInstances);
    await clickRefresh(page);
    await expect
      .poll(() => operationCalls(page, rpc.listAgentInstances), { timeout: 10_000 })
      .toBeGreaterThan(before);
  });

  await test.step("2. and a conversation somebody named shows that name", async () => {
    // The other half of the shared spec's claim: "not an id" would also be satisfied
    // by every row reading "Untitled", which is true and useless. Only fixtures can
    // guarantee a conversation whose name its reader chose.
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
