import { test, expect } from "../../fixtures/test";
import {
  agentNewChat,
  agentPage,
  agents,
  dataRows,
  expectSettled,
  instances,
  loadPage,
  pageTitle,
  rowNamed,
} from "../../helpers/app";
import { tick } from "../../helpers/controls";
import { confirmation, pressOnce, pressUntil } from "../../helpers/resource";

/**
 * An agent's own page — the surface between the agents list and a chat.
 *
 * Named for the page rather than for conversations, because a conversation shows up
 * on three surfaces and only one of them is here: this page's table, the rail on the
 * chat page, and the transcript in the middle of it. The folder is the surface, so
 * "which spec owns this" has an answer — see `README.md`. **This file owns renaming
 * and deleting a conversation**; the rail keeps one test for doing either without
 * leaving the conversation, which is the only thing that table cannot say.
 *
 * Three things about the page are worth pinning, and each is a claim a screenshot
 * cannot check:
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
  await test.step("1. the agent's page opens", async () => {
    // Opened directly: `agent-chat-entry.spec.ts` and the template-link test below own
    // the clicks from the list and the rail.
    await loadPage(page, agentPage(agents.sharedOnK8s));
    await expectSettled(page);
  });

  await test.step("2. the rail and the header name the agent, and the structure says what runs it", async () => {
    await expect(page.getByTestId("agent-rail-identity")).toContainText("shared-brain");
    await expect(page.getByTestId("agent-name")).toHaveText("shared-brain");
    await expect(page.getByTestId("structure-harness")).toContainText("kagent/k8s-agent");
  });

  await test.step("3. it lists this pair's conversation and not its twin's", async () => {
    // The assertion the server-side filter exists for. `shared-brain` is two agents;
    // each has exactly one conversation, and they are indistinguishable on
    // everything but the harness. Narrowed on the template alone, both would appear
    // here and the page would look perfectly reasonable.
    await expect(dataRows(page)).toHaveCount(1);
    await expect(rowNamed(page, "Drafting the runbook")).toHaveCount(1);
    await expect(page.getByTestId("conversation-link-2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0")).toHaveCount(0);
  });

  await test.step("4. the other agent cut from the same template has the other one", async () => {
    await loadPage(page, agentPage(agents.sharedOnFastLane));
    await expectSettled(page);

    await expect(dataRows(page)).toHaveCount(1);
    // By id, not text: the row's title becomes its first message once that is read.
    await expect(page.getByTestId("conversation-link-2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0")).toHaveCount(1);
    await expect(page.getByTestId("conversations-table")).not.toContainText(
      "Drafting the runbook",
    );
  });

  await test.step("5. its twin, with identical refs, lists none of them in its rail", async () => {
    // Same template and harness as `shared-brain`, so only the agent's own name can tell them apart.
    await loadPage(page, agentNewChat({ name: "shared-brain-twin", template: "", harness: "" }));
    await expect(page.getByTestId("agent-rail-identity")).toContainText("shared-brain-twin");
    await expect(page.getByTestId("chat-sessions-empty")).toBeVisible();
    await expect(page.getByTestId("chat-session-1d4f7a92-0c38-4e61-b25a-7f930e6c8b14")).toHaveCount(0);
  });
});

test("agents: the conversations table lists newest first, as the rail does", async ({
  page,
}) => {
  await loadPage(page, agentPage(agents.k8s));
  await expectSettled(page);

  // The fixtures list these oldest-ish first, so only a sort puts them in this order.
  const newestFirst = [
    "8e5f2b09-6c14-4a7d-83b0-9d1c7e40f5a6",
    "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
    "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05",
    "3c9a1e64-8d47-4f22-b71a-05e2d8c96b18",
  ];
  const keys = await dataRows(page).evaluateAll((rows) =>
    rows.map((row) => row.getAttribute("data-row-key")),
  );
  expect(keys).toEqual(newestFirst);
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
    const untitled = page.getByTestId(`conversation-link-${instances.idle}`);
    // Not the UUID. A database key presented under a "Conversation" heading reads as
    // a name somebody chose, and eight rows of it are indistinguishable at a glance
    // — which is the specific failure that started this rework.
    await expect(untitled).not.toHaveText(instances.idle);
    await expect(untitled).toContainText("Untitled");
    // The short id is still there: two untitled conversations with one agent have
    // nothing else to tell them apart.
    await expect(untitled).toContainText(instances.idle.slice(0, 8));
  });

  await test.step("3. renaming one changes what the list shows", async () => {
    /*
     * Both clicks pressed until they take: the rename dialog animates in, and a click
     * aimed at it mid-transition is dropped. That reports as "the list never showed the
     * new name", which points at the save rather than at the press that never landed.
     */
    const rename = page.getByTestId("conversation-rename-input");
    await pressUntil(page.getByTestId(`conversation-rename-${instances.idle}`), () =>
      expect(rename).toBeVisible(),
    );
    // The box opens *empty* for an unnamed conversation rather than pre-filled with
    // the placeholder, or clearing a title would be impossible: saving would turn an
    // honest "Untitled" into a literal one.
    const field = rename.locator("input");
    await expect(field).toHaveValue("");

    await field.fill("Rollback rehearsal");
    // The list is the proof, not the toast: a success message says the app thinks it
    // worked, and a rename that failed would still show one on a broken backend.
    await pressUntil(page.getByRole("button", { name: "Save" }), () =>
      expect(rowNamed(page, "Rollback rehearsal")).toHaveCount(1),
    );
    await expect(
      page.getByTestId(`conversation-link-${instances.idle}`),
    ).toHaveText("Rollback rehearsal");
  });

  await test.step("4. a name the controller would refuse is refused before the round trip", async () => {
    await page.getByTestId(`conversation-rename-${instances.idle}`).click();
    const field = page.getByTestId("conversation-rename-input").locator("input");
    await field.fill(" leading space");

    // Refused rather than trimmed, which is what the controller does — and the
    // reason matters: silently rewriting what somebody typed reads on screen as a
    // rename that did not take.
    await expect(page.getByTestId("conversation-rename-problem")).toContainText(
      "cannot start or end with a space",
    );
    await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
    // Same arriving-box case as the Save below: the rename modal animates in.
    await pressOnce(page.getByRole("button", { name: "Cancel" }));
  });

  await test.step("5. clearing a name puts it back to being untitled", async () => {
    await page.getByTestId(`conversation-rename-${instances.idle}`).click();
    const field = page.getByTestId("conversation-rename-input").locator("input");
    await expect(field).toHaveValue("Rollback rehearsal");
    await field.fill("");
    // Once the box has stopped arriving, for the reason the delete step below gives.
    await pressOnce(page.getByRole("button", { name: "Save" }));

    await expect(
      page.getByTestId(`conversation-link-${instances.idle}`),
    ).toContainText("Untitled");
  });
});

