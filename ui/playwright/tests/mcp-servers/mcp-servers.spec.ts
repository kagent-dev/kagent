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
  dialog,
  expectRequired,
  pressUntil,
  selectOption,
} from "../../helpers/resource";
import { operationCalls, rpc } from "../../helpers/mockCalls";
import { background } from "../../helpers/style";

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

    const builtIn = rowNamed(page, "kagent-tool-server");
    await expect(builtIn).toContainText("kagent");
    await expect(builtIn).toContainText("RemoteMCPServer");
    await expect(builtIn).toContainText("3");

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

  await test.step("4. only the expand cell unfolds a row's tools", async () => {
    for (const name of SEEDED) {
      await expect(rowNamed(page, name)).toHaveClass(/link-table-row/);
    }

    // `dataRows` rather than `rowNamed`: an open panel holds tools whose names also contain "grafana".
    const server = dataRows(page).filter({ hasText: "grafana-mcp" });
    const expandCell = server.getByRole("cell").first();
    const nameCell = server.getByRole("cell").nth(1);

    // The cell lights up on its own; the rest of the row does not.
    await expandCell.hover({ position: { x: 4, y: 4 } });
    await expect.poll(() => background(expandCell)).not.toBe(await background(nameCell));

    // Its padding, not the button inside it, so the whole cell is the target.
    await expandCell.click({ position: { x: 4, y: 4 } });
    await expect(page.getByTestId("tool-list")).toBeVisible();
    await expect(page).toHaveURL(/\/mcp-servers(\?|$)/);

    // Hidden rather than absent: antd keeps an expanded panel mounted under `display: none`.
    await expandCell.click({ position: { x: 4, y: 4 } });
    await expect(page.getByTestId("tool-list")).toBeHidden();
  });

  await test.step("4b. a tool name opens its server searched to that tool, and back keeps the filter", async () => {
    await page.getByTestId("mcp-servers-filters-search").fill("grafana");
    await expect(page).toHaveURL(/[?&]q=grafana/);
    await dataRows(page).filter({ hasText: "grafana-mcp" }).getByRole("cell").first().click({ position: { x: 4, y: 4 } });
    await page.getByTestId("tool-list-link").filter({ hasText: "grafana_query" }).click();
    await expect(page).toHaveURL(/\/mcp-servers\/platform\/grafana-mcp\?kind=RemoteMCPServer\.api\.kagent\.dev&q=grafana_query&exact=1$/);
    await expect(page.getByTestId("mcp-server-search-term")).toHaveText(["grafana_query"]);
    await expect(page.getByTestId("mcp-server-search-exact")).toBeChecked();
    await expect(page.getByTestId("mcp-server-tools").getByTestId("tool-list-item")).toHaveText([/^grafana_query/]);

    await expect(page.getByTestId("mcp-server-back")).toHaveText("MCP servers");
    await page.getByTestId("mcp-server-back").click();
    await expect(page).toHaveURL(/\/mcp-servers\?(.*&)?q=grafana(&|$)/);
    await expect(page.getByTestId("mcp-servers-filters-search")).toHaveValue("grafana");
    await expect(dataRows(page)).toHaveCount(1);

    await page.getByTestId("mcp-servers-filters-search").fill("");
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  await test.step("4c. the rest of the row opens the server, in a new tab with a modifier", async () => {
    const namespaceCell = dataRows(page).filter({ hasText: "grafana-mcp" }).getByRole("cell").nth(2);
    const server = /\/mcp-servers\/platform\/grafana-mcp\?kind=RemoteMCPServer\.api\.kagent\.dev$/;
    const opened = page.context().waitForEvent("page");
    await namespaceCell.click({ modifiers: ["ControlOrMeta"] });
    const tab = await opened;
    await expect(tab).toHaveURL(server);
    await tab.close();

    await namespaceCell.click();
    await expect(page).toHaveURL(server);
    await page.getByTestId("mcp-server-back").click();
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  const input = page.getByTestId("mcp-server-search-input");
  const terms = page.getByTestId("mcp-server-search-term");
  const tools = page.getByTestId("mcp-server-tools").getByTestId("tool-list-item");

  await test.step("4d. a details page opens with every tool", async () => {
    await loadPage(page, "/mcp-servers/kagent/kagent-tool-server?kind=RemoteMCPServer.api.kagent.dev", {
      title: "MCP server kagent-tool-server",
    });
    await expect(tools).toHaveCount(3, { timeout: 30_000 });
  });

  await test.step("4e. typing filters at once, and Enter keeps it as a term", async () => {
    await input.fill("k8s_get");
    await expect(tools).toHaveText([/^k8s_get_events/, /^k8s_get_pods/]);
    await input.press("Enter");
    await expect(terms).toHaveText(["k8s_get"]);
    await expect(input).toHaveValue("");
    await expect(page).toHaveURL(/[?&]q=k8s_get(&|$)/);
  });

  await test.step("4f. exact match leaves out names that only start with a term", async () => {
    // Clicked rather than `check()`: the box follows the address, which updates in a transition.
    await page.getByTestId("mcp-server-search-exact").click();
    await expect(page.getByTestId("mcp-server-search-exact")).toBeChecked();
    await expect(page).toHaveURL(/[?&]exact=1(&|$)/);
    await expect(page.getByTestId("mcp-server-tools-no-match")).toBeVisible();
    await page.getByTestId("mcp-server-search-exact").click();
    await expect(page.getByTestId("mcp-server-search-exact")).not.toBeChecked();
    await expect(tools).toHaveCount(2);
  });

  await test.step("4g. several terms match any of them", async () => {
    await terms.getByText("k8s_get").click();
    await expect(terms).toHaveCount(0);
    await input.fill("events");
    await page.getByTestId("mcp-server-search-add").click();
    await input.fill("describe");
    await input.press("Enter");
    await expect(terms).toHaveText(["events", "describe"]);
    await expect(tools).toHaveText([/^k8s_describe_resource/, /^k8s_get_events/]);
  });

  await test.step("4h. the usage tab is searched too, and a tool there narrows the search to it", async () => {
    await page.getByRole("tab", { name: "Usage" }).click();
    await expect(page).toHaveURL(/[?&]tab=usage(&|$)/);
    const usage = page.getByTestId("mcp-server-users");
    await expect(page.getByTestId("mcp-server-user-k8s-agent-7f3a91c")).toBeVisible();
    await expect(usage.getByTestId("mcp-server-user-tool")).toHaveText([/^k8s_get_events/]);

    await page.getByTestId("mcp-server-search-clear").click();
    await expect(usage.getByTestId("mcp-server-user-tool")).toHaveText([/^k8s_get_events/, /^k8s_get_pods/]);
    await usage.getByTestId("tool-list-link").filter({ hasText: "k8s_get_pods" }).click();
    await expect(terms).toHaveText(["k8s_get_pods"]);
    await expect(page.getByTestId("mcp-server-search-exact")).toBeChecked();
    await expect(input).toBeFocused();
    await expect(page.getByRole("tab", { name: "Usage" })).toHaveAttribute("aria-selected", "true");
    await expect(usage.getByTestId("mcp-server-user-tool")).toHaveText([/^k8s_get_pods/]);
  });

  await test.step("4i. a tool in the tools list narrows the search the same way", async () => {
    await page.getByRole("tab", { name: /^Tools/ }).click();
    await page.getByTestId("mcp-server-search-exact").click();
    await expect(page.getByTestId("mcp-server-search-exact")).not.toBeChecked();
    await terms.getByText("k8s_get_pods").click();
    await page.getByTestId("mcp-server-tools").getByTestId("tool-list-link").filter({ hasText: "k8s_get_events" }).click();
    await expect(terms).toHaveText(["k8s_get_events"]);
    await expect(input).toBeFocused();
    await expect(tools).toHaveText([/^k8s_get_events/]);
  });

  await test.step("4j. back on the list", async () => {
    await loadPage(page, routes.mcpServers, { title: "MCP servers" });
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
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

  });

  await test.step("7. the create form marks what it will not submit without", async () => {
    await page.getByTestId("mcp-servers-new").click();
    await page.waitForURL(/\/mcp-servers\/new(\?|$)/);
    await expectSettled(page);

    // The namespace is a dropdown that always holds a value, and is required.
    await expectRequired(page, {
      marked: ["Name", "Namespace", "Server URL"],
      unmarked: ["TLS", "Headers sent to the server", "Connection timeout"],
    });
    await expect(page.getByTestId("mcp-namespace")).toHaveClass(/ant-select/);
    await expect(page.getByTestId("form-back-link")).toContainText("MCP servers");

    // Create is held back; an empty required field is flagged once it is left.
    await expect(page.getByTestId("mcp-submit")).toHaveAttribute("aria-disabled", "true");
    await page.getByTestId("mcp-name").focus();
    await page.getByTestId("mcp-url").focus();
    await page.getByTestId("mcp-url").blur();
    await expect(page.getByText("A server name is required.")).toBeVisible();
    await expect(page.getByText("A server URL is required.")).toBeVisible();

    // A host with spaces is rejected as typed, and no name is guessed from it.
    await page.getByTestId("mcp-url").fill("not a url");
    await expect(page.getByText("Enter a valid host and path, for example api.example.com/mcp.")).toBeVisible();
    await expect(page.getByTestId("mcp-name")).toHaveValue("");
  });

  await test.step("8. a registered server appears on the list, counted as zero tools", async () => {
    await page.getByTestId("mcp-name").fill(CREATED);
    await selectOption(page, "mcp-namespace", "kagent");
    await page.getByTestId("mcp-url").fill("mcp.example.com/sse");

    // A dirty form asks before it is left.
    await page.getByTestId("mcp-cancel").click();
    await expect(page.getByTestId("mcp-discard-body")).toBeVisible();
    await pressUntil(page.getByRole("button", { name: "Keep editing" }), async () => {
      await expect(page.getByTestId("mcp-discard-body")).toBeHidden();
    });

    await page.getByTestId("mcp-submit").click();
    await page.waitForURL(/\/mcp-servers(\?|$)/, { timeout: 30_000 });

    // Read back off the list rather than from a toast or a closed form: those two only
    // prove the app believes it worked.
    const row = rowNamed(page, CREATED);
    await expect(row).toHaveCount(1, { timeout: 30_000 });
    await expect(row).toContainText("kagent");
    // The form creates a `RemoteMCPServer`; check the durable kind rather than taking
    // the submitted request on trust.
    await expect(row).toContainText("RemoteMCPServer");
    // Nothing has connected to it yet, which is the state a cluster reports for a
    // server the controller has not reached. A fixture answering with tools would hide
    // the one state a newly registered server is actually in.
    await expect(row).toContainText("0");
    await expect(dataRows(page)).toHaveCount(SEEDED.length + 1);
  });

  await test.step("9. deleting asks first, and Keep leaves it alone", async () => {
    await page.getByTestId(`delete-${CREATED}`).click();
    const prompt = dialog(page);
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
