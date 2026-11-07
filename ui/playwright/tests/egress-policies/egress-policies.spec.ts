import type { Page } from "@playwright/test";
import { test, expect } from "../../fixtures/test";
import { dataRows, expectPageTitle, expectSettled, loadPage, rowNamed, routes } from "../../helpers/app";
import { LIFECYCLE_TIMEOUT, dialog, expectLoading, optionNamed, pressOnce } from "../../helpers/resource";

/**
 * Egress policies: list, create, attach to an agent, see it in "Used by", edit, delete.
 * The created policy has a dotted name, which the API accepts as a DNS subdomain.
 *
 * The mock keeps writes in memory per page load, so after the first `goto` every step
 * moves in-app. The mock applies the CRD's rules, so a create it accepts is one a cluster
 * would accept too.
 */

const CREATED = "suite.example.com";
const CREATED_URL = new RegExp(`/policies/egress/kagent/${CREATED.replace(/\./g, "\\.")}$`);
const SEEDED = ["github-api", "internal-tools", "package-mirrors", "warehouse"];

test.describe.configure({ timeout: LIFECYCLE_TIMEOUT });

/** The Used by count is the list's last value to arrive, so rows have stopped moving. */
async function expectUsersCounted(page: Page) {
  await expect(rowNamed(page, "github-api").locator("td").nth(4)).toHaveText("2");
}

async function backToListSettled(page: Page) {
  await page.getByRole("link", { name: "Back to policies" }).click();
  await expect(page).toHaveURL(/\/policies\?tab=egress$/);
  await expectUsersCounted(page);
}

