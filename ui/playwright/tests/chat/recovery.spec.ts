import { test, expect } from "../../fixtures/test";
import { agentChat, instances } from "../../helpers/app";
import type { ChatHistory, SendMessageInput } from "../../../src/api/chat/types";

/**
 * A backend that outlives the tab. Replace only the ChatClient port; the real
 * page, composer, history merge and recovery hook still run. State lives in the
 * test process so reloads and two tabs see the same admission slot.
 */
test.beforeEach(async ({ context }) => {
  await context.route(/\/src\/api\/chat\/index\.ts(?:\?.*)?$/, async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, body: `${await response.text()}
import { ApiError as RecoveryApiError } from "/src/api/ApiError.ts";
setChatClientFactory(() => ({
  protocolVersion: "recovery-test",
  history: async () => window.recoveryHistory(),
  cancel: async () => {},
  send: async function* (input) {
    const response = await window.recoverySend();
    if (response.conflict) {
      yield { type: "error", error: new RecoveryApiError("resource conflict", { kind: "http", status: 409, url: "send" }) };
      return;
    }
    yield { type: "status", state: "working", taskId: "active-task" };
    await new Promise((resolve) => {
      window.addEventListener("recovery-test-disconnect", resolve, { once: true });
      input.signal?.addEventListener("abort", resolve, { once: true });
    });
  },
}));
` });
  });
});

test("chat: refresh observes active work and restores the final result", async ({ page, context }) => {
  let state = "working";
  let sends = 0;
  await context.exposeBinding("recoveryHistory", () => ({
    messages: state === "completed" ? [{ id: "final", taskId: "active-task", role: "agent", parts: [{ kind: "text", text: "Tests passed after refresh" }], createdAt: "2026-10-01T10:00:00Z" }] : [],
    turn: { taskId: "active-task", state },
  }));
  await context.exposeBinding("recoverySend", () => { sends += 1; return {}; });

  await page.goto(agentChat(instances.ready));
  await expect(page.getByTestId("chat-session-notice")).toContainText("Session busy");
  await page.reload();
  await expect(page.getByTestId("chat-session-notice")).toContainText("Session busy");
  await expect(page.getByTestId("chat-cancel")).toBeVisible();
  await expect(page.getByTestId("chat-send")).toHaveCount(0);
  await page.getByTestId("chat-input").fill("Next prompt");
  await expect(page.getByTestId("chat-input")).toHaveValue("Next prompt");
  await page.getByTestId("chat-input").press("Enter");
  await expect(page.getByTestId("chat-input")).toHaveValue("Next prompt");
  expect(sends).toBe(0);

  state = "completed";
  await expect(page.getByTestId("chat-message")).toContainText("Tests passed after refresh", { timeout: 10_000 });
  await expect(page.getByTestId("chat-send")).toBeEnabled();
  await expect(page.getByTestId("chat-input")).toHaveValue("Next prompt");
  await expect(page.getByTestId("chat-session-notice")).toHaveCount(0);
});

test("chat: two tabs preserve refused drafts and recover a disconnected stream", async ({ page, context }) => {
  let state = "idle";
  let sends = 0;
  await context.exposeBinding("recoveryHistory", () => ({
    messages: state === "completed" ? [{ id: "final", taskId: "active-task", role: "agent", parts: [{ kind: "text", text: "Finished the original work" }], createdAt: "2026-10-01T10:00:00Z" }] : [],
    turn: state === "idle" ? undefined : { taskId: "active-task", state },
  }));
  await context.exposeBinding("recoverySend", () => {
    sends += 1;
    if (state === "working") return { conflict: true };
    state = "working";
    return {};
  });
  const other = await context.newPage();
  await page.goto(agentChat(instances.ready));
  await other.goto(agentChat(instances.ready));
  // A worker-controlled second tab can load the original module from its cache.
  // Install through the same public port in that tab, then re-read its history.
  await other.evaluate(async () => {
    const chatUrl = "/src/api/chat/index.ts";
    const errorUrl = "/src/api/ApiError.ts";
    const [{ setChatClientFactory }, { ApiError }] = await Promise.all([import(chatUrl), import(errorUrl)]);
    const backend = window as unknown as {
      recoveryHistory: () => Promise<ChatHistory>;
      recoverySend: () => Promise<{ conflict?: boolean }>;
    };
    setChatClientFactory(() => ({
      protocolVersion: "recovery-test",
      history: async () => backend.recoveryHistory(),
      cancel: async () => {},
      send: async function* (input: SendMessageInput) {
        if ((await backend.recoverySend()).conflict) {
          yield { type: "error", error: new ApiError("resource conflict", { kind: "http", status: 409, url: "send" }) };
          return;
        }
        yield { type: "status", state: "working", taskId: "active-task" };
        await new Promise((resolve) => {
          window.addEventListener("recovery-test-disconnect", resolve, { once: true });
          input.signal?.addEventListener("abort", resolve, { once: true });
        });
      },
    }));
    window.dispatchEvent(new Event("visibilitychange"));
  });
  await page.getByTestId("chat-input").fill("Run tests");
  await page.getByTestId("chat-send").click();
  await expect(page.getByTestId("chat-cancel")).toBeVisible();

  await other.getByTestId("chat-input").fill("Keep this draft");
  await expect(other.getByTestId("chat-input")).toHaveValue("Keep this draft");
  await other.getByTestId("chat-input").press("Enter");
  await expect(other.getByTestId("chat-session-notice")).toContainText("Session busy");
  await expect(other.getByTestId("chat-input")).toHaveValue("Keep this draft");
  expect(sends).toBe(1);

  await page.evaluate(() => window.dispatchEvent(new Event("recovery-test-disconnect")));
  await expect(page.getByTestId("chat-session-notice")).toContainText("Session busy");
  await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
  state = "completed";
  await page.bringToFront();
  await expect(page.getByTestId("chat-send")).toBeVisible({ timeout: 10_000 });
  await other.bringToFront();
  await expect(other.getByTestId("chat-send")).toBeEnabled({ timeout: 10_000 });
  await expect(other.getByTestId("chat-input")).toHaveValue("Keep this draft");
  expect(sends).toBe(1);
});

test("chat: a raced resource conflict keeps the draft and follows the admitted turn", async ({ page, context }) => {
  let busy = false;
  let sends = 0;
  await context.exposeBinding("recoveryHistory", () => ({
    messages: [], turn: busy ? { taskId: "active-task", state: "working" } : undefined,
  }));
  await context.exposeBinding("recoverySend", () => {
    sends += 1;
    busy = true;
    return { conflict: true };
  });
  await page.goto(agentChat(instances.ready));
  await page.getByTestId("chat-input").fill("Race with the other tab");
  await page.getByTestId("chat-send").click();
  await expect(page.getByTestId("chat-session-notice")).toContainText("Session busy");
  await expect(page.getByTestId("chat-input")).toHaveValue("Race with the other tab");
  await expect(page.getByTestId("chat-turn-error")).toHaveCount(0);
  await expect(page.locator('[data-testid="chat-message"][data-role="user"]')).toHaveCount(0);
  expect(sends).toBe(1);
});
