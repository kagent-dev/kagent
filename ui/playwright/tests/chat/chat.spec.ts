import { test, expect } from "../../fixtures/test";
import { waitForAppReady } from "../../helpers/page";
import { firstReadyAgent } from "../../helpers/resources";

// Chat / session — success journey. Agent resolve, session create, and session
// lookups hit the real backend; only the A2A chat stream is mocked — by the proxy
// (mocks/server.mjs), which answers /a2a with a canned agent reply so the suite
// never needs a live LLM. The proxy echoes the request's contextId, so the reply
// lines up with the session the backend created.
//
// The agent under test is discovered at runtime (firstReadyAgent) rather than
// hard-coded, so the suite isn't tied to a specific seeded agent — it just needs
// one deployment-ready agent to exist. Error journeys live in chat-errors.spec.ts.

const USER_MESSAGE = "List the pods please";
const AGENT_REPLY = "Hello from the agent"; // the proxy's canned reply text

test("chat: send and receive a reply", async ({ page, request }) => {
  const chatUrl = `/agents/${await firstReadyAgent(request)}/chat`;

  // region Reading — the empty state before any message
  await test.step("opens on the empty state before any message", async () => {
    await page.goto(chatUrl);
    await waitForAppReady(page);
    await expect(page.getByRole("heading", { name: "Start a conversation" })).toBeVisible();
    await expect(page.getByTestId("chat-input")).toBeVisible();
  });

  // region Sending — send a message and render the (proxy-mocked) agent reply
  await test.step("sends a message and renders the agent reply", async () => {
    const input = page.getByTestId("chat-input");
    await expect(input).toBeEnabled();

    await input.fill(USER_MESSAGE);
    await page.getByTestId("chat-send").click();

    // Scope to transcript items: the sidebar also lists the session under the message text.
    const transcript = page.locator("[data-mm-item]");
    await expect(transcript.getByText(USER_MESSAGE)).toBeVisible();
    await expect(transcript.getByText(AGENT_REPLY)).toBeVisible();
  });
});
