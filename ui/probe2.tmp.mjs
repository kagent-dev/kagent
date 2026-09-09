import { chromium } from "playwright";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 900 } });
await page.goto("http://localhost:8002/agents/kagent/assistant/on/kagent/new");
await page.waitForTimeout(2500);
await page.getByTestId("chat-input").fill("Remember: the sky is green.");
await page.getByTestId("chat-send").click();
await page.waitForURL(/\/chat$/, { timeout: 90000 });
await page.waitForFunction(() => {
  const all = document.querySelectorAll('[data-testid="chat-message"]');
  return all.length >= 2 && all[all.length - 1].getAttribute("data-role") !== "user";
}, null, { timeout: 180000 });
await page.waitForTimeout(2500);
// Checkpoint in this same page session, so the mark comes from `savedHere`.
await page.getByTestId("chat-checkpoint").click();
await page.waitForTimeout(4000);
console.log("source lines:", await page.locator('[data-testid^="chat-checkpoint-mark-"]').count());
await page.locator('[data-testid^="chat-checkpoint-fork-"]').first().click();
await page.waitForURL((u) => !u.pathname.includes(page.url().split("/agents/")[1]?.split("/")[0] ?? "zzz"), { timeout: 90000 }).catch(() => {});
await page.waitForTimeout(6000);
console.log("fork url:", page.url());
console.log("fork lines:", await page.locator('[data-testid^="chat-checkpoint-mark-"]').count());
console.log("fork messages:", await page.locator('[data-testid="chat-message"]').count());
await browser.close();
