import { chromium } from "playwright";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 900 } });
page.on("console", (m) => { if (m.type() === "error") console.log("CONSOLE", m.text()); });
await page.goto("http://localhost:8002/agents/01a08756-1d73-71e8-8acc-8a23d2282d5e/chat");
await page.locator('[data-testid="chat-message"]').first().waitFor({ timeout: 60000 });
await page.waitForTimeout(5000);
const count = () => page.locator('[data-testid="chat-message"][data-role="user"]').count();
console.log("source user messages:", await count());
const marks = page.locator('[data-testid^="chat-checkpoint-mark-"]');
console.log("checkpoints:", await marks.count());
await marks.first().locator('[data-testid^="chat-checkpoint-fork-"]').click();
await page.waitForURL((u) => !u.pathname.includes("01a08756"), { timeout: 90000 });
console.log("navigated to", page.url());
// Immediately, without waiting for a settle: what is on screen?
for (const wait of [200, 1000, 3000, 6000]) {
  await page.waitForTimeout(wait === 200 ? 200 : wait - 200);
  console.log(`  +${wait}ms user messages:`, await count());
}
console.log("fork checkpoint lines:", await page.locator('[data-testid^="chat-checkpoint-mark-"]').count());
console.log("fork checkpoint button disabled:", await page.getByTestId("chat-checkpoint").isDisabled());
await browser.close();