test("egress policies: a policy is created, attached, edited and deleted", async ({ page }) => {
  await test.step("1. an empty list and a failed read say different things", async () => {
    await loadPage(page, routes.policies, { scenario: "empty" });
    await expect(page.getByText("No egress policies yet.")).toBeVisible();

    await loadPage(page, routes.policies, { scenario: "error" });
    await expect(page.getByTestId("egress-policies-error")).toBeVisible();
    await expect(page.getByText("No egress policies yet.")).toHaveCount(0);

    // A dotted last segment still loads the app rather than a missing file.
    await loadPage(page, "/policies/egress/kagent/missing.example.com");
    await expect(page.getByTestId("egress-policy-not-found")).toContainText("kagent/missing.example.com");
  });

  // Last of the full loads: it ends on `?mock=ok`, which step 3 waits out before acting.
  await test.step("2. a loading state precedes the data", async () => {
    await expectLoading(page, routes.policies);
  });

  await test.step("3. every policy is listed with its origins, auth and users", async () => {
    await expectPageTitle(page, "Policies");
    await expect(page.getByTestId("policies-tabs").getByRole("tab", { name: "Egress Policies" })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("img", { name: "An agent or sandbox template lists a policy, and the policy lets it reach HTTP(S) hosts." })).toBeVisible();
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
    // github-api is used by the release-notes agent and the python-sandbox template.
    const github = rowNamed(page, "github-api");
    await expect(github).toContainText("Secret key");
    await expect(github.locator("td").nth(2)).toHaveText("2");
    await expect(github.locator("td").nth(4)).toHaveText("2");
    await expect(rowNamed(page, "internal-tools")).toContainText("Actor JWT");
    await expect(rowNamed(page, "package-mirrors")).toContainText("None");
  });

  await test.step("4. search matches origins as well as names", async () => {
    await page.getByPlaceholder("Search by name or origin").fill("pythonhosted");
    await expect(dataRows(page)).toHaveCount(1);
    await expect(rowNamed(page, "package-mirrors")).toHaveCount(1);
    await page.getByPlaceholder("Search by name or origin").fill("");
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });

  await test.step("5. the form checks the CRD's rules before saving", async () => {
    await page.getByRole("button", { name: "Create egress policy" }).click();
    await expectPageTitle(page, "New egress policy");
    await page.getByTestId("egress-form-submit").click();
    await expect(page.getByTestId("egress-form-errors")).toContainText("Enter a name.");

    await page.getByTestId("egress-form-name").fill(CREATED);
    await page.getByTestId("egress-destination-0-origin").fill("https://api.example.com");
    await page.getByTestId("egress-destination-0-auth").getByText("Secret key").click();
    await page.getByTestId("egress-destination-0-header").fill("Cookie");
    await page.getByTestId("egress-destination-0-secret-name").fill("example-credentials");
    await page.getByTestId("egress-destination-0-secret-key").fill("token");
    await page.getByTestId("egress-form-submit").click();
    await expect(page.getByTestId("egress-destination-0-problems")).toContainText("can't carry a credential");
    await expect(page).toHaveURL(/\/policies\/egress\/new$/);

    // A wildcard origin offers no credential source.
    await page.getByTestId("egress-form-add-destination").click();
    await expect(page.getByTestId("egress-destination-1-origin")).toBeFocused();
    await page.getByTestId("egress-destination-1-origin").fill("https://*.example.org");
    await expect(page.getByTestId("egress-destination-1-auth").getByRole("radio", { name: "Secret key" })).toBeDisabled();
  });

  await test.step("6. a valid policy is created and read back", async () => {
    await page.getByTestId("egress-destination-0-header").fill("Authorization");
    await page.getByTestId("egress-destination-0-prefix").fill("Bearer ");
    await page.getByTestId("egress-form-submit").click();
    await expect(page).toHaveURL(CREATED_URL);

    const destinations = page.getByTestId("egress-policy-destinations");
    await expect(rowNamed(page, "https://api.example.com")).toContainText("example-credentials/token");
    await expect(rowNamed(page, "https://api.example.com")).toContainText('"Bearer "');
    await expect(destinations).toContainText("https://*.example.org");
    await expect(page.getByTestId("egress-policy-users")).toContainText("No agents or sandbox templates list this policy.");
  });

  await test.step("7. an agent attaches the policy from its own namespace", async () => {
    await backToListSettled(page);
    await rowNamed(page, "github-api").getByRole("link", { name: "github-api", exact: true }).click();
    await page.getByTestId("egress-user-link-release-notes").click();
    await page.getByTestId("agent-edit").click();

    const select = page.getByTestId("agent-form-egress-refs");
    await expect(select).toContainText("github-api");
    await select.click();
    // Only policies in the agent's namespace are offered.
    await expect(optionNamed(page, "warehouse")).toHaveCount(0);
    await pressOnce(optionNamed(page, CREATED));
    await page.keyboard.press("Escape");
    await expect(select).toContainText(CREATED);
    await page.getByTestId("agent-form-submit").click();
    await expect(page).toHaveURL(/\/agents\?tab=agents$/);
  });

  await test.step("8. the policy lists the agent, and the agent applied it", async () => {
    await page.getByRole("link", { name: "Policies", exact: true }).click();
    await expect(page).toHaveURL(/\/policies$/);
    await expectUsersCounted(page);
    await rowNamed(page, CREATED).getByRole("link", { name: CREATED, exact: true }).click();
    const user = rowNamed(page, "release-notes");
    await expect(user).toContainText("Agent");
    await expect(page.getByTestId("egress-user-applied-release-notes")).toHaveText("Applied");
  });

  await test.step("9. sandbox templates show as text, applied when only another policy failed", async () => {
    await backToListSettled(page);
    await rowNamed(page, "github-api").getByRole("link", { name: "github-api", exact: true }).click();
    const sandbox = rowNamed(page, "python-sandbox");
    await expect(sandbox).toContainText("SandboxTemplate");
    await expect(sandbox.getByRole("link")).toHaveCount(0);
    // Its condition is False only for retired-mirror, so github-api was admitted.
    await expect(page.getByTestId("egress-user-applied-python-sandbox")).toHaveText("Applied");
  });

  await test.step("10. an edit replaces the destinations", async () => {
    await backToListSettled(page);
    await page.getByTestId(`edit-${CREATED}`).click();
    await expect(page.getByTestId("egress-form-name")).toHaveAttribute("readonly", "");
    await expect(page.getByTestId("egress-destination-0-origin")).toHaveValue("https://api.example.com");
    await page.getByTestId("egress-destination-1-remove").click();
    // One row is left and it has no remove button, so focus moves to "Add destination".
    await expect(page.getByTestId("egress-form-add-destination")).toBeFocused();
    await page.getByTestId("egress-destination-0-origin").fill("https://api2.example.com");
    await page.getByTestId("egress-form-submit").click();
    await expect(page).toHaveURL(CREATED_URL);
    await expect(rowNamed(page, "https://api2.example.com")).toHaveCount(1);
    await expect(page.getByTestId("egress-policy-destinations")).not.toContainText("example.org");
  });

  await test.step("11. deleting the policy removes it from the list", async () => {
    await page.getByTestId(`delete-${CREATED}`).click();
    // release-notes attached it in step 7.
    await expect(dialog(page)).toContainText("This policy is in use by 1 agent or sandbox template you can see.");
    await pressOnce(dialog(page).getByRole("button", { name: "Delete" }));
    await expect(page).toHaveURL(/\/policies\?tab=egress$/);
    await expectSettled(page);
    await expect(rowNamed(page, CREATED)).toHaveCount(0);
    await expect(dataRows(page)).toHaveCount(SEEDED.length);
  });
});
