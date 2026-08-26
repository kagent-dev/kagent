import { test, expect } from "../../fixtures/test";
import {
  agentChat,
  agentNewChat,
  agentPage,
  agents,
  dataRows,
  expectSettled,
  instances,
  loadPage,
  rowNamed,
  routes,
} from "../../helpers/app";

/**
 * One agent, and the conversations people have had with it.
 *
 * The surface between the agents list and a chat. Three things about it are worth
 * pinning, and each is a claim a screenshot cannot check:
 *
 * - **The list is this agent's conversations, narrowed by the server.** Two agents
 *   cut from one template must not show each other's, and `ListAgentInstances` takes
 *   both halves of the pair for exactly that reason. A client-side filter on the
 *   template alone would pass every visual inspection and merge the two.
 * - **A conversation has a name, and an unnamed one is not a bare UUID.** That was
 *   the thing that made the old list unreadable — rows of hex under a heading — so
 *   what it degrades to is asserted rather than assumed.
 * - **Somebody else's conversation is listed and cannot be opened.** `all_creators`
 *   is always asked for now, and `GetAgentInstance` is scoped to its creator, so
 *   such a row is genuinely a dead end. Offering a link into a chat that answers
 *   `NotFound` would be worse than not listing it at all.
 */

test("agents: one agent lists its own conversations, and only its own", async ({
  page,
}) => {
  await test.step("1. the agents list leads here", async () => {
    await loadPage(page, routes.agents, { title: "Agents" });
    await expectSettled(page);

    // Clicked rather than navigated to: what is under test is that the list is a way
    // in, which a `page.goto` to the destination could never fail on.
    //
    // Through the agent's name and then the rail. The name opens a new conversation,
    // which is what a reader clicking an agent wants; the agent's own page — what it
    // already has — is one step further, reached from the rail that page carries.
    await page.getByTestId("agent-link-kagent-shared-brain-k8s-agent").click();
    await page.getByTestId("agent-nav-agent-conversations").click();
    await expect(page).toHaveURL(new RegExp(`${agentPage(agents.sharedOnK8s)}$`));
    await expectSettled(page);
  });

  await test.step("2. the rail names the agent by its template and says what runs it", async () => {
    // The rail, not a page heading: this page has none. The rail names the agent, holds
    // the way back and offers a new conversation, so a title repeating the name and
    // three buttons repeating rail entries were a band across the top saying nothing.
    await expect(page.getByTestId("agent-rail-identity")).toContainText("shared-brain");
    await expect(page.getByTestId("agent-identity")).toContainText("k8s-agent");
  });

  await test.step("3. it lists this pair's conversation and not its twin's", async () => {
    // The assertion the server-side filter exists for. `shared-brain` is two agents;
    // each has exactly one conversation, and they are indistinguishable on
    // everything but the harness. Narrowed on the template alone, both would appear
    // here and the page would look perfectly reasonable.
    await expect(dataRows(page)).toHaveCount(1);
    await expect(rowNamed(page, "Drafting the runbook")).toHaveCount(1);
    await expect(page.getByTestId("conversations-table")).not.toContainText("2b6e0c45");
  });

  await test.step("4. the other agent cut from the same template has the other one", async () => {
    await loadPage(page, agentPage(agents.sharedOnFastLane));
    await expectSettled(page);

    await expect(dataRows(page)).toHaveCount(1);
    await expect(page.getByTestId("conversations-table")).toContainText("2b6e0c45");
    await expect(page.getByTestId("conversations-table")).not.toContainText(
      "Drafting the runbook",
    );
  });
});

