import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
  withScenario,
} from "../../helpers/app";

/**
 * MCP servers — the reading journey.
 *
 * `DEFERRED.md` listed this as blocked on a page that did not exist. The page does
 * exist, and did before this spec was written; the entry was simply stale. Which is
 * the argument for porting it now rather than trusting the note.
 *
 * The thing worth pinning here is the tool count per server, because it is derived
 * rather than read: the API returns a server with its discovered tools nested, and
 * the list has to count them. A server that discovered none is the case most likely
 * to be got wrong — an earlier listing of these dropped such a server from the page
 * entirely — so it is asserted explicitly.
 *
 * The row's own expander is covered too: a server's tools are only reachable by
 * opening it, and the row opens anywhere along its length rather than on the chevron
 * alone.
 */

const SERVERS = ["kagent-tool-server", "grafana-mcp", "warehouse-mcp"];

test("mcp servers: the list loads and counts each server's tools", async ({
  page,
}) => {
  await test.step("1. a loading state precedes the data", async () => {
    await page.goto(withScenario(routes.mcpServers, "slow"));
    await expect(page.locator(".ant-spin-spinning")).toBeVisible();
  });

  await test.step("2. every server is listed", async () => {
    for (const name of SERVERS) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1);
    }
    await expect(dataRows(page)).toHaveCount(SERVERS.length);
    await expectSettled(page);
  });

  await test.step("2b. interactive rows opt in to clickable styling", async () => {
    // Row hover/pointer styles are now opt-in through this class so static tables
    // do not imply click behavior.
    for (const name of SERVERS) {
      await expect(rowNamed(page, name)).toHaveClass(/clickable-table-row/);
    }
  });

  await test.step("3. each row shows its namespace, kind and tool count", async () => {
    const local = rowNamed(page, "kagent-tool-server");
    await expect(local).toContainText("kagent");
    await expect(local).toContainText("MCPServer");
    await expect(local).toContainText("3");

    const remote = rowNamed(page, "grafana-mcp");
    await expect(remote).toContainText("platform");
    await expect(remote).toContainText("RemoteMCPServer");
    await expect(remote).toContainText("2");
  });

  await test.step("4. a server that discovered no tools is still listed, as zero", async () => {
    // Not filtered out and not blank. A registered server reporting nothing is more
    // likely to be misconfigured than a busy one, so it is the row a reader most
    // needs to see.
    const empty = rowNamed(page, "warehouse-mcp");
    await expect(empty).toHaveCount(1);
    await expect(empty).toContainText("0");
  });

  await test.step("5. the summary counts servers and tools together", async () => {
    // 3 + 2 + 0 across three servers — a total the page computes, so worth pinning
    // against the rows above rather than restating a constant.
    await expect(page.getByTestId("mcp-servers-summary")).toContainText(
      "3 servers",
    );
    await expect(page.getByTestId("mcp-servers-summary")).toContainText(
      "5 tools",
    );
  });

  await test.step("6. filtering narrows by server, tool name or description", async () => {
    await page.getByTestId("mcp-servers-filters-search").fill("grafana");
    await expect(rowNamed(page, "grafana-mcp")).toHaveCount(1);
    await expect(rowNamed(page, "kagent-tool-server")).toHaveCount(0);

    await page.getByTestId("mcp-servers-filters-search").fill("");
    await expect(dataRows(page)).toHaveCount(SERVERS.length);
  });

  await test.step("7. a row opens anywhere along it, and closes the same way", async () => {
    // `dataRows` rather than `rowNamed`: once the row is open, the panel below it holds
    // tools whose names also contain "grafana", so a by-text row locator matches two
    // rows and the second click lands inside the panel instead of on the row.
    const server = dataRows(page).filter({ hasText: "grafana-mcp" });
    // Deliberately not the chevron: the point of the assertion is the rest of the row.
    // The namespace cell is as far from the expander as a cell gets.
    await server.getByRole("cell").nth(2).click();

    // The panel lists the tools the server discovered, which no column does.
    await expect(page.getByTestId("tool-server-tools")).toBeVisible();

    // And it closes again, so the row is a toggle rather than a one-way reveal.
    //
    // Hidden rather than absent, and the distinction is the table library's: once a row
    // has been expanded it keeps its panel mounted and collapses it with `display: none`
    // (`ExpandedRow.js` — `display: expanded ? null : 'none'`). Asserting on count here
    // fails against a panel the reader cannot see.
    await server.getByRole("cell").nth(2).click();
    await expect(page.getByTestId("tool-server-tools")).toBeHidden();
  });

  await test.step("8. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.mcpServers, {
      scenario: "empty",
      title: "MCP servers",
    });
    await expect(page.getByText("No MCP servers yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });
});
