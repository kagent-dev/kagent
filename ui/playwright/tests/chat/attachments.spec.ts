import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { agentChat, agentNewChat, agents, instances, loadPage } from "../../helpers/app";
import { beginTurn, expectTurnFinished } from "../../helpers/chat";

/** Files sent with a message: staged, removed, sent, and read back after a reload. */

const file = (name: string, mimeType: string, text = "hello") => ({
  name,
  mimeType,
  buffer: Buffer.from(text),
});

/** Fires one drag event on the page header and reports whether the page claimed it. */
const drag = (page: Page, type: string, files: string[], text?: string) =>
  page.evaluate(
    ({ type, files, text }) => {
      const data = new DataTransfer();
      for (const name of files) data.items.add(new File(["hello"], name, { type: "text/plain" }));
      if (text) data.setData("text/plain", text);
      const event = new DragEvent(type, { dataTransfer: data, bubbles: true, cancelable: true });
      document.querySelector("header")!.dispatchEvent(event);
      return event.defaultPrevented;
    },
    { type, files, text },
  );

test("chat: files are staged, removed, sent, and kept across a reload", async ({ page }) => {
  const staged = page.getByTestId("chat-staged-files").getByTestId("attachment-chip");
  const fromReader = page.locator('[data-testid="chat-message"][data-role="user"]');
  const picker = page.getByTestId("chat-file-input");

  await test.step("1. three files are staged and one is removed", async () => {
    await loadPage(page, agentChat(instances.ready));
    await expect(page.getByTestId("chat-input")).toBeEnabled({ timeout: 30_000 });
    await picker.setInputFiles([
      file("a.txt", "text/plain"),
      file("b.csv", "text/csv"),
      file("c.json", "application/json"),
    ]);
    await expect(staged).toHaveCount(3);

    await page.getByRole("button", { name: "Remove b.csv" }).click();
    await expect(staged.getByTestId("attachment-name")).toHaveText(["a.txt", "c.json"]);
  });

  await test.step("2. the message carries exactly the files left", async () => {
    const turn = await beginTurn(page);
    await page.getByTestId("chat-input").fill("Read these");
    await page.getByTestId("chat-send").click();
    await expectTurnFinished(page, turn);

    await expect(
      fromReader.filter({ hasText: "Read these" }).getByTestId("attachment-name"),
    ).toHaveText(["a.txt", "c.json"]);
    await expect(staged).toHaveCount(0);
    await expect(page.getByText("You attached: a.txt, c.json.")).toBeVisible();
    await expect(page.getByRole("link", { name: "Download summary.txt" })).toHaveAttribute(
      "download",
      "summary.txt",
    );
  });

  await test.step("3. an unsupported file is refused in place", async () => {
    await picker.setInputFiles([file("setup.exe", "application/x-msdownload")]);
    await expect(page.getByTestId("chat-file-error")).toHaveText(
      "setup.exe is not a supported file type.",
    );
    await expect(staged).toHaveCount(0);
    await expect(page.getByTestId("chat-send")).toBeDisabled();
  });

  await test.step("4. files alone can be sent, typed by extension", async () => {
    // An empty type is what browsers report for .md; the extension decides it.
    await picker.setInputFiles([file("notes.md", "")]);
    await expect(page.getByTestId("chat-file-error")).toHaveCount(0);

    const turn = await beginTurn(page);
    await page.getByTestId("chat-send").click();
    await expectTurnFinished(page, turn);
    await expect(fromReader.last().getByTestId("attachment-name")).toHaveText(["notes.md"]);
    await expect(page.getByText("You attached: notes.md.")).toBeVisible();
  });

  await test.step("5. a reload shows the same files", async () => {
    await page.reload();
    await expect(
      fromReader.filter({ hasText: "Read these" }).getByTestId("attachment-name"),
    ).toHaveText(["a.txt", "c.json"]);
    await expect(fromReader.last().getByTestId("attachment-name")).toHaveText(["notes.md"]);
  });
});

test("chat: a new conversation can start with files only", async ({ page }) => {
  await loadPage(page, agentNewChat(agents.k8s));
  await page.getByTestId("chat-file-input").setInputFiles([file("plan.yaml", "")]);
  await page.getByTestId("chat-send").click();

  // Handed to the conversation's page, which sends them and titles it by the file.
  await expect(
    page
      .locator('[data-testid="chat-message"][data-role="user"]')
      .getByTestId("attachment-name"),
  ).toHaveText(["plan.yaml"]);
  await expect(page.getByText("You attached: plan.yaml.")).toBeVisible();
  await expect(page.getByTestId("chat-sessions-list")).toContainText("plan.yaml");
});

test("chat: files dropped anywhere on the page are staged", async ({ page }) => {
  const overlay = page.getByTestId("chat-drop-overlay");
  await loadPage(page, agentChat(instances.ready));
  await expect(page.getByRole("button", { name: "Attach files" })).toBeVisible({ timeout: 30_000 });

  await test.step("1. a text drag is left alone", async () => {
    expect(await drag(page, "dragenter", [], "a link")).toBe(false);
    expect(await drag(page, "dragover", [], "a link")).toBe(false);
    await expect(overlay).toHaveCount(0);
  });

  await test.step("2. a file drag shows the overlay until it leaves", async () => {
    await drag(page, "dragenter", ["a.txt"]);
    await expect(overlay).toHaveText("Drop files to attach");
    await drag(page, "dragleave", ["a.txt"]);
    await expect(overlay).toHaveCount(0);
  });

  await test.step("3. a drop on the header stages the files", async () => {
    await drag(page, "dragenter", ["a.txt", "b.txt"]);
    expect(await drag(page, "drop", ["a.txt", "b.txt"])).toBe(true);
    await expect(overlay).toHaveCount(0);
    await expect(
      page.getByTestId("chat-staged-files").getByTestId("attachment-name"),
    ).toHaveText(["a.txt", "b.txt"]);
  });
});

test("chat: a harness that cannot read files offers no attach control", async ({ page }) => {
  // fast-lane runs Codex, which takes one text part only.
  await loadPage(page, agentNewChat(agents.sharedOnFastLane));
  await expect(page.getByTestId("chat-composer")).toHaveAttribute("data-can-attach", "false");
  await expect(page.getByRole("button", { name: "Attach files" })).toHaveCount(0);

  // Still claimed, so the browser does not open the file, but nothing is staged.
  await drag(page, "dragenter", ["a.txt"]);
  expect(await drag(page, "drop", ["a.txt"])).toBe(true);
  await expect(page.getByTestId("chat-drop-overlay")).toHaveCount(0);
  await expect(page.getByTestId("chat-staged-files")).toHaveCount(0);
});