test("agents: a conversation is named by the reader, and never renders as a bare UUID", async ({
  page,
}) => {
  await loadPage(page, agentPage(agents.k8s));
  await expectSettled(page);

  await test.step("1. a named conversation reads as its name", async () => {
    await expect(page.getByTestId(`conversation-link-${instances.ready}`)).toHaveText(
      "Tuesday cluster review",
    );
  });

  await test.step("2. an unnamed one says it is untitled rather than showing its key", async () => {
    const untitled = page.getByTestId(`conversation-link-${instances.suspended}`);
    // Not the UUID. A database key presented under a "Conversation" heading reads as
    // a name somebody chose, and eight rows of it are indistinguishable at a glance
    // — which is the specific failure that started this rework.
    await expect(untitled).not.toHaveText(instances.suspended);
    await expect(untitled).toContainText("Untitled");
    // The short id is still there: two untitled conversations with one agent have
    // nothing else to tell them apart.
    await expect(untitled).toContainText(instances.suspended.slice(0, 8));
  });

  await test.step("3. renaming one changes what the list shows", async () => {
    await page.getByTestId(`conversation-rename-${instances.suspended}`).click();
    // The box opens *empty* for an unnamed conversation rather than pre-filled with
    // the placeholder, or clearing a title would be impossible: saving would turn an
    // honest "Untitled" into a literal one.
    const field = page.getByTestId("conversation-rename-input").locator("input");
    await expect(field).toHaveValue("");

    await field.fill("Rollback rehearsal");
    await page.getByRole("button", { name: "Save" }).click();

    // The list is the proof, not the toast: a success message says the app thinks it
    // worked, and a rename that failed would still show one on a broken backend.
    await expect(rowNamed(page, "Rollback rehearsal")).toHaveCount(1);
    await expect(
      page.getByTestId(`conversation-link-${instances.suspended}`),
    ).toHaveText("Rollback rehearsal");
  });

  await test.step("4. a name the controller would refuse is refused before the round trip", async () => {
    await page.getByTestId(`conversation-rename-${instances.suspended}`).click();
    const field = page.getByTestId("conversation-rename-input").locator("input");
    await field.fill(" leading space");

    // Refused rather than trimmed, which is what the controller does — and the
    // reason matters: silently rewriting what somebody typed reads on screen as a
    // rename that did not take.
    await expect(page.getByTestId("conversation-rename-problem")).toContainText(
      "cannot start or end with a space",
    );
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
    await page.getByRole("button", { name: "Cancel" }).click();
  });

  await test.step("5. clearing a name puts it back to being untitled", async () => {
    await page.getByTestId(`conversation-rename-${instances.suspended}`).click();
    const field = page.getByTestId("conversation-rename-input").locator("input");
    await expect(field).toHaveValue("Rollback rehearsal");
    await field.fill("");
    await page.getByRole("button", { name: "Save" }).click();

    await expect(
      page.getByTestId(`conversation-link-${instances.suspended}`),
    ).toContainText("Untitled");
  });
});

/**
 * Auto-titling, which is the other half of naming a conversation.
 *
 * Its own test rather than a step, because it needs a different seed state: a
 * conversation nobody has named that nonetheless has something said in it. Every
 * other seeded transcript belongs to a *named* conversation, where the stored name
 * wins and this path is unreachable.
 *
 * And it is asserted on the chat page deliberately. Deriving a title needs the
 * conversation's transcript; this page has it because it is rendering it, while a
 * *list* would pay a read per row to do the same — so a list falls back to the id
 * and says so. Claiming otherwise would be promising a feature that costs a round
 * trip per row to deliver.
 */
test("agents: an unnamed conversation is titled from its first message where that is free", async ({
  page,
}) => {
  await loadPage(page, agentChat("2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0"));
  await expect(page.getByTestId("chat-panel")).toBeVisible();

  // In the conversation list, which is where conversations are named. The card above
  // it names the *agent* — the pair being switched between — not this conversation.
  const row = page.getByTestId("chat-session-2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0");
  await expect(row).toContainText("Summarise last night's deploy");
  // A title, not the message: cut at a word boundary with an ellipsis, which is what
  // says it is a summary rather than the text itself.
  await expect(row).toContainText("…");
  // And emphatically not the id, which is what an unnamed conversation falls back to
  // when there is nothing said in it to derive from.
  await expect(row).not.toContainText("Untitled");
});

/**
 * Item 7: the "include agents created by others" toggle is gone, and the consequence
 * of removing it is handled rather than hidden.
 *
 * Always asking for `all_creators` is the easy half. The hard half is that an
 * instance is scoped to its creator on *read* — `GetAgentInstance` resolves through
 * `WHERE namespace = $1 AND id = $2 AND user_id = $3`, and the A2A gateway reads it
 * through that same call — so a conversation somebody else started is listable and
 * genuinely not openable. This is what that has to look like.
 */
