import type { Locator } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/app";

/** Geometry rather than a screenshot baseline: theme tokens can change, alignment cannot. */
async function geometry(table: Locator) {
  return table.evaluate((element) => {
    const row = element.querySelector("tbody tr.ant-table-row")!;
    const cell = row.querySelector("td")!;
    const style = getComputedStyle(cell);
    const shell = element.closest(".ant-table") ?? element.querySelector(".ant-table")!;
    return {
      height: row.getBoundingClientRect().height,
      padding: [style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft],
      radius: getComputedStyle(shell).borderRadius,
      widths: [...row.querySelectorAll("td")].map((entry) => Math.round(entry.getBoundingClientRect().width)),
    };
  });
}

for (const name of ["agents", "unmapped"] as const) {
  for (const width of [1280, 1600]) {
    test(`agents namespace loading: ${name} keeps its row geometry at ${width}px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 1100 });
      await loadPage(page, name === "agents" ? "/agents" : "/agents/unmapped", { scenario: "slow" });
      const loading = page.getByTestId(`${name}-loading`);
      await expect(loading).toBeVisible();
      await expect(loading).toHaveAttribute("aria-busy", "true");
      await expect(page.getByTestId("agents-summary")).toHaveCount(0);
      await expect(page.getByRole("button", { name: /Delete conversations all/ })).toHaveCount(0);
      const before = await geometry(loading);
      await loading.screenshot({ path: testInfo.outputPath("loading.png") });

      await expect(loading).toHaveCount(0, { timeout: 20_000 });
      const table = page.getByTestId(`${name}-table`);
      await expect(table).toBeVisible();
      expect(await geometry(table)).toEqual(before);
      await table.screenshot({ path: testInfo.outputPath("loaded.png") });
      if (name === "agents") {
        await expect(page.getByTestId("agents-summary")).toHaveText("9 of 9 agents");
      } else {
        await expect(page.getByTestId("unmapped-table").getByRole("link")).toHaveCount(1);
      }
    });
  }

  test(`agents namespace loading: ${name} shows namespace failures instead of an empty result`, async ({ page }) => {
    await loadPage(page, name === "agents" ? "/agents" : "/agents/unmapped", { scenario: "error" });
    await expect(page.getByTestId(`${name}-error`)).toContainText("SystemService/ListNamespaces");
    await expect(page.getByTestId(`${name}-loading`)).toHaveCount(0);
    await expect(page.getByText(/No agents yet|No visible conversations are unmapped/)).toHaveCount(0);
    await expect(page.getByRole("button", { name: /Delete conversations all/ })).toHaveCount(0);
  });
}
