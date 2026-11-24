import { test, expect } from "../../fixtures/test";
import { agentConversation, instances, withScenario } from "../../helpers/app";

/**
 * What sits beside the conversation: the record, and the agent.
 *
 * Its own file because neither is the rail and neither is the transcript. Both were in
 * `agent-rail.spec.ts` under a `conversation:` prefix while everything around them was prefixed
 * `conversation agent rail:` — which is what a subject in the wrong file looks like from the
 * outside.
 *
 * What they have in common is the reason they exist at all: each answers a question
 * about the conversation that the conversation itself cannot, and each used to cost a
 * navigation to reach. Reference that costs a navigation is reference nobody consults.
 */

const AGENT_CONVERSATION = agentConversation(instances.ready);

test("conversation: a conversation's record is read without leaving the conversation", async ({
  page,
}) => {
  /*
   * This was an entry in the rail, and reading four facts about a conversation meant
   * leaving it and then finding the way back. Reference that costs a navigation is
   * reference nobody consults, so it is a modal over the conversation now — in the
   * gutter under Share, which is where the conversation's other controls live.
   */
  await page.goto(AGENT_CONVERSATION);
  await page.getByTestId("conversation-details").click();

  const fields = page.getByTestId("conversation-details-fields");
  await expect(fields).toBeVisible({ timeout: 30_000 });
  // The record, not a summary: the id is what a reader copies into a CLI.
  await expect(fields).toContainText(instances.ready);

  // Still on the conversation behind it — the point of not making this a page.
  await expect(page).toHaveURL(new RegExp(`/agents/${instances.ready}/conversation$`));
  await expect(page.getByTestId("conversation-input")).toBeVisible();

  // There is no Edit anywhere on it: an instance has no spec to change. What the agent
  // *is* lives on its AgentTemplate and how it *runs* on its Harness, so a control here
  // would offer something that does not exist.
  await expect(page.getByTestId("agent-details-edit")).toHaveCount(0);
});

test("conversation: the agent panel says what the conversation cannot", async ({ page }) => {
  /*
   * A conversation is an `AgentInstance`, and an instance holds no configuration —
   * what model is answering, what it was told to do and what tools it can reach all
   * live on the `AgentTemplate` it was cut from. So this panel reads the template,
   * which is also a thing the reader can open and change.
   */
  /*
   * Wider than the project's 1280, because the panel folds itself away below 1440 —
   * see `CONTEXT_COLLAPSES_BELOW`. At the default width this asserts the responsive
   * behaviour rather than the panel's content, which is what it is about.
   */
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(AGENT_CONVERSATION);
  const panel = page.getByTestId("conversation-agent-context");
  await expect(panel).toBeVisible({ timeout: 30_000 });

  await test.step("1. it names the template, and the template is a link", async () => {
    // Not a dead label: every conversation with this agent reads the same template, and
    // the page behind this link is where that is said before anybody edits it.
    await expect(page.getByTestId("conversation-agent-context-template")).toBeVisible();
  });

  await test.step("2. the model and the tools, read from that template", async () => {
    await expect(panel).toContainText("Model");
    await expect(panel).toContainText("Tools");
  });

  await test.step("3. and it can be put away, and stays away", async () => {
    await page.getByTestId("conversation-context-collapse").click();
    await expect(panel).toBeHidden();
    await expect(page.getByTestId("conversation-context-expand")).toBeVisible();

    // Remembered per reader, like the rail: closing it on one conversation and finding
    // it back on the next is what makes people stop using the control.
    await page.reload();
    await expect(page.getByTestId("conversation-context-expand")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("conversation-context-expand").click();
    await expect(page.getByTestId("conversation-agent-context")).toBeVisible();
  });
});

/**
 * The column the panel sits in, when the conversation could not be read.
 *
 * It holds its width from the first frame so the transcript does not shift sideways when
 * the record lands — but the panel draws from that record, and the toggle beside it is
 * hidden while there is nothing to toggle. Held open on a failed read, that is 288px of
 * empty column with no way to reclaim it, which is worse than the shift it prevents.
 */
test("conversation: a conversation that cannot be read gives the column back", async ({
  page,
}) => {
  await page.goto(withScenario(AGENT_CONVERSATION, "error"));

  await expect(page.getByTestId("conversation-instance-error")).toBeVisible({
    timeout: 30_000,
  });

  // Zero-width rather than merely empty: the aside is what the reader would be left
  // staring at, and a box that still occupies the row is the fault being checked.
  await expect(page.getByTestId("conversation-agent-context")).toHaveCount(0);
  await expect(page.getByTestId("conversation-context-collapse")).toBeHidden();
  await expect(page.getByTestId("conversation-context-expand")).toBeHidden();
});

/**
 * The context panel's template link, and the way back from the template page.
 *
 * The template page is shared by every conversation with an agent, so reaching it from a
 * conversation must not strand the reader on a page whose only exit is the template list.
 */
test("conversation: the template opened from a conversation leads back to it", async ({ page }) => {
  // Wide enough for the context panel, as above.
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(AGENT_CONVERSATION);
  const link = page.getByTestId("conversation-agent-context-template");
  await expect(link).toBeVisible({ timeout: 30_000 });
  const conversationUrl = page.url();
  let historyLength = 0;

  await test.step("1. the link says where it goes", async () => {
    await link.hover();
    await expect(page.getByRole("tooltip")).toContainText("AgentTemplate");
  });

  await test.step("2. the template page offers the way back, across its tabs", async () => {
    await link.click();
    const back = page.getByTestId("form-back-link");
    await expect(back).toBeVisible({ timeout: 30_000 });
    historyLength = await page.evaluate(() => window.history.length);
    await expect(back).toHaveText("Conversation", { timeout: 30_000 });

    await page.getByRole("tab", { name: /Agents/ }).click();
    await expect(back).toHaveText("Conversation");
  });

  await test.step("3. and it returns to the same conversation", async () => {
    await page.getByTestId("form-back-link").click();
    await expect(page).toHaveURL(conversationUrl);
    await expect(page.getByTestId("conversation-agent-context")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("4. without leaving a second copy of it in history", async () => {
    // Going back pops the template entry rather than pushing the conversation again. Read from
    // the history length, since `goBack()` depends on what precedes the first page,
    // and Firefox replaces the initial blank entry where Chromium keeps it.
    expect(await page.evaluate(() => window.history.length)).toBe(historyLength);
  });
});
