import { expect, test } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";

/**
 * Sharing a conversation: create a link, see it listed, revoke it, open one.
 *
 * A share is over an `AgentInstance`, because the instance *is* the conversation.
 * The gRPC interceptor validates the share token, and the A2A gateway reads the
 * instance as the share's owner; reading it as the visitor would find nothing.
 *
 * ## What a fixture can and cannot prove here
 *
 * It can prove the page spends a token and reports a refusal. It **cannot** prove
 * the header reaches a backend: chat in mock mode is served by a client-side fake
 * that builds no request, so it reads the registration directly rather than seeing
 * what travelled. That gap is real and recorded in `playwright/DEFERRED.md`; only
 * the live suite can close it.
 */

const CONVERSATION = agentChat(instances.ready);
/** A link issued before this tab opened — see `SEEDED_INSTANCE_SHARE` in the mock. */
const SEEDED_LINK = `/shared/agent/kagent/${instances.ready}/mock-instance-token-seed`;

test("sharing: a link is created, shown once, listed and revoked", async ({ page }) => {
  await page.goto(CONVERSATION);

  await test.step("1. sharing is offered on a conversation", async () => {
    await expect(page.getByTestId("chat-share")).toBeVisible({ timeout: 30_000 });
    await page.getByTestId("chat-share").click();
    await expect(page.getByTestId("share-dialog")).toBeVisible();
    await expect(page.getByTestId("share-create")).toBeVisible();
  });

  await test.step("2. read-only is the default, because giving away access should be deliberate", async () => {
    // `READ_WRITE` lets a visitor send *as the owner*, so it is an opt-in.
    await expect(page.getByTestId("share-allow-writes")).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  await test.step("3. a created link is shown once, and says so", async () => {
    await page.getByTestId("share-create").click();

    const fresh = page.getByTestId("share-fresh-link");
    await expect(fresh).toBeVisible({ timeout: 15_000 });
    // The controller stores only a digest, so this is the one moment the token
    // exists to be shown — and the page has to say that rather than imply it can be
    // fetched again.
    await expect(fresh).toContainText("cannot be shown again");
    // A whole link, not a bare token: that is the form a person actually sends.
    await expect(fresh).toContainText("/shared/agent/kagent/");
  });

  await test.step("4. the list shows the share, and never the token", async () => {
    const list = page.getByTestId("share-list");
    // Two: the one just created and the one seeded as "issued before this tab
    // opened". Waited for rather than counted immediately — the list reloads after a
    // create, and counting mid-reload reads a number that is about to change.
    await expect(list.locator("tbody tr")).toHaveCount(2, { timeout: 15_000 });
    await expect(list).toContainText("Read only");
    await expect(
      list,
      "the list cannot show a token — only its digest is stored",
    ).not.toContainText("mock-instance-token");
  });

  await test.step("5. revoking removes it", async () => {
    await page.locator('[data-testid^="revoke-share-"]').first().click();
    await expect(page.getByTestId("share-list").locator("tbody tr")).toHaveCount(1, {
      timeout: 15_000,
    });
    await expect(page.getByTestId("share-error")).toHaveCount(0);
    // The link on screen may be the one just revoked, and a copy button for a dead
    // link is worse than none.
    await expect(page.getByTestId("share-fresh-link")).toHaveCount(0);
  });
});

test("sharing: a link issued earlier opens the conversation, read-only", async ({
  page,
}) => {
  await test.step("1. it opens and says what it is", async () => {
    await page.goto(SEEDED_LINK);

    // Said on the page, not only in the URL: a reader who was sent a link has no
    // other way to know this is somebody else's conversation.
    await expect(page.getByTestId("shared-agent-notice")).toContainText("read-only", {
      timeout: 30_000,
    });
    await expect(page.getByTestId("shared-agent-error")).toHaveCount(0);
  });

  await test.step("2. the conversation is there", async () => {
    await expect(page.getByTestId("shared-agent-transcript")).toBeVisible();
    await expect(page.getByTestId("chat-message").first()).toBeVisible({
      timeout: 30_000,
    });
  });

  await test.step("3. a read-only link offers no composer", async () => {
    // An input that could not send is worse than none: the send would be refused by
    // the controller and the reader would have typed for nothing.
    await expect(page.getByTestId("chat-input")).toHaveCount(0);
  });
});

test("sharing: a token the backend never issued is refused", async ({ page }) => {
  // The assertion that makes the one above mean something: the fixture refuses a
  // token it cannot resolve, exactly as the controller does. Without it, a build
  // that mangled the token would serve the conversation anyway and the miss would
  // read on screen as success.
  await page.goto(`/shared/agent/kagent/${instances.ready}/not-a-real-token`);

  await expect(page.getByTestId("shared-agent-error")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("shared-agent-transcript")).toHaveCount(0);
});

test("sharing: a link that allows replies offers a way to reply", async ({ page }) => {
  /*
   * The permission was grantable and had no effect.
   *
   * An owner could tick "Also allow replies", hand the link over, and the person
   * opening it had no composer — a permission granted and silently ignored. The page
   * withheld one deliberately, on the reasoning that a composer working for some links
   * and not others fails invisibly; the cost of that reasoning was worse than the
   * problem it avoided.
   *
   * The link carries `?reply`, which is a hint about what to draw and never a
   * permission: the controller resolves the token and refuses a send the share does
   * not allow, so a hand-edited address gets a refusal in the controller's own words.
   */
  await page.goto(`${SEEDED_LINK}?reply`);

  await expect(page.getByTestId("shared-agent-notice")).toBeVisible({ timeout: 30_000 });
  // And it says what replying means here, because it is not obvious: a share answers
  // as its owner, so anything sent is recorded as theirs.
  await expect(page.getByTestId("shared-agent-notice")).toContainText("owner");
  await expect(page.getByTestId("chat-input")).toBeVisible();

});
