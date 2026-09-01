import { test, expect } from "../../fixtures/test";
import { dataRows, loadPage, rowNamed, routes } from "../../helpers/app";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * MCP servers — the error journey.
 *
 * The same contract the agents and models error journeys assert, on purpose: a
 * failed list load should look and behave identically wherever it happens, and
 * several pages pinning one contract is what catches the first page to drift from
 * it.
 */

test("mcp servers: a failed load is reported, not disguised as an empty list", async ({
  page,
}) => {
  await test.step("1. the failure is on screen and names what went wrong", async () => {
    await loadPage(page, routes.mcpServers, { scenario: "error", title: "MCP servers" });

    const alert = page.getByTestId("mcp-servers-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load MCP servers");
    // The backend's own account of the failure reaches the user rather than a
    // generic message, and it names the call that failed — which the HTTP status
    // this used to assert never did. Asserted as that property, not as a literal
    // status: there is no status to report now, and putting one back in the
    // message to satisfy a string match would be fitting the product to a stale
    // test.
    await expect(alert).toContainText("asked to fail");
    await expect(alert).toContainText("ToolService/ListToolServers");
  });

  await test.step("2. it is not mistaken for an empty list", async () => {
    // The distinction this whole spec exists for: "there are none" and "we could not
    // find out" lead a reader to opposite conclusions, and only one of them is true.
    await expect(page.getByText("No MCP servers yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. retrying asks the backend again", async () => {
    const before = await operationCalls(page, rpc.listToolServers);

    await page.getByRole("button", { name: "Try again" }).click();
    await expect
      .poll(() => operationCalls(page, rpc.listToolServers), { timeout: 10_000 })
      .toBeGreaterThan(before);
  });

  await test.step("4. the page recovers when the backend does", async () => {
    // A failure that cannot clear is indistinguishable from a broken page, so the
    // recovery is as much a part of the contract as the message.
    await loadPage(page, routes.mcpServers, { title: "MCP servers" });
    await expect(page.getByTestId("mcp-servers-error")).toHaveCount(0);
    await expect(rowNamed(page, "kagent-tool-server")).toHaveCount(1);
  });
});
