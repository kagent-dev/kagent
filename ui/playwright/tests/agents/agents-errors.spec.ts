import { test, expect } from "../../fixtures/test";
import {
  agentPage,
  agents,
  dataRows,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";
import { operationCalls, rpc } from "../../helpers/mockCalls";

/**
 * Agents — the error journey.
 *
 * Two failure modes worth pinning: that the page says so rather than going blank,
 * and that it does not quietly report "there are no agents" when the truth is that
 * it could not find out.
 *
 * The list is `AgentTemplateService/ListAgentTemplates` now, because an agent is a
 * template paired with a harness and `status.harnesses[]` carries every pair. That
 * is asserted below rather than assumed: naming the failing call is what makes the
 * message actionable, and it also pins which service this page reads — which changed
 * with the model.
 *
 * **It reads two services, and the order matters when both fail.** Templates are read
 * one namespace at a time, because `ListAgentTemplates` validates its namespace first
 * and refuses an empty one rather than treating it as a wildcard. So `ListNamespaces`
 * is an *input* to the template read, not a nicety beside it: when it fails there are
 * no namespaces to iterate, the template read never runs, and a page that reported only
 * `templates.error` would sit at "no agents" — an empty state describing a backend that
 * was never asked. This scenario fails everything, so the alert correctly names the call
 * that actually failed, which is the namespace one. Step 5 covers the other order.
 */

test("agents: a failed load is reported, not disguised as an empty list", async ({
  page,
}) => {
  await test.step("1. the failure is on screen and names what went wrong", async () => {
    await loadPage(page, routes.agents, { scenario: "error", title: "Agents" });

    const alert = page.getByTestId("agents-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load agents");
    // The backend's own account of the failure reaches the reader rather than a
    // generic message, and it names the call that failed. Asserted as that property
    // rather than as a literal status: a gRPC error is an HTTP 200, so there is no
    // status to report and putting one back to satisfy a string match would be
    // fitting the product to a stale test.
    await expect(alert).toContainText("asked to fail");
    // The call that actually failed, not the one the page is *about*. Reporting
    // "could not list templates" when the namespaces read is what broke sends the
    // reader to the wrong service.
    await expect(alert).toContainText("SystemService/ListNamespaces");
  });

  await test.step("2. it is not mistaken for an empty list", async () => {
    // Every wording the empty state can take, because reporting a failed read as
    // "there are none" is the specific bug this step exists for.
    await expect(page.getByText(/No agents/)).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. no stale data is left on screen from before the failure", async () => {
    await expect(rowNamed(page, agents.k8s.template)).toHaveCount(0);
  });

  await test.step("4. retrying asks the backend again", async () => {
    // Counted as an operation: a retry issues no HTTP request under the substituted
    // transport, so counting requests would report "no retry happened" for a retry
    // that demonstrably did.
    //
    // The namespace call, because that is the one that failed and the one the
    // template read is waiting on. Counting `ListAgentTemplates` would assert that a
    // retry re-ran a read which never ran in the first place, and would fail for a
    // retry that worked.
    const before = await operationCalls(page, rpc.listNamespaces);

    await page
      .getByTestId("agents-error")
      .getByRole("button", { name: "Try again" })
      .click();
    await expect
      .poll(() => operationCalls(page, rpc.listNamespaces), { timeout: 10_000 })
      .toBeGreaterThan(before);
    // Still failing, so the message stays put rather than flickering away.
    await expect(page.getByTestId("agents-error")).toBeVisible();
  });

  await test.step("5. the page recovers once the backend does", async () => {
    await loadPage(page, routes.agents, { scenario: "ok", title: "Agents" });
    await expect(page.getByTestId("agents-error")).toHaveCount(0);
    await expect(rowNamed(page, agents.k8s.template)).toHaveCount(1);
  });
});

/**
 * An agent's own page, when its reads fail.
 *
 * Its two reads answer different questions — what this agent *is*, and what has been
 * said to it — and either can fail alone. Reporting one as the other is what this
 * covers: an agent whose conversations could not be read is not an agent with no
 * conversations.
 */
test("agents: an agent whose conversations cannot be read says so, and is not empty", async ({
  page,
}) => {
  await test.step("1. the failure names the read that failed", async () => {
    await loadPage(page, agentPage(agents.k8s), { scenario: "error" });

    const alert = page.getByTestId("conversations-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("AgentInstanceService/ListAgentInstances");
  });

  await test.step("2. and it is not reported as an agent nobody has talked to", async () => {
    // The distinction the whole page turns on: "no conversations yet" invites
    // starting one, and would be a lie about an agent with forty.
    await expect(page.getByText(/No conversations with this agent yet/)).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(0);
  });

  await test.step("3. an address for an agent whose template is gone says which half is missing", async () => {
    // A real state rather than a 404: a template can be deleted while the
    // conversations cut from it keep running, because an instance runs from the
    // prepared revision it was built against.
    await loadPage(page, agentPage({ template: "was-deleted", harness: "k8s-agent" }), {
      scenario: "ok",
    });
    const missing = page.getByTestId("agent-template-missing");
    await expect(missing).toBeVisible();
    await expect(missing).toContainText("keep running");
  });
});
