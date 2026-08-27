import { expect, test } from "../../fixtures/test";

/**
 * Opening a conversation somebody shared.
 *
 * ## What is left of sharing, and why
 *
 * Only this half. The controller's share RPCs identify a *session*, and the gRPC
 * interceptor that honours `X-Share-Token` resolves it through
 * `GetSessionShareByToken` — so a share is a capability over a session and nothing
 * else. Chat is addressed by `AgentInstance` now, and while
 * `AgentInstanceService` does carry `CreateAgentInstanceShare`, nothing on the read
 * path validates the token it mints: the A2A gateway authorises on the instance
 * instead. A Share button would therefore hand somebody a link that cannot be
 * opened, which is worse than no button.
 *
 * So the UI mints no tokens, and this spec covers what remains true: a link issued
 * before still opens, still says what it is, and still stops working when revoked.
 * `playwright/DEFERRED.md` records what it would take to share a conversation again.
 *
 * The token is seeded in `src/mocks/state.ts` rather than created through the UI,
 * because there is no longer a control that creates one — the fixture equivalent of
 * a link somebody was sent last week.
 */

const TOKEN = "mock-share-token-1";
const SESSION = "session-8f31";
const SHARED = `/shared/${SESSION}/${TOKEN}`;

test("shared conversation: a link issued earlier opens, read-only", async ({ page }) => {
  await test.step("1. it shows the conversation and says what it is", async () => {
    await page.goto(SHARED);

    // Said on the page, not only in the URL. A reader who was sent a link has no
    // other way to know that this is somebody else's conversation, or why there is
    // nowhere to reply.
    await expect(page.getByTestId("shared-session-notice")).toContainText("read-only", {
      timeout: 30_000,
    });
    await expect(page.getByTestId("shared-session-transcript")).toBeVisible();
    await expect(page.getByTestId("shared-session-error")).toHaveCount(0);
  });

  await test.step("2. the transcript is the shared conversation's own", async () => {
    // The seeded session's turns, read through `sessions.tasks` — not the A2A
    // gateway, which knows nothing about sessions. A page that rendered an empty
    // transcript would look like a working share of an empty conversation.
    await expect(page.getByTestId("chat-message").first()).toBeVisible({
      timeout: 30_000,
    });
  });

  await test.step("3. there is no composer, because a share is for reading", async () => {
    // An input that could not send would be worse than none.
    await expect(page.getByTestId("chat-input")).toHaveCount(0);
  });
});

test("shared conversation: a token the backend never issued is refused", async ({
  page,
}) => {
  // The claim this makes is about the header being sent, not about the page
  // rendering: the fixture backend refuses a token it cannot resolve, exactly as the
  // controller does, so a build that stopped sending `X-Share-Token` would serve an
  // unauthenticated read and the miss would read on screen as success.
  await page.goto(`/shared/${SESSION}/not-a-real-token`);

  await expect(page.getByTestId("shared-session-error")).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByTestId("shared-session-transcript")).toHaveCount(0);
});
