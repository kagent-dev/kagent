import { test, expect } from "../../fixtures/test";
import { waitForAppReady } from "../../helpers/page";
import { mockAgentStreamError } from "../../helpers/a2a";
import { firstReadyAgent } from "../../helpers/resources";

// Chat — error journeys. A broken A2A stream (aborted in the browser before it
// reaches the proxy) surfaces an error toast; a missing session id shows the
// not-found screen. The agent is discovered at runtime (firstReadyAgent).

const USER_MESSAGE = "List the pods please";
const AGENT_REPLY = "Hello from the agent";

test("chat: stream error and missing session", async ({ page, request }) => {
  const agent = await firstReadyAgent(request);

  // region Sending — a broken stream surfaces an error toast, no reply
  await test.step("surfaces an error when the stream fails", async () => {
    await mockAgentStreamError(page);

    await page.goto(`/agents/${agent}/chat`);
    await waitForAppReady(page);
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    await expect(page.locator('[data-sonner-toast][data-type="error"]')).toBeVisible();
    await expect(page.getByText(AGENT_REPLY)).toHaveCount(0);
  });

  // region Reading — a missing session id shows the not-found screen
  await test.step("shows session-not-found for a missing session", async () => {
    await page.goto(`/agents/${agent}/chat/missing`);
    await expect(page.getByRole("heading", { name: "Session not found" })).toBeVisible();
  });
});
