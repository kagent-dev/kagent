import { test, expect } from "../../fixtures/test";
import { agents, loadPage, routes } from "../../helpers/app";
import { expectShell } from "../../helpers/nav";
import { CORE_NAV_ORDER, allSlots, navOrder } from "../../helpers/extensions";

/**
 * Extension points — with nothing installed.
 *
 * This is the shape a default build takes, and it is the case most likely to rot
 * unnoticed, because every other spec and every screenshot is taken with the
 * example switched on. A framework that only works when something is plugged
 * into it is a framework that breaks the day a deployment ships bare.
 */

test("extension points: a bare build renders the application and nothing else", async ({
  page,
}) => {
  await test.step("1. no point mounts anything anywhere", async () => {
    for (const [path, title] of [
      [routes.dashboard, "Dashboard"],
      [routes.agents, "Agents"],
      [routes.models, "Models"],
    ] as const) {
      // Wait for the page to have actually rendered before asserting an absence.
      // "Nothing is here" is trivially true of a page that has not mounted yet,
      // so without this anchor the whole step would pass on a blank screen.
      await loadPage(page, path, { title });
      await expect(
        allSlots(page),
        `${path} rendered a vendor slot with no extension installed`,
      ).toHaveCount(0);
    }
  });

  await test.step("2. the sidebar shows exactly what the application ships", async () => {
    expect(await navOrder(page)).toEqual(CORE_NAV_ORDER);
  });

  await test.step("3. the app is otherwise whole", async () => {
    // The absence of contributions must not take any of the app with it.
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectShell(page);
    // By the harness as well as the template, because an agent is the pair: the
    // template alone appears on two rows, so matching it would pass on a build that
    // had lost the harness column entirely.
    await expect(
      page
        .getByRole("row")
        .filter({ hasText: agents.k8s.template })
        .filter({ hasText: agents.k8s.harness }),
    ).toHaveCount(1);
  });

  await test.step("4. a route only an extension would contribute is a 404", async () => {
    // The example contributes this path; with nothing installed the router must
    // not have quietly kept a slot for it.
    await loadPage(page, "/example/insights");
    await expect(page.getByTestId("not-found")).toBeVisible();
    await expect(allSlots(page)).toHaveCount(0);
  });
});