test("agents: somebody else's conversation is listed, and plainly cannot be opened", async ({
  page,
}) => {
  await loadPage(page, agentPage(agents.k8s));
  await expectSettled(page);

  await test.step("1. the toggle and its alert are gone", async () => {
    await expect(page.getByTestId("instances-all-creators")).toHaveCount(0);
    await expect(page.getByTestId("instances-own-only")).toHaveCount(0);
  });

  await test.step("2. everyone's conversations are listed", async () => {
    // Four: two the caller started and two somebody else did. Without `all_creators`
    // this would be two, and a shared agent would look half idle.
    await expect(dataRows(page)).toHaveCount(4);
    await expect(page.getByTestId("conversations-table")).toContainText(
      "bob@example.com",
    );
  });

  await test.step("3. and the ones that are not the reader's carry no link", async () => {
    // The point of the whole step: no anchor, so nothing invites a click into a chat
    // that will answer NotFound. A link here would be worse than not listing the row.
    await expect(
      page.getByTestId(`conversation-link-${instances.someoneElses}`),
    ).toHaveCount(0);
    await expect(
      page.getByTestId(`conversation-unopenable-${instances.someoneElses}`),
    ).toBeVisible();
    // Still named, so it reads as a conversation rather than as a row that failed to
    // render.
    await expect(
      page.getByTestId(`conversation-unopenable-${instances.someoneElses}`),
    ).toHaveText("Search relevance spike");
  });

  await test.step("4. the page says why, once, so the missing link reads as a rule", async () => {
    const note = page.getByTestId("conversations-others-note");
    await expect(note).toBeVisible();
    await expect(note).toContainText("started by somebody else");
    // The mechanism, in the words a reader can act on: a share link is the way in.
    await expect(note).toContainText("share link");
  });

  await test.step("5. renaming and deleting are refused for the same reason", async () => {
    // Both writes resolve the instance through the creator exactly as the read does,
    // so offering them and then failing would be worse than plainly not offering.
    await expect(
      page.getByTestId(`conversation-rename-${instances.someoneElses}`),
    ).toBeDisabled();
    await expect(
      page.getByTestId(`conversation-rename-${instances.ready}`),
    ).toBeEnabled();
  });

  await test.step("6. and opening one directly says so in the same terms", async () => {
    // The claim above is only worth making if it is what the backend actually does.
    // This is the same conversation, addressed directly.
    await loadPage(page, `/agents/kagent/${instances.someoneElses}`, { scenario: "ok" });
    const missing = page.getByTestId("instance-not-found");
    await expect(missing).toBeVisible();
    await expect(missing).toContainText("not found");
  });
});

/**
 * Item 3: navigation between an agent, its template and its conversations.
 *
 * As originally written the item asked for "the agents page filtered by that
 * template", which is circular under this shape — that filter *is* what an agent's
 * page shows. So what is left is a chain of links, and this walks it in both
 * directions.
 */
test("agents: an agent links to its template, and a conversation links up to its agent", async ({
  page,
}) => {
  await test.step("1. an agent's page links to the template it is cut from", async () => {
    await loadPage(page, agentPage(agents.k8s));
    await expectSettled(page);

    // To the template itself, because a template is a real object a reader may want
    // to change — and it is the half of the pair that this build can edit.
    await expect(page.getByTestId("agent-template-link")).toHaveAttribute(
      "href",
      `/agent-templates/kagent/${agents.k8s.template}`,
    );
    // Said on the page, because it is the thing a reader gets wrong: editing the
    // template reaches every agent cut from it, not only this one.
    await expect(page.getByTestId("agent-identity-note")).toContainText(
      "every agent cut from it",
    );
  });

  await test.step("2. a conversation opens its chat", async () => {
    await page.getByTestId(`conversation-link-${instances.ready}`).click();
    await expect(page).toHaveURL(new RegExp(`/agents/kagent/${instances.ready}/chat$`));
    // Arrived somewhere a message can be typed, which is what opening a conversation
    // is for. A route that resolved but rendered no composer would pass a URL check.
    await expect(page.getByTestId("chat-input")).toBeEditable();
  });

  await test.step("3. and links back up to its agent from the rail", async () => {
    await page.getByTestId("agent-nav-agent-conversations").click();
    await expect(page).toHaveURL(new RegExp(`${agentPage(agents.k8s)}$`));
    await expectSettled(page);
    await expect(dataRows(page)).toHaveCount(4);
  });

  await test.step("4. the conversation's own record links up too", async () => {
    await loadPage(page, `/agents/kagent/${instances.ready}`, { scenario: "ok" });
    await expectSettled(page);

    await expect(page.getByTestId("instance-agent-link")).toHaveAttribute(
      "href",
      agentPage(agents.k8s),
    );
    await expect(page.getByTestId("instance-template-link")).toHaveAttribute(
      "href",
      `/agent-templates/kagent/${agents.k8s.template}`,
    );
  });
});

