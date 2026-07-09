import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// Sub-stage 2.6 — delete an agent from the list. Delete is reachable via the
// per-card "Agent options" menu → "Delete" → confirm dialog. deleteAgent issues
// DELETE /api/agents/<ns>/<name> (captured by the stub). The stub is stateless,
// so we assert the captured DELETE + dialog close rather than row removal.

async function openDeleteDialog(page: import("@playwright/test").Page) {
  await page.getByRole("button", { name: "Agent options" }).first().click();
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
}

test.describe("agent delete / lifecycle", () => {
  test("deletes an agent via the confirm dialog", async ({ page, mock }) => {
    await loadPage(page, "/", { heading: "Agents" });
    await expect(page.getByText("e2e-agent")).toBeVisible();

    await openDeleteDialog(page);
    await page.getByRole("alertdialog").getByRole("button", { name: "Delete" }).click();

    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    const req = await mock.lastRequest("DELETE", "/api/agents/default/e2e-agent");
    expect(req, "expected a captured DELETE /api/agents/<ns>/<name>").not.toBeNull();
  });

  test("cancel leaves the agent untouched", async ({ page, mock }) => {
    await loadPage(page, "/", { heading: "Agents" });

    await openDeleteDialog(page);
    await page.getByRole("alertdialog").getByRole("button", { name: "Cancel" }).click();

    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    const deletes = (await mock.capturedRequests()).filter((r) => r.method === "DELETE");
    expect(deletes).toHaveLength(0);
  });
});
