import { test, expect } from "../../fixtures/test";
import { waitForAppReady } from "../../helpers/page";
import { mockAgentStreamError } from "../../helpers/a2a";

// Chat / session — one success journey + one failure journey (two videos). Agent
// resolve, session create, and session lookups hit the real backend; only the A2A
// chat stream is mocked — by the proxy (mocks/server.mjs), which answers /a2a with
// a canned agent reply so the suite never needs a live LLM. The proxy echoes the
// request's contextId, so the reply lines up with the session the backend created.

const AGENT = "kagent/k8s-agent"; // a seeded, deployment-ready agent
const CHAT_URL = `/agents/${AGENT}/chat`;
const USER_MESSAGE = "List the pods please";
const AGENT_REPLY = "Hello from the agent"; // the proxy's canned reply text

test("chat session: send a message and render the agent reply", async ({ page }) => {
  await test.step("opens on the empty state before any message", async () => {
    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { name: "Start a conversation" })).toBeVisible();
    await expect(page.getByTestId("chat-input")).toBeVisible();
  });

  await test.step("sends a message and renders the agent reply", async () => {
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    await expect(page.getByText(USER_MESSAGE)).toBeVisible();
    await expect(page.getByText(AGENT_REPLY)).toBeVisible();
  });
});

test("chat failures: stream error and session-not-found", async ({ page }) => {
  await test.step("surfaces an error when the stream fails", async () => {
    await mockAgentStreamError(page);

    await page.goto(CHAT_URL);
    await waitForAppReady(page);
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page.getByText(AGENT_REPLY)).toHaveCount(0);
  });

  await test.step("shows session-not-found for a missing session", async () => {
    await page.goto(`/agents/${AGENT}/chat/missing`);
    await expect(page.getByRole("heading", { name: "Session not found" })).toBeVisible();
  });
});