/**
 * Starting a conversation, which is what "New chat" on an agent does.
 *
 * The whole story in one journey, because a create verified against anything but the
 * list is a create that passes with a broken backend: make it, come back, count.
 */
test("agents: a conversation is created by its first message, not by the click", async ({
  page,
}) => {
  /*
   * The behaviour this inverts, and why.
   *
   * "New chat" used to call `CreateAgentInstance` and navigate to the result, so an
   * instance existed the moment somebody clicked — and every visit that changed its mind
   * left an empty conversation behind for good. That is measured, not feared: the live
   * cluster accumulated nine of them, all unnamed, none with a single message, and the
   * worker pool ran out twice in one afternoon because of it. An instance is not free —
   * it holds a prepared revision, and deleting the last instance referencing a revision
   * does not collect it.
   *
   * So the click opens a page, and the first message creates the conversation.
   */
  let before = 0;

  await test.step("1. the list is read first, so the count means something", async () => {
    await loadPage(page, agentPage(agents.k8s));
    await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
    await expectSettled(page);
    before = await dataRows(page).count();
    expect(before).toBeGreaterThan(0);
  });

  await test.step("2. the click opens a conversation that does not exist yet", async () => {
    await page.getByTestId("chat-new-session").click();
    // Addressed by the *agent*, because there is no instance to address it by — which
    // is the whole point. An id in this URL would mean something had been created.
    await page.waitForURL(new RegExp(`${agentNewChat(agents.k8s)}$`), { timeout: 30_000 });
    await expect(page.getByTestId("new-chat-empty")).toBeVisible();
    await expect(page.getByTestId("chat-input")).toBeEditable();
  });

  await test.step("3. leaving without sending creates nothing", async () => {
    // The assertion the old behaviour could not pass, and the reason for the change.
    // Back through the rail, which is how a reader who changed their mind leaves.
    await page.getByTestId("agent-nav-agent-conversations").click();
    await expectSettled(page);
    await expect(dataRows(page)).toHaveCount(before, { timeout: 30_000 });
  });

  await test.step("4. sending creates it, and lands in it", async () => {
    await page.getByTestId("chat-new-session").click();
    await page.waitForURL(new RegExp(`${agentNewChat(agents.k8s)}$`), { timeout: 30_000 });
    await page.getByTestId("chat-input").fill("Why is checkout crashlooping?");
    await page.getByTestId("chat-send").click();

    // Now there is an id, because now there is a conversation.
    await page.waitForURL(/\/agents\/kagent\/[0-9a-f-]{36}\/chat$/, { timeout: 30_000 });
    await expect(page.getByTestId("new-chat-error")).toHaveCount(0);
    // And the message that created it is in the transcript rather than lost in the
    // navigation — it is handed to the chat page and sent there, so the reader sees
    // their own words in the conversation they are going to keep reading.
    await expect(page.getByTestId("chat-message").first()).toContainText(
      "Why is checkout crashlooping?",
      { timeout: 30_000 },
    );
  });

  await test.step("5. and the message is not left behind for a reload to send again", async () => {
    /*
     * Reported as a defect: reloading a conversation just started sent its opening
     * message a second time — a whole extra turn, from a page the reader only asked
     * to redraw.
     *
     * The cause is that router state is not in memory. `AgentNewChatPage` hands the
     * text over in `location.state`, which the browser keeps in the session history
     * entry, so it came back with the page and the effect that sends it fired again.
     * The page clears it once the turn is under way.
     *
     * Asserted against the history entry rather than by reloading, because the
     * fixture backend keeps its writes in the page's own memory: a reload starts a
     * backend that has never heard of this conversation, so there would be no
     * transcript to count a second message in. The whole entry is searched rather
     * than a router-specific field, so this keeps holding if the router changes where
     * it files state.
     */
    const entry = await page.evaluate(() => JSON.stringify(window.history.state ?? null));
    expect(
      entry,
      "the opening message must not survive in the history entry",
    ).not.toContain("crashlooping");
  });

  await test.step("6. and the agent's list is the proof, with one more row", async () => {
    // Back through the rail rather than by reloading: the fixture backend keeps writes
    // in the page's own memory, so a full page load would start a backend that has
    // never heard of this conversation.
    await page.getByTestId("agent-nav-agent-conversations").click();
    await expectSettled(page);
    // One more, not "at least one more" — and one, not two, which is what a request id
    // minted per send rather than per draft would have produced.
    await expect(dataRows(page)).toHaveCount(before + 1, { timeout: 30_000 });
  });

  await test.step("7. an agent with no ready revision cannot start one, and says why", async () => {
    await loadPage(page, agentPage(agents.preparing));
    await expectSettled(page);

    // `CreateAgentInstance` answers FailedPrecondition for a pair with no successful
    // revision. That used to be a tooltip on a disabled button, which is a reason a
    // reader only finds by hovering the thing they were about to give up on; it is an
    // alert on the page now, carrying the controller's own words.
    const blocked = page.getByTestId("agent-cannot-start");
    await expect(blocked).toBeVisible();
    await expect(blocked).toHaveAttribute("data-blocked-reason", /golden snapshot/);
  });
});

