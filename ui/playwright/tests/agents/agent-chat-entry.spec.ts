import { test, expect } from "../../fixtures/test";
import {
  agentNewChat,
  agents,
  expectSettled,
  instances,
  loadPage,
  routes,
} from "../../helpers/app";

/**
 * Getting from the list of agents to a conversation with one.
 *
 * Its own spec because the gap it covers is invisible to every other one. The chat
 * specs navigate straight to `/agents/:ns/:id/chat`, which is a fair way to test a
 * chat and no way at all to test that anything *links* to it — and for a while
 * nothing did: the only route to a conversation was an agent card on the dashboard,
 * so from the page named after agents there was no way to talk to one, and the whole
 * suite stayed green.
 *
 * So this asserts the journey rather than the destination: start where a reader
 * starts, click what they would click, and end up somewhere a message can be typed.
 * The chat itself is covered in `chat/chat.spec.ts`.
 *
 * The journey is two hops now rather than one, and that is the shape being tested:
 * the agents list holds agents, and a conversation is inside one. Clicking an agent
 * expecting a chat is exactly the mistake this spec exists to catch.
 */

test("agents: the list is the way in to a conversation, through the agent", async ({
  page,
}) => {
  await test.step("1. the agents list offers each agent by name", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectSettled(page);

    // An agent is named by its template, and the link is the affordance under test:
    // matching a cell's text would pass just as well on a name that links nowhere,
    // which is the exact bug this spec exists for.
    await expect(
      page.getByTestId(`agent-link-kagent-${agents.k8s.template}-${agents.k8s.harness}`),
    ).toBeVisible();
  });

  await test.step("2. clicking an agent offers a new conversation, and creates nothing", async () => {
    await page
      .getByTestId(`agent-link-kagent-${agents.k8s.template}-${agents.k8s.harness}`)
      .click();
    // A conversation that does not exist yet, addressed by the agent. It must not open
    // somebody's existing chat, and it must not create one: an instance created by a
    // click that changes its mind is permanent, holds a prepared revision, and is what
    // left nine empty conversations on the live cluster.
    await expect(page).toHaveURL(new RegExp(`${agentNewChat(agents.k8s)}$`));
    await expectSettled(page);
    await expect(page.getByTestId("new-chat-empty")).toBeVisible();
    await expect(page.getByTestId("chat-input")).toBeVisible();
  });

  await test.step("3. and a conversation already open with it is one click away", async () => {
    // From the rail, which is the point of the rail: arriving at a new conversation does
    // not cut the reader off from the ones they already have. The rail is the only
    // navigation on this page — there is no table, because there is nothing to tabulate
    // until a conversation exists.
    await page.getByTestId(`chat-session-${instances.ready}`).click();

    // That conversation's chat, not a generic one: the namespace and the instance id
    // are both in the path, and opening the wrong one is a failure this would catch.
    await expect(page).toHaveURL(new RegExp(`/agents/kagent/${instances.ready}/chat$`));
    await expect(page.getByTestId("chat-panel")).toBeVisible();
  });

  await test.step("4. a message can be typed straight away", async () => {
    // The box is there without anything being clicked: the instance *is* the
    // conversation, so arriving at one is arriving at somewhere to talk.
    await expect(page.getByTestId("chat-input")).toBeVisible();
    // A composer that cannot be typed into is not an arrival worth asserting.
    await expect(page.getByTestId("chat-input")).toBeEditable();
  });

  await test.step("5. the card names the agent; the list names the conversation", async () => {
    // The card is what opens the agent switcher, so it names the thing being switched
    // — a template and the harness that runs it. It used to name the conversation,
    // which meant it changed every time a reader opened a different conversation with
    // the same agent while the menu behind it listed agents that never changed.
    const identity = page.getByTestId("agent-rail-identity");
    await expect(identity).toContainText(agents.k8s.template);
    await expect(identity).toContainText(`on ${agents.k8s.harness}`);

    // The conversation is still named, one row among its siblings, which is where a
    // reader picks between them.
    await expect(page.getByTestId(`chat-session-${instances.ready}`)).toContainText(
      "Tuesday cluster review",
    );
  });

  await test.step("6. starting another goes to the same call to action, creating nothing", async () => {
    // "New chat" navigates rather than creating, everywhere it appears. A second
    // conversation with one agent is a second instance of the same pair — but it comes
    // into existence when a message is sent, not when a button is clicked.
    const url = page.url();
    await page.getByTestId("chat-new-session").click();

    await expect(page).not.toHaveURL(url);
    await expect(page).toHaveURL(new RegExp(`${agentNewChat(agents.k8s)}$`));
    await expect(page.getByTestId("new-chat-empty")).toBeVisible();
  });
});
