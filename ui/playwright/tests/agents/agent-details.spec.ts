import { test, expect } from "../../fixtures/test";
import {
  agentDetail,
  agentPage,
  agents,
  instances,
  loadPage,
} from "../../helpers/app";

/**
 * A conversation's own record — what the control plane knows about one `AgentInstance`.
 *
 * This is the page that replaced the old agent details page when agents became
 * instances, and `DEFERRED.md` records why the replacement is not a reduction: an
 * instance has no spec. What it has is a state, an operation, the pair it was cut from
 * and a failure, and the configuration lives on the template and the harness it links
 * to. Nothing had driven it in a browser until this spec.
 */

test("agents: a conversation's record reports its state and links to what configures it", async ({
  page,
}) => {
  await loadPage(page, agentDetail(instances.ready));

  await test.step("1. the state, in words, and what the word means", async () => {
    await expect(page.getByTestId("instance-status-card")).toBeVisible();
    // Read to the operator rather than passed through: `ACTOR_STATE_RUNNING` is the
    // controller's vocabulary, and a reader should not have to learn it here.
    const state = page.getByTestId("instance-state");
    await expect(state).toHaveText(/^[A-Z][a-z]/);
    await expect(state).not.toContainText("_");
    // The sentence beside it, which is the part a state tag cannot carry: what being in
    // this state means for what the reader can do next.
    await expect(page.getByTestId("instance-state-meaning")).not.toBeEmpty();
  });

  await test.step("2. it links to its agent and its template rather than restating them", async () => {
    /*
     * The claim the page exists to make. An instance carries no model, no prompt and no
     * tools, so the record links to the two surfaces that do — and a link is checked by
     * where it goes, a label being the easy half to get right.
     */
    await expect(page.getByTestId("instance-agent-link")).toHaveAttribute(
      "href",
      agentPage(agents.k8s),
    );
    await expect(page.getByTestId("instance-template-link")).toHaveAttribute(
      "href",
      /\/agent-templates\//,
    );
    // And it says why the template link is the one that matters, since editing it
    // changes every agent cut from it rather than this conversation alone.
    await expect(page.getByTestId("instance-template-note")).toContainText(
      "not only this one",
    );
  });

  await test.step("3. the record itself, which is what the controller stored", async () => {
    await expect(page.getByTestId("instance-details")).toBeVisible();
    await expect(page.getByTestId("instance-details")).toContainText(instances.ready);
  });
});

test("agents: a record the reader cannot have says which of the two it is", async ({
  page,
}) => {
  await test.step("1. a failed conversation reports the reason it failed", async () => {
    /*
     * The half of "an agent's readiness reason, end to end" that this page owns. A
     * failure with no message renders a sentence saying the record held none — so an
     * empty alert here would be a rendering fault rather than a quiet controller, and
     * the fixture carries a real message to tell those apart.
     */
    await loadPage(page, agentDetail(instances.failed));
    const failure = page.getByTestId("instance-failure");
    await expect(failure).toBeVisible();
    await expect(failure).toContainText("cannot be resumed");
  });

  await test.step("2. somebody else's reads as not found, in the controller's own words", async () => {
    /*
     * Not a bug and not a euphemism: an instance is read as its creator, so the
     * controller genuinely answers `NotFound` for one that is not yours. The page says
     * both readings out loud rather than picking one, because a reader who knows the
     * conversation exists would otherwise think the record was lost.
     */
    await loadPage(page, agentDetail(instances.someoneElses));
    const notFound = page.getByTestId("instance-not-found");
    await expect(notFound).toBeVisible();
    await expect(notFound).toContainText("started by somebody else");
    // No half-drawn record behind the notice.
    await expect(page.getByTestId("instance-details")).toHaveCount(0);
  });

  await test.step("3. a read that failed is not a record that is missing", async () => {
    // The distinction this suite keeps everywhere: "we could not find out" must not
    // render as "there is nothing here". Different id, different action — retry rather
    // than a way back to the list.
    await loadPage(page, agentDetail(instances.ready), { scenario: "error" });
    await expect(page.getByTestId("instance-error")).toBeVisible();
    await expect(page.getByTestId("instance-not-found")).toHaveCount(0);
    await expect(
      page.getByTestId("instance-error").getByRole("button", { name: "Try again" }),
    ).toBeVisible();
  });
});