test("agents: deleting an agent says what goes with it, and takes both halves", async ({
  page,
}) => {
  /*
   * There is nothing to delete called "an agent".
   *
   * An agent is a (template, harness) pair, and the pair is *derived* — the controller
   * materialises it from admission and retires it when the labels stop matching. So
   * deleting an agent means deleting its template, which retires the pair and stops new
   * conversations, plus the conversations already open, which are separate rows that
   * outlive it and would otherwise be left running against a retired pair with nothing
   * describing them.
   *
   * The order matters and is asserted by the outcome rather than by spying: the
   * conversations go first, because a conversation whose pair is already retired still
   * runs and would be stranded.
   */
  await loadPage(page, agentPage(agents.k8s));
  await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
  await test.step("1. the prompt counts what will be destroyed", async () => {
    await page.getByTestId(`delete-${agents.k8s.template} on ${agents.k8s.harness}`).click();
    const consequence = page.getByTestId("agent-delete-consequence");
    await expect(consequence).toBeVisible();
    // Counted, not "some": a reader deciding this needs to know whether they are
    // throwing away one conversation or thirty.
    await expect(consequence).toContainText("of your conversations will be deleted");
    // And split, because the two halves have different outcomes. An instance is scoped
    // to its creator on write as well as read, so somebody else's cannot be deleted
    // from here and keeps running — saying so is what stops "delete agent" reading as
    // a promise it cannot keep.
    await expect(consequence).toContainText("cannot be deleted from here");
    // And what happens to the agent itself, which depends on whether anything is left.
    // These fixtures include conversations started by somebody else, so the mapping
    // stays: deleting it would retire the pair and leave those running with nothing
    // describing them, which is not ours to do to tidy up an agent they did not ask to
    // delete.
    await expect(consequence).toContainText("the agent stays");
    // And why it matters beyond tidiness.
    await expect(consequence).toContainText("releases the workers");
  });

  await test.step("2. confirming removes this reader's conversations", async () => {
    // Scoped to the open popconfirm: every row carries a delete, so an unscoped match
    // answers a prompt nobody is looking at.
    await page.locator(".ant-popover:visible").getByRole("button", { name: "Delete" }).click();
    await page.waitForURL(/\/agents(\?|$)/, { timeout: 30_000 });
    await expectSettled(page);
    // The agent is still listed, because somebody else's conversations are still under
    // it. It goes when nothing is.
    await expect(rowNamed(page, agents.k8s.template)).toHaveCount(1, { timeout: 30_000 });
  });
});

test("agents: conversations can be picked and deleted together from the table too", async ({
  page,
}) => {
  /*
   * The rail offers this, so the table does too: a reader clearing out an agent does it
   * from whichever surface they are on, and one that offers it while the other does not
   * is a difference they have to learn.
   */
  await loadPage(page, agentPage(agents.k8s));
  await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });

  await test.step("1. somebody else's conversation cannot be ticked", async () => {
    // An instance is scoped to its creator on write as well as read, so a checkbox
    // beside somebody else's would be offering a delete that is refused.
    const disabled = page.locator(
      "tbody tr td.ant-table-selection-column input:disabled",
    );
    await expect(disabled.first()).toBeVisible();
  });

  await test.step("2. picking one offers the bulk action, counted", async () => {
    await page
      .locator("tbody tr td.ant-table-selection-column input:not(:disabled)")
      .first()
      .check();
    await expect(page.getByTestId("conversations-bulk-bar")).toContainText(
      "1 conversation selected",
    );
  });

  await test.step("3. and deleting says what goes with it", async () => {
    await page.getByTestId("delete-1 selected").click();
    const prompt = page.locator(".ant-popover:visible");
    await expect(prompt).toContainText("can be recovered");
    // The reason it matters here rather than only being tidy.
    await expect(prompt).toContainText("workers they hold");
  });
});
