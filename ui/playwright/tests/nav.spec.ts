import { test, expect } from "../fixtures/test";
import { loadPage } from "../helpers/page";
import { gotoView, gotoCreate } from "../helpers/nav";

// Exercises nav.ts: dropdown-based navigation between listing pages, asserting
// each destination's <h1>. Covers real client-side routing (not just page loads).
test("navigates between sections via the View menu", async ({ page }) => {
  await loadPage(page, "/", { heading: "Agents" });

  await gotoView(page, "Models", "**/models");
  await expect(page.getByRole("heading", { level: 1, name: "Models" })).toBeVisible();

  await gotoView(page, "MCP & tools", "**/mcp");
  await expect(page.getByRole("heading", { level: 1, name: "MCP & tools" })).toBeVisible();
});

test("navigates to create pages via the Create menu", async ({ page }) => {
  await loadPage(page, "/", { heading: "Agents" });

  await gotoCreate(page, "New Agent", "**/agents/new");
  await expect(page.getByRole("heading", { level: 1, name: "New Agent", exact: true })).toBeVisible();

  await gotoCreate(page, "New Agent Harness", "**/agents/new-harness");
  await expect(page.getByRole("heading", { level: 1, name: "New Agent Harness" })).toBeVisible();

  await gotoCreate(page, "New Model", "**/models/new");
  await expect(page.getByRole("heading", { level: 1, name: "New Model" })).toBeVisible();

  await gotoCreate(page, "New MCP Server", "**/mcp/new");
  await expect(page.getByRole("heading", { level: 1, name: "New MCP server" })).toBeVisible();

  await gotoCreate(page, "New prompt library", "**/prompts/new");
  await expect(page.getByRole("heading", { level: 1, name: "New Prompt Library" })).toBeVisible();
});
