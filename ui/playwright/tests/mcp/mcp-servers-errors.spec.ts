import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// MCP servers — error journey. Client-side url-required validation on create.

test("mcp: url validation", async ({ page }) => {
  // region Creating — client-side validation blocks the POST
  await test.step("blocks create when the URL is empty", async () => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill("e2e-url-validation");
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page.getByText("URL is required")).toBeVisible();
    await expect(page).toHaveURL(/\/mcp\/new/);
  });
});
