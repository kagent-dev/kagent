import { test, expect } from "../../fixtures/test";
import {
  dataRows,
  expectSettled,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";
import {
  LIFECYCLE_TIMEOUT,
  expectLoading,
  clickRefresh,
  confirmDelete,
  confirmation,
  expectRequired,
} from "../../helpers/resource";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * MCP servers — the whole life of one, in a single journey.
 *
 * One test, because a video and a trace are recorded per *test* — see
 * `playwright/README.md`.
 *
 * **There is no update half, and that is the product rather than an omission.**
 * `ToolService` serves create, list and delete and no update: a registered server's
 * address is its identity, so changing where it points is deregistering one server
 * and registering another. The journey is create, read, delete.
 *
 * The thing worth pinning on the read is the tool count per server, because it is
 * derived rather than reported: the API returns a server with its discovered tools
 * nested, and the list has to count them. A server that discovered none is the case
 * most likely to be got wrong, so it is asserted explicitly.
 */

const SEEDED = ["kagent-tool-server", "grafana-mcp", "warehouse-mcp"];

/** The one this journey registers and removes. */
const CREATED = "browser-made-server";

/*
 * A lifecycle is longer than a journey, so it gets its own budget — see
 * `LIFECYCLE_TIMEOUT`. Set per file rather than across the suite, so the tight default
 * keeps doing its job everywhere else.
 */
test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

test("mcp servers: a server is registered, read and deregistered", async ({
  page,
}) => {
  await test.step("1. a loading state precedes the data", async () => {
    await expectLoading(page, routes.mcpServers);
  });

  await test.step("2. every server is listed, with its namespace, kind and tool count", async () => {
    for (const name of SEEDED) {
      await expect(rowNamed(page, name), `"${name}" is missing`).toHaveCount(1);
    }
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
    await expectSettled(page);

    const local = rowNamed(page, "kagent-tool-server");
    await expect(local).toContainText("kagent");
    await expect(local).toContainText("MCPServer");
    await expect(local).toContainText("3");

    const remote = rowNamed(page, "grafana-mcp");
    await expect(remote).toContainText("platform");
    await expect(remote).toContainText("RemoteMCPServer");
    await expect(remote).toContainText("2");

    // Not filtered out and not blank. A registered server reporting no tools is more
    // likely to be misconfigured than a busy one, so it is the row a reader most needs
    // to see.
    const empty = rowNamed(page, "warehouse-mcp");
    await expect(empty).toHaveCount(1);
    await expect(empty).toContainText("0");
  });

  await test.step("3. the summary counts servers and tools together", async () => {
    // 3 + 2 + 0 across three servers — a total the page computes, so worth pinning
    // against the rows above rather than restating a constant.
    const summary = page.getByTestId("mcp-servers-summary");
    await expect(summary).toContainText("3 servers");
    await expect(summary).toContainText("5 tools");
  });

  await test.step("4. a row opens anywhere along it, and closes the same way", async () => {
    // Interactive rows opt in to the clickable styling, so a static table does not
    // imply click behaviour it does not have.
    for (const name of SEEDED) {
      await expect(rowNamed(page, name)).toHaveClass(/clickable-table-row/);
    }

    // `dataRows` rather than `rowNamed`: once the row is open, the panel below it holds
    // tools whose names also contain "grafana", so a by-text row locator matches two
    // rows and the second click lands inside the panel instead of on the row.
    const server = dataRows(page).filter({ hasText: "grafana-mcp" });
    // Deliberately not the chevron: the point of the assertion is the rest of the row.
    // The namespace cell is as far from the expander as a cell gets.
    await server.getByRole("cell").nth(2).click();

    // The panel lists the tools the server discovered, which no column does.
    await expect(page.getByTestId("tool-server-tools")).toBeVisible();

    /*
     * And it closes again, so the row is a toggle rather than a one-way reveal.
     *
     * Hidden rather than absent, and the distinction is the table library's: once a row
     * has been expanded it keeps its panel mounted and collapses it with `display: none`
     * (`ExpandedRow.js` — `display: expanded ? null : 'none'`). Asserting on count here
     * fails against a panel the reader cannot see.
     */
    await server.getByRole("cell").nth(2).click();
    await expect(page.getByTestId("tool-server-tools")).toBeHidden();
  });

  await test.step("5. filtering narrows by server, tool name or description", async () => {
    await page.getByTestId("mcp-servers-filters-search").fill("grafana");
    await expect(rowNamed(page, "grafana-mcp")).toHaveCount(1);
    await expect(rowNamed(page, "kagent-tool-server")).toHaveCount(0);

    await page.getByTestId("mcp-servers-filters-search").fill("");
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  await test.step("6. refreshing re-reads the list, and says that it did", async () => {
    /*
     * Back on `ok` first, and that is the point rather than housekeeping: the scenario
     * persists for the browsing session, so a refresh clicked while `slow` is still in
     * force waits 2.5 seconds per call before it can confirm anything — which reads as
     * a missing feature rather than a slow one. The prompts journey failed exactly that
     * way, on both engines, because its list fans out one call per namespace.
     */
    await loadPage(page, routes.mcpServers, { scenario: "ok", title: "MCP servers" });
    await expectSettled(page);

    const before = await operationCalls(page, rpc.listToolServers);

    await clickRefresh(page);
    await expect
      .poll(() => operationCalls(page, rpc.listToolServers), { timeout: 10_000 })
      .toBeGreaterThan(before);

    // A refresh usually returns the same rows, so a successful one is otherwise
    // indistinguishable from a button that did nothing.
    await expect(page.getByText("Tool servers refreshed")).toBeVisible();
  });

  await test.step("7. the create form marks what it will not submit without", async () => {
    await page.getByTestId("mcp-servers-new").click();
    await page.waitForURL(/\/mcp\/new(\?|$)/);
    await expectSettled(page);

    // Unlike every other form in this app: `validateMcpServerForm` accepts a blank
    // namespace and the controller defaults it, so a mark there would be a lie.
    await expectRequired(page, {
      marked: ["Name", "Server URL"],
      unmarked: ["Namespace", "TLS", "Headers"],
    });
  });

  await test.step("8. a registered server appears on the list, counted as zero tools", async () => {
    await page.getByTestId("mcp-name").fill(CREATED);
    await page.getByTestId("mcp-namespace").fill("kagent");
    await page.getByTestId("mcp-url").fill("mcp.example.com/sse");

    await page.getByTestId("mcp-submit").click();
    await page.waitForURL(/\/mcp(\?|$)/, { timeout: 30_000 });

    // Read back off the list rather than from a toast or a closed form: those two only
    // prove the app believes it worked.
    const row = rowNamed(page, CREATED);
    await expect(row).toHaveCount(1, { timeout: 30_000 });
    await expect(row).toContainText("kagent");
    // A remote URL is a `RemoteMCPServer`; the other branch of the form makes an
    // `MCPServer`. The kind is the form's choice made durable, so it is checked here
    // rather than taken on trust from the segmented control.
    await expect(row).toContainText("RemoteMCPServer");
    // Nothing has connected to it yet, which is the state a cluster reports for a
    // server the controller has not reached. A fixture answering with tools would hide
    // the one state a newly registered server is actually in.
    await expect(row).toContainText("0");
    await expect(dataRows(page)).toHaveCount(SEEDED.length + 1);
  });

  await test.step("9. deleting asks first, and Keep leaves it alone", async () => {
    await page.getByTestId(`delete-${CREATED}`).click();
    const prompt = confirmation(page);
    // The confirmation names the row. "Delete this server?" is no help in a table of
    // four of them, and *which* is the one question the reader has.
    await expect(prompt).toContainText(CREATED);
    await prompt.getByRole("button", { name: "Keep" }).click();
    await expect(rowNamed(page, CREATED)).toHaveCount(1);
    // Waited out rather than assumed gone: the dialog stays visible while it animates
    // away, and the next step's click would land on it.
    await expect(prompt).toHaveCount(0);
  });

  await test.step("10. confirming removes that row and leaves the rest", async () => {
    await confirmDelete(page, CREATED);

    await expect(rowNamed(page, CREATED)).toHaveCount(0, { timeout: 30_000 });
    // "Gone" has to mean that one rather than the read: a list that failed to reload is
    // also a list the row is missing from.
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
    await expect(rowNamed(page, "kagent-tool-server")).toHaveCount(1);
  });

  await test.step("11. an empty result says so instead of showing a bare table", async () => {
    await loadPage(page, routes.mcpServers, {
      scenario: "empty",
      title: "MCP servers",
    });
    await expect(page.getByText("No MCP servers yet.")).toBeVisible();
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("12. a failed load is reported, not disguised as an empty list", async () => {
    await loadPage(page, routes.mcpServers, {
      scenario: "error",
      title: "MCP servers",
    });

    const alert = page.getByTestId("mcp-servers-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load MCP servers");
    // The backend's own account of the failure reaches the reader rather than a generic
    // message, and it names the call that failed — which an HTTP status never did.
    await expect(alert).toContainText("asked to fail");
    await expect(alert).toContainText("ToolService/ListToolServers");

    // The distinction this step exists for: "there are none" and "we could not find
    // out" lead a reader to opposite conclusions, and only one of them is true.
    await expect(page.getByText("No MCP servers yet.")).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("13. retrying asks the backend again, and it recovers", async () => {
    const before = await operationCalls(page, rpc.listToolServers);

    await page.getByRole("button", { name: "Try again" }).click();
    await expect
      .poll(() => operationCalls(page, rpc.listToolServers), { timeout: 10_000 })
      .toBeGreaterThan(before);

    // A failure that cannot clear is indistinguishable from a broken page, so the
    // recovery is as much a part of the contract as the message.
    await loadPage(page, routes.mcpServers, { title: "MCP servers" });
    await expect(page.getByTestId("mcp-servers-error")).toHaveCount(0);
    await expect(rowNamed(page, "kagent-tool-server")).toHaveCount(1);
  });
});
