import { test, expect } from "../../fixtures/test";
import { agentChat, instances, withScenario } from "../../helpers/app";

/**
 * What sits beside the conversation: the record, and the agent.
 *
 * Its own file because neither is the rail and neither is the transcript. Both were in
 * `agent-rail.spec.ts` under a `chat:` prefix while everything around them was prefixed
 * `chat agent rail:` — which is what a subject in the wrong file looks like from the
 * outside.
 *
 * What they have in common is the reason they exist at all: each answers a question
 * about the conversation that the conversation itself cannot, and each used to cost a
 * navigation to reach. Reference that costs a navigation is reference nobody consults.
 */

const AGENT_CHAT = agentChat(instances.ready);

test("chat: a conversation's record is read without leaving the conversation", async ({
  page,
}) => {
  /*
   * This was an entry in the rail, and reading four facts about a conversation meant
   * leaving it and then finding the way back. Reference that costs a navigation is
   * reference nobody consults, so it is a modal over the conversation now — in the
   * gutter under Share, which is where the conversation's other controls live.
   */
  await page.goto(AGENT_CHAT);
  await page.getByTestId("chat-details").click();

  const fields = page.getByTestId("conversation-details-fields");
  await expect(fields).toBeVisible({ timeout: 30_000 });
  // The record, not a summary: the id is what a reader copies into a CLI.
  await expect(fields).toContainText(instances.ready);

  // Still on the conversation behind it — the point of not making this a page.
  await expect(page).toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`));
  await expect(page.getByTestId("chat-input")).toBeVisible();

  // There is no Edit anywhere on it: an instance has no spec to change. What the agent
  // *is* lives on its AgentTemplate and how it *runs* on its Harness, so a control here
  // would offer something that does not exist.
  await expect(page.getByTestId("agent-details-edit")).toHaveCount(0);
});

test("chat: the agent panel says what the conversation cannot", async ({ page }) => {
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
  await page.goto(AGENT_CHAT);
  const panel = page.getByTestId("chat-agent-context");
  await expect(panel).toBeVisible({ timeout: 30_000 });

  await test.step("0. the agent comes first, and links to its page", async () => {
    const agent = page.getByTestId("chat-agent-context-agent");
    await expect(agent).toHaveText("kagent/k8s-agent-7f3a91c");
    await expect(agent).toHaveAttribute("href", "/agents/kagent/k8s-agent-7f3a91c");
    await expect(panel.getByRole("link").first()).toHaveText("kagent/k8s-agent-7f3a91c");
  });

  await test.step("1. it names the template, and the template is a link", async () => {
    // Not a dead label: every conversation with this agent reads the same template, and
    // the page behind this link is where that is said before anybody edits it.
    await expect(page.getByTestId("chat-agent-context-template")).toBeVisible();
  });

  await test.step("2. the model and the tools, read from that template", async () => {
    await expect(panel).toContainText("Model");
    await expect(panel).toContainText("Tools");
  });

  await test.step("2b. the chat's name and ID, and its details one click away", async () => {
    await expect(panel.getByText("Chat", { exact: true })).toBeVisible();
    await expect(page.getByTestId("chat-agent-context-session-name")).toHaveText("Tuesday cluster review");
    await expect(page.getByTestId("chat-agent-context-session")).toContainText(instances.ready);
    await expect(page.getByTestId("chat-agent-context-revision")).toContainText("rev-7f3a91c");
    await expect(panel).not.toContainText("Conversation");
    await page.getByTestId("chat-agent-context-details").click();
    await expect(page.getByTestId("conversation-details-fields")).toContainText(instances.ready);
    await page.keyboard.press("Escape");
    await expect(page.getByTestId("conversation-details-fields")).toBeHidden();
  });

  await test.step("3. and it can be put away, and stays away", async () => {
    await page.getByTestId("chat-context-collapse").click();
    await expect(panel).toBeHidden();
    await expect(page.getByTestId("chat-context-expand")).toBeVisible();

    // Remembered per reader, like the rail: closing it on one conversation and finding
    // it back on the next is what makes people stop using the control.
    await page.reload();
    await expect(page.getByTestId("chat-context-expand")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("chat-context-expand").click();
    await expect(page.getByTestId("chat-agent-context")).toBeVisible();
  });
});

/** An unnamed chat takes its title from its first message, as the rail does, with the id shown once. */
test("chat: the panel titles an unnamed chat by its first message", async ({ page }) => {
  const autoTitled = "2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0";
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(agentChat(autoTitled));
  const name = page.getByTestId("chat-agent-context-session-name");
  await expect(name).toContainText("Summarise last night's deploy", { timeout: 30_000 });
  await expect(page.getByTestId(`chat-session-${autoTitled}`)).toContainText(
    (await name.textContent()) ?? "",
  );
  await expect(name).not.toContainText(autoTitled.slice(0, 8));
  await expect(page.getByTestId("chat-agent-context-session")).toContainText(autoTitled);

  await page.getByTestId("chat-agent-context-details").click();
  const fields = page.getByTestId("conversation-details-fields");
  await expect(fields).toContainText("Summarise last night's deploy");
  await expect(fields).not.toContainText("Untitled");
});

/**
 * The column the panel sits in, when the conversation could not be read.
 *
 * It holds its width from the first frame so the transcript does not shift sideways when
 * the record lands — but the panel draws from that record, and the toggle beside it is
 * hidden while there is nothing to toggle. Held open on a failed read, that is 288px of
 * empty column with no way to reclaim it, which is worse than the shift it prevents.
 */
test("chat: a conversation that cannot be read gives the column back", async ({
  page,
}) => {
  await page.goto(withScenario(AGENT_CHAT, "error"));

  await expect(page.getByTestId("chat-instance-error")).toBeVisible({
    timeout: 30_000,
  });

  // Zero-width rather than merely empty: the aside is what the reader would be left
  // staring at, and a box that still occupies the row is the fault being checked.
  await expect(page.getByTestId("chat-agent-context")).toHaveCount(0);
  await expect(page.getByTestId("chat-context-collapse")).toBeHidden();
  await expect(page.getByTestId("chat-context-expand")).toBeHidden();
});

/**
 * The context panel's template link, and the way back from the template page.
 *
 * The template page is shared by every conversation with an agent, so reaching it from a
 * chat must not strand the reader on a page whose only exit is the template list.
 */
test("chat: the template opened from a conversation leads back to it", async ({ page }) => {
  // Wide enough for the context panel, as above.
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(AGENT_CHAT);
  const link = page.getByTestId("chat-agent-context-template");
  await expect(link).toBeVisible({ timeout: 30_000 });
  const chatUrl = page.url();
  let historyLength = 0;

  await test.step("1. the link says where it goes", async () => {
    await link.hover();
    await expect(page.getByRole("tooltip")).toContainText("AgentTemplate");
  });

  await test.step("2. the template page offers the way back, across its tabs", async () => {
    await link.click();
    const back = page.getByTestId("template-back");
    await expect(back).toBeVisible({ timeout: 30_000 });
    historyLength = await page.evaluate(() => window.history.length);
    await expect(back).toHaveText("Back to conversation", { timeout: 30_000 });

    await page.getByRole("tab", { name: /Agents/ }).click();
    await expect(back).toHaveText("Back to conversation");
  });

  await test.step("3. and it returns to the same conversation", async () => {
    await page.getByTestId("template-back").click();
    await expect(page).toHaveURL(chatUrl);
    await expect(page.getByTestId("chat-agent-context")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("4. without leaving a second copy of it in history", async () => {
    // Going back pops the template entry rather than pushing the chat again. Read from
    // the history length, since `goBack()` depends on what precedes the first page,
    // and Firefox replaces the initial blank entry where Chromium keeps it.
    expect(await page.evaluate(() => window.history.length)).toBe(historyLength);
  });
});

/**
 * The harness and the MCP server named in the panel each have a details page, and each
 * leads back to the conversation the same way the template page does.
 */
test("chat: the harness, an MCP server and the model open from a conversation and lead back to it", async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(AGENT_CHAT);
  const harness = page.getByTestId("chat-agent-context-harness");
  await expect(harness).toBeVisible({ timeout: 30_000 });
  const chatUrl = page.url();

  await test.step("1. the harness link opens the harness details", async () => {
    await expect(harness).toHaveText("kagent/k8s-agent");
    await harness.click();
    await expect(page).toHaveURL(/\/harnesses\/kagent\/k8s-agent$/);
    await expect(page.getByTestId("harness-runtime")).toHaveText("kagent", { timeout: 30_000 });
    await expect(page.getByTestId("harness-user-k8s-agent-7f3a91c")).toBeVisible();
    await expect(page.getByTestId("harness-user-template-k8s-agent-7f3a91c")).toHaveText("k8s-agent-7f3a91c");
  });

  await test.step("2. and its back button returns to the conversation", async () => {
    const back = page.getByTestId("harness-back");
    await expect(back).toHaveText("Back to conversation");
    await back.click();
    await expect(page).toHaveURL(chatUrl);
    await expect(page.getByTestId("chat-agent-context")).toBeVisible({ timeout: 30_000 });
  });

  await test.step("3. tools explain themselves on focus, and the server name opens the server", async () => {
    // Tools sit under their server, collapsed, with a selected-of-total count.
    const group = page.getByTestId("chat-agent-context-server-group").filter({ hasText: "kagent/kagent-tool-server" });
    const count = group.getByTestId("chat-agent-context-server-count");
    const toggle = group.getByTestId("chat-agent-context-server-toggle");
    await expect(count).toHaveText(/^\d+\/3 tools$/);
    // The whole row toggles: the count text, and the empty space between the name and the count.
    await count.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    const nameBox = await group.getByTestId("chat-agent-context-server").boundingBox();
    const countBox = await count.boundingBox();
    const toggleBox = await toggle.boundingBox();
    if (!nameBox || !countBox || !toggleBox) throw new Error("the server row is not laid out");
    expect(countBox.x - (nameBox.x + nameBox.width)).toBeGreaterThan(8);
    await toggle.click({ position: { x: (nameBox.x + nameBox.width + countBox.x) / 2 - toggleBox.x, y: toggleBox.height / 2 } });
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await toggle.press("Enter");
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    const tool = group.getByTestId("chat-agent-context-tool").filter({ hasText: "k8s_get_events" });
    await expect(group.getByRole("link")).toHaveCount(1);
    await tool.focus();
    await expect(page.getByRole("tooltip")).toContainText("Read recent events for a resource.");
    await group.getByTestId("chat-agent-context-server").click();
    await expect(page).toHaveURL(/\/mcp-servers\/kagent\/kagent-tool-server\?kind=RemoteMCPServer\.api\.kagent\.dev$/);
    const tools = page.getByTestId("mcp-server-tools");
    await expect(tools.getByTestId("tool-server-tool")).toHaveCount(3, { timeout: 30_000 });
    await expect(page.getByTestId("mcp-server-user-k8s-agent-7f3a91c")).toBeVisible();
    await expect(page.getByTestId("mcp-server-users")).toContainText("k8s_get_pods");
    await page.getByTestId("mcp-server-user-tool").filter({ hasText: "k8s_get_pods" }).focus();
    await expect(page.getByRole("tooltip")).toContainText("List pods in a namespace.");
  });

  await test.step("4. and its back button returns to the conversation", async () => {
    const back = page.getByTestId("mcp-server-back");
    await expect(back).toHaveText("Back to conversation");
    await back.click();
    await expect(page).toHaveURL(chatUrl);
    await expect(page.getByTestId("chat-agent-context")).toBeVisible({ timeout: 30_000 });
    await expect(page.getByTestId("chat-agent-context-server").filter({ hasText: "kagent/kagent-tool-server" })).toBeVisible();
  });

  await test.step("5. Cancel on the model it opens returns to the conversation", async () => {
    await page.getByTestId("chat-agent-context-model").click();
    await expect(page).toHaveURL(/\/models\/[^/]+\/[^/]+\/edit$/);
    await expect(page.getByTestId("model-cancel")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("model-cancel").click();
    await expect(page).toHaveURL(chatUrl);
    await expect(page.getByTestId("chat-agent-context")).toBeVisible({ timeout: 30_000 });
  });
});