test("agents: an unnamed conversation is named by its first message, as the rail names it", async ({
  page,
}) => {
  // Unnamed, with a seeded first message; `instances.idle` has none, so it stays "Untitled".
  const autoTitled = "2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0";
  const title = "Summarise last night's deploy for the incident channel, and…";
  await loadPage(page, agentPage(agents.sharedOnFastLane));
  await expectSettled(page);

  await test.step("1. the table names it by its first message", async () => {
    await expect(page.getByTestId(`conversation-link-${autoTitled}`)).toHaveText(title);
  });

  await test.step("2. and the rail shows the same title", async () => {
    await expect(page.getByTestId(`chat-session-${autoTitled}`)).toContainText(title);
  });
});

/**
 * Item 7: the "include agents created by others" toggle is gone, and the consequence
 * of removing it is handled rather than hidden.
 *
 * Always asking for `all_creators` is the easy half. The hard half is that an
 * instance is scoped to its creator on *read* — `GetAgentInstance` resolves through
 * `WHERE id = $1 AND user_id = $2`, and the A2A gateway reads it
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
    await loadPage(page, `/agents/${instances.someoneElses}`, { scenario: "ok" });
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
    // Each node says what it is on the node itself, not behind an info icon.
    await expect(page.getByTestId("structure-template")).toContainText(
      "Agents built from it share these settings",
    );
  });

  await test.step("2. a conversation opens its chat", async () => {
    await page.getByTestId(`conversation-link-${instances.ready}`).click();
    await expect(page).toHaveURL(new RegExp(`/agents/${instances.ready}/chat$`));
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
    await loadPage(page, `/agents/${instances.ready}`, { scenario: "ok" });
    await expectSettled(page);

    await expect(page.getByTestId("instance-agent-link")).toHaveAttribute(
      "href",
      agentPage(agents.k8s),
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
    await page.waitForURL(/\/agents\/[0-9a-f-]{36}\/chat$/, { timeout: 30_000 });
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
     * The opening message is handed over in memory, so nothing in the history entry can
     * send it again on a reload, Back or forward. Asserted against the entry rather than
     * by reloading, because a reload starts a fixture backend that has never heard of this
     * conversation.
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

test("agents: a failed edit says why on the agent's page, not only in a tooltip", async ({ page }) => {
  /*
   * A typo in a Harness image used to surface only as "Update failed" with the
   * controller's message on hover. Nothing is blocked, because conversations keep the
   * last good revision, so `agent-cannot-start` never appears; this alert is the
   * only place on the page that says what went wrong.
   */
  await loadPage(page, agentPage(agents.updateFailed));
  await expectSettled(page);

  const failed = page.getByTestId("agent-update-failed");
  await expect(failed).toBeVisible();
  await expect(failed).toContainText("could not pull image ghcr.io/kagent-dev/kagent/claude-harnes:0.9.0");
  await expect(failed).toContainText("last good revision");
  await expect(page.getByTestId("agent-cannot-start")).toHaveCount(0);
  await expect(page.getByTestId("agent-revision-state")).toHaveAttribute("data-revision-state", "updateFailed");
});

