import { test, expect } from "../../fixtures/test";
import { waitForAppReady, expectToast } from "../../helpers/page";
import { mockAgentReply, mockAgentStreamError } from "../../helpers/a2a";

// Sub-stage 2.2 — the chat/session flow. Data calls (agent resolve, sessions,
// createSession) are server-side (stub); the A2A stream is a browser fetch
// mocked via page.route. A brand-new chat is the cleanest path (no session GETs
// on load); createSession fires on send and the stub returns a session with id
// "e2e-session", which the mocked SSE frames use as their contextId.

const CHAT_URL = "/agents/default/e2e-agent/chat";
const USER_MESSAGE = "List the pods please";
const AGENT_REPLY = "Hello from the agent";
const TOOL_NAME = "get_pods";

test.describe("chat / session", () => {
  test("sends a message and renders the agent reply + tool call", async ({ page }) => {
    await mockAgentReply(page, {
      text: AGENT_REPLY,
      tool: { name: TOOL_NAME, args: { ns: "default" }, result: "pod-a Running" },
    });

    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    // User's own message, the agent's streamed reply, and the tool-call block.
    await expect(page.getByText(USER_MESSAGE)).toBeVisible();
    await expect(page.getByText(AGENT_REPLY)).toBeVisible();
    await expect(page.getByTestId("tool-call")).toContainText(TOOL_NAME);
  });

  test("shows the empty state before any message", async ({ page }) => {
    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { name: "Start a conversation" })).toBeVisible();
    await expect(page.getByTestId("chat-input")).toBeVisible();
  });

  test("surfaces an error when the stream fails", async ({ page }) => {
    await mockAgentStreamError(page);

    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    // No agent reply rendered.
    await expect(page.getByText(AGENT_REPLY)).toHaveCount(0);
  });

  test("loads an existing session and renders prior messages", async ({ page }) => {
    await page.goto("/agents/default/e2e-agent/chat/e2e-session");
    await waitForAppReady(page);
    await expect(page.getByText("Prior question")).toBeVisible();
    await expect(page.getByText("Prior answer")).toBeVisible();
  });

  test("shows session-not-found for a missing session", async ({ page }) => {
    await page.goto("/agents/default/e2e-agent/chat/missing");
    await expect(page.getByRole("heading", { name: "Session not found" })).toBeVisible();
  });

  test("submits feedback on an agent reply", async ({ page, mock }) => {
    await mockAgentReply(page, { text: AGENT_REPLY });
    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();
    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();
    await expect(page.getByText(AGENT_REPLY)).toBeVisible();

    await page.getByRole("button", { name: "Thumbs up" }).click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("textbox").fill("Great answer");
    await dialog.getByRole("button", { name: /submit/i }).click();

    await expectToast(page, /thank you/i, { type: "success" });
    expect(await mock.lastRequest("POST", "/api/feedback"), "expected POST /api/feedback").not.toBeNull();
  });

});