test("agents: a ready agent shows no failure alert", async ({ page }) => {
  await loadPage(page, agentPage(agents.k8s));
  await expectSettled(page);
  await expect(page.getByTestId("agent-revision-state")).toHaveAttribute("data-revision-state", "ready");
  await expect(page.getByTestId("agent-update-failed")).toHaveCount(0);
});

test("agents: deleting an Agent preserves shared resources and conversations", async ({ page }) => {
  await loadPage(page, agentPage(agents.k8s));
  await expect(dataRows(page).first()).toBeVisible({ timeout: 30_000 });
  await page.getByTestId(`delete-${agents.k8s.name}`).click();
  await expect(confirmation(page)).toContainText("Existing conversations keep their prepared revisions");
  await pressOnce(confirmation(page).getByRole("button", { name: "Delete" }));
  await page.waitForURL(/\/agents(\?|$)/, {timeout: 30_000});
  await expect(rowNamed(page, agents.k8s.name)).toHaveCount(0);
  await loadPage(page, `/agent-templates/kagent/${agents.k8s.template}`);
  await expect(page.getByTestId("template-edit")).toBeVisible();
  await loadPage(page, `/agents/${instances.ready}/chat`);
  await expect(page.getByTestId("chat-input")).toBeVisible();
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
    await tick(
      page.locator("tbody tr td.ant-table-selection-column input:not(:disabled)").first(),
    );
    await expect(page.getByTestId("conversations-bulk-bar")).toContainText(
      "1 conversation selected",
    );
  });

  await test.step("3. and deleting says what goes with it", async () => {
    const prompt = confirmation(page);
    // Clicked once, not pressed until: this is the popconfirm's own trigger, and a
    // retry closes what the first press opened.
    await page.getByTestId("delete-1 selected").click();
    await expect(prompt).toBeVisible();
    await expect(prompt).toContainText("can be recovered");
    // The reason it matters here rather than only being tidy.
    await expect(prompt).toContainText("workers they hold");
  });
});

/**
 * The schedules that run this agent.
 *
 * A `ScheduledRun` names a harness and an agent template, which is the pair this page
 * is — so the section belongs here rather than on a conversation or on the template.
 * Two things are worth pinning, and neither is visible in a screenshot:
 *
 * - **The narrowing is the browser's, and it narrows on both halves of the pair.**
 *   `ListScheduledRuns` takes only a page, so the filter is client-side; keyed on the
 *   template alone it would show one agent's schedules under its twin.
 * - **A failed read is not an empty list.** The section must not offer "nothing runs
 *   this agent yet" when the truth is that it could not find out.
 */
test("agents: an agent lists the schedules that run it, and offers one when it has none", async ({
  page,
}) => {
  await test.step("1. the schedules targeting this agent are listed", async () => {
    // The tab is in the address, so it opens straight onto the schedules.
    await loadPage(page, `${agentPage(agents.k8s)}?tab=schedules`);
    await expect(page.getByRole("tab", { name: "Schedules (3)" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByTestId("agent-schedules")).toBeVisible();
    await expectSettled(page);

    const list = page.getByTestId("agent-schedules-list");
    await expect(list.locator("li")).toHaveCount(3);
    // Named and dated, not a bare UUID: a row saying only "Daily cluster report" does
    // not say when, and the id says nothing at all.
    await expect(list).toContainText("Daily cluster report");
    await expect(list).toContainText("Every day at 09:00");
  });

  await test.step("2. each row leads to that schedule's page", async () => {
    const first = page
      .getByTestId("agent-schedules-list")
      .locator('[data-testid^="agent-schedule-link-"]')
      .first();
    const id = (await first.getAttribute("data-testid"))?.replace(
      "agent-schedule-link-",
      "",
    );
    await expect(first).toHaveAttribute("href", `/schedules/${id ?? ""}`);

    await first.click();
    await expect(page).toHaveURL(new RegExp(`/schedules/${id ?? ""}$`));
    // The schedule's own page, so the link is navigation rather than a URL that
    // happens to parse.
    await expect(pageTitle(page)).toHaveText("Daily cluster report");
  });

  await test.step("2b. so does the rest of the row, without the link firing twice", async () => {
    await loadPage(page, agentPage(agents.k8s));
    await page.getByRole("tab", { name: /Schedules/ }).click();
    const row = page
      .getByTestId("agent-schedules-list")
      .locator('[data-testid^="agent-schedule-"]')
      .filter({ hasText: "Daily cluster report" })
      .first();
    // The cadence text, which is as far from the link as the row gets.
    await row.getByText("Every day at 09:00").click();
    await expect(page).toHaveURL(/\/schedules\/[0-9a-f-]+$/);
    await expect(pageTitle(page)).toHaveText("Daily cluster report");
  });

  await test.step("3. an agent nothing schedules offers to create one", async () => {
    // The same template's twin on another harness has none, which is also the
    // assertion the client-side filter exists for: keyed on the template alone, the
    // three above would appear here too.
    await loadPage(page, `${agentPage(agents.sharedOnK8s)}?tab=schedules`);
    await expect(page.getByTestId("agent-schedules-empty")).toBeVisible();
    await expect(page.getByTestId("agent-schedules-list")).toHaveCount(0);

    const create = page.getByTestId("agent-schedules-create");
    await expect(create).toHaveAttribute("href", "/schedules/new");
    await expect(create).toContainText("Create a schedule");
  });

  await test.step("4. a failed read says so, and does not claim there are none", async () => {
    await loadPage(page, `${agentPage(agents.k8s)}?tab=schedules`, { scenario: "error" });

    const alert = page.getByTestId("agent-schedules-error");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("Could not load this agent's schedules");
    // The backend's own account of it, so the reader knows which call broke.
    await expect(alert).toContainText("asked to fail");
    // The specific bug this step exists for: a failure reported as an empty state.
    await expect(page.getByTestId("agent-schedules-empty")).toHaveCount(0);
    await expect(page.getByTestId("agent-schedules-create")).toHaveCount(0);
  });
});

/**
 * What the agent is made of, shown as a tree above its conversations.
 *
 * `shared-brain` exercises every branch: two bindings on one server (one asks first),
 * and a chain of sub-agents deeper than the page expands in place.
 */
test("agents: the page shows the agent's structure, and sub-agents expand in place", async ({ page }) => {
  await loadPage(page, agentPage(agents.sharedOnK8s));
  await expectSettled(page);
  const structure = page.getByTestId("agent-structure");

  await test.step("1. the harness and the shared template head the tree", async () => {
    await expect(structure.getByTestId("structure-harness")).toContainText("kagent runtime");
    await expect(page.getByTestId("agent-template-link")).toHaveText("kagent/shared-brain");
    // Three agents reference the template: this one, its fast-lane twin and its namesake twin.
    await expect(page.getByTestId("agent-template-shared")).toHaveText("Shared by 3 agents");
    await expect(page.getByTestId("agent-revision")).toHaveText("rev-shared-b…");
  });

  await test.step("2. tools open into one group per binding, and say which ask first", async () => {
    const tools = structure.getByTestId("structure-tools").first();
    await expect(tools).toContainText("1 MCP server, 3 tools");
    await expect(tools.getByTestId("structure-tool-group")).toHaveCount(0);

    await tools.getByTestId("structure-tools-toggle").click();
    await expect(tools.getByTestId("structure-tools-toggle")).toHaveAttribute("aria-expanded", "true");
    await expect(tools.getByTestId("structure-tool-group")).toHaveCount(2);
    await expect(tools.getByTestId("structure-tool-group").nth(0)).toContainText("k8s_get_events");
    await expect(tools.getByTestId("structure-tool-group").nth(1)).toContainText("Asks before each call");
  });

  await test.step("3. a sub-agent expands to its own template's parts", async () => {
    const noteTaker = structure.getByTestId("structure-subagent-note-taker");
    await noteTaker.getByTestId("structure-subagent-note-taker-toggle").click();
    await expect(noteTaker.getByTestId("structure-model").first()).toContainText("kagent/default-model-config");

    // Only the top list of sub-agents starts open. One more level opens in place, and its
    // prompt lives in a ConfigMap, so that is named instead.
    await noteTaker.getByTestId("structure-subagents-toggle").first().click();
    const triage = noteTaker.getByTestId("structure-subagent-triage");
    await triage.getByTestId("structure-subagent-triage-toggle").click();
    await expect(triage.getByTestId("structure-instructions").first()).toContainText("support-prompts");
  });

  await test.step("4. past two levels a sub-agent is a link to its template", async () => {
    await structure.getByTestId("structure-subagent-triage").getByTestId("structure-subagents-toggle").first().click();
    const deepest = structure.getByTestId("structure-subagent-link-cluster-checker");
    await expect(deepest).toBeVisible();
    await expect(deepest.getByRole("link")).toHaveAttribute("href", `/agent-templates/kagent/${agents.k8s.template}`);
    await expect(structure.getByTestId("structure-subagent-cluster-checker")).toHaveCount(0);
  });

  await test.step("5. conversations and schedules sit in tabs below, with counts", async () => {
    await expect(page.getByRole("tab", { name: "Conversations (1)" })).toHaveAttribute("aria-selected", "true");
    await page.getByRole("tab", { name: "Schedules (0)" }).click();
    await expect(page.getByTestId("agent-schedules-empty")).toBeVisible();
    await expect(page).toHaveURL(/[?&]tab=schedules/);
  });
});

test("agents: the header labels its fields, and the revision's tooltip is on its text only", async ({ page }) => {
  await loadPage(page, agentPage(agents.sharedOnK8s));
  await expectSettled(page);
  const header = page.getByTestId("agent-header");
  const revision = page.getByTestId("agent-revision");
  const fullRevision = page.getByRole("tooltip").filter({ hasText: "rev-shared-brain" });

  await test.step("1. status, namespace and revision are labelled", async () => {
    for (const label of ["Status", "Namespace", "Revision"]) {
      await expect(header.getByText(label, { exact: true })).toBeVisible();
    }
  });

  await test.step("2. hovering the shortened revision shows it in full", async () => {
    await revision.getByText("rev-shared-b…").hover();
    await expect(fullRevision).toBeVisible();
  });

  await test.step("3. hovering the copy button shows only its own tooltip", async () => {
    await revision.getByRole("button").hover();
    // The copy tooltip appearing first means the hover landed before the absence is checked.
    await expect(page.getByRole("tooltip").filter({ hasText: "Copy" })).toBeVisible();
    await expect(fullRevision).toBeHidden();
  });
});

test("agents: an inline template is shown as part of the agent", async ({ page }) => {
  await loadPage(page, agentPage({ name: "release-notes", template: "", harness: "" }));
  await expectSettled(page);
  await expect(page.getByTestId("agent-template-inline")).toHaveText("Inline");
  await expect(page.getByTestId("agent-template-link")).toHaveCount(0);
  await expect(page.getByTestId("structure-tools")).toContainText("This agent answers from the model alone.");
});
