import { test, expect } from "../../fixtures/test";
import { operationCallCounts, rpc } from "../../helpers/mockCalls";

/**
 * Watching the substrate move.
 *
 * "Worker pod" changes on its own — the substrate suspends an actor and restarts it
 * elsewhere — so a page read once shows a placement that has already moved. Polling is
 * offered beside Refresh and is off until asked for: twice a second is a rate to watch
 * something at, not a rate to leave a page at.
 *
 * ## What is counted, and why it is not requests
 *
 * Reads are counted as *operations*, not as HTTP requests, because in mock mode there
 * are none: the API is served by a substituted transport, so `page.on("request")` sees
 * only the navigation and a `window.fetch` wrapper sees nothing whatever. Both
 * instruments answer zero for a page that is polling perfectly. Counting operations is
 * also what this test means — "refreshes the page" is a claim about reads, not about
 * HTTP — and it survives the next change of transport.
 *
 * The instrument itself must not be able to read zero for the wrong reason, in either
 * direction: `operationCallCounts` throws on an RPC the backend does not serve rather
 * than reporting nought calls to it.
 *
 * The count is what makes this a test rather than a screenshot: the failure that matters
 * is a control that reads "enabled" and polls nothing, which is exactly what the first
 * implementation here did — the caching layer deduplicated its revalidations over a window
 * longer than the interval, turning "twice a second" into once every two and a half.
 *
 * ## Why two RPCs are watched and only one is expected to climb
 *
 * The page reads the inventory and the list of namespaces, and polling deliberately
 * re-reads only the first: the namespace list is the page's scope control, not its data,
 * and re-reading it twice a second is a request per tick that can only ever answer the
 * same thing. Watching both is what makes that a tested decision rather than an
 * accident — a timer wired to "refresh everything" would show up here as the namespace
 * count climbing too.
 */

const SUBSTRATE = "/substrate";

const POLLED = rpc.substrateStatus;

/** The scope control's own read, which must stay still while the inventory moves. */
const NOT_POLLED = rpc.listNamespaces;

const READS = [POLLED, NOT_POLLED] as const;

const readCounts = (page: import("@playwright/test").Page) =>
  operationCallCounts(page, READS);

test("substrate: polling is off until asked for, then re-reads the inventory", async ({
  page,
}) => {
  await test.step("1. the page reads once and then leaves it alone", async () => {
    await page.goto(SUBSTRATE);
    await expect(page.getByTestId("substrate-actors-card")).toBeVisible();

    const afterLoad = await readCounts(page);
    expect(afterLoad[POLLED]).toBeGreaterThan(0);

    await page.waitForTimeout(1_500);
    expect(
      (await readCounts(page))[POLLED],
      "a page nobody asked to poll must not re-read on its own",
    ).toBe(afterLoad[POLLED]);
  });

  await test.step("2. the control sits beside Refresh, and says which it is", async () => {
    const toggle = page.getByTestId("substrate-poll-toggle");
    await expect(toggle).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-pressed", "false");
    await expect(toggle).toContainText("disabled");
  });

  await test.step("3. enabled, the inventory is re-read — and only the inventory", async () => {
    const before = await readCounts(page);
    await page.getByTestId("substrate-poll-toggle").click();
    await expect(page.getByTestId("substrate-poll-toggle")).toContainText("enabled");

    /*
     * Waited for, not counted inside a window.
     *
     * This asked for two reads within 2.2 seconds, which at the default of one second
     * allows exactly two — no margin at all, so a single tick landing late failed it.
     * Under a parallel run a late tick is ordinary. The claim is that polling re-reads
     * repeatedly; how quickly it gets there is the machine's business, and step 1 and
     * step 4 are what pin down the other end, where "must not re-read" is exact.
     */
    await expect
      .poll(async () => (await readCounts(page))[POLLED] - before[POLLED], {
        timeout: 15_000,
        message: "the inventory should be re-read while polling",
      })
      .toBeGreaterThanOrEqual(2);

    expect(
      (await readCounts(page))[NOT_POLLED],
      "the namespace list is the scope control, not the data — polling must leave it alone",
    ).toBe(before[NOT_POLLED]);
  });

  await test.step("4. disabled, it stops", async () => {
    await page.getByTestId("substrate-poll-toggle").click();
    await expect(page.getByTestId("substrate-poll-toggle")).toContainText("disabled");

    // A tick already in flight may still land, so the count is taken after a beat and then
    // has to hold still.
    await page.waitForTimeout(800);
    const settled = await readCounts(page);
    await page.waitForTimeout(1_800);
    expect(await readCounts(page), "turning it off must actually stop it").toEqual(settled);
  });
});

/**
 * The rate is the reader's, and so is stopping without losing it.
 *
 * A fixed rate was either too slow to watch a placement move or too fast to leave
 * running, so the interval is a field beside the toggle. Two of its values are not
 * rates at all: zero, and anything unparseable — antd hands back `null` for "." or an
 * empty box — and both stop the timer while leaving polling switched on, so pausing
 * does not cost the reader the number they had chosen.
 */
test("substrate: the polling interval is the reader's, and zero stops it", async ({
  page,
}) => {
  const interval = page.getByTestId("substrate-poll-interval").locator("input");

  await test.step("1. there is no interval to set until polling is on", async () => {
    await page.goto(SUBSTRATE);
    await expect(page.getByTestId("substrate-actors-card")).toBeVisible();
    await expect(page.getByTestId("substrate-poll-interval")).toHaveCount(0);
  });

  await test.step("2. switching polling on offers one, defaulting to the floor", async () => {
    await page.getByTestId("substrate-poll-toggle").click();
    // The fastest this page will ask: someone who turned polling on wants to see movement.
    await expect(interval).toHaveValue("0.5");
    await expect(page.getByTestId("substrate-poll-interval")).toContainText("seconds");
  });

  await test.step("2b. one second reads as one, not as ones", async () => {
    // Singular for exactly one: "1 seconds" reads as a page not reading its own value.
    await interval.fill("1");
    await interval.blur();
    await expect(page.getByTestId("substrate-poll-interval")).toContainText("second");
  });

  await test.step("3. the rate the reader set is the rate it re-reads at", async () => {
    await interval.fill("0.5");
    await interval.blur();
    const before = await readCounts(page);

    /*
     * Waited for rather than counted inside a fixed window.
     *
     * This asked for three re-reads within 2.2 seconds, which at half a second allows
     * four — so one stalled tick failed it, and under a parallel run one stalled tick
     * is ordinary. It was measuring how fast the machine could serve four reads, and
     * reporting a busy laptop as a broken timer.
     *
     * The claim kept is the one about the app: the timer fires repeatedly at the rate
     * it was given, rather than once. The exact cadence is deliberately not asserted —
     * it cannot be, from outside, without also asserting the hardware. Step 5 is what
     * holds the other end down: at zero it must not fire at all, and that one is
     * exact because "never" does not depend on how fast anything is.
     */
    await expect
      .poll(async () => (await readCounts(page))[POLLED] - before[POLLED], {
        timeout: 15_000,
        message: "polling at half a second should re-read repeatedly",
      })
      .toBeGreaterThanOrEqual(3);
  });

  await test.step("4. below the floor is read as the floor, not refused", async () => {
    await interval.fill("0.1");
    await interval.blur();
    // Corrected on the field, so the number on screen is the number being used.
    await expect(interval).toHaveValue("0.5");
  });

  await test.step("5. zero stops the timer without switching polling off", async () => {
    await interval.fill("0");
    await interval.blur();
    // The toggle still reads enabled: this is a pause, and the reader keeps their place.
    await expect(page.getByTestId("substrate-poll-toggle")).toContainText("enabled");

    const before = await readCounts(page);
    await page.waitForTimeout(2_200);
    expect(
      (await readCounts(page))[POLLED],
      "zero seconds must not re-read at all",
    ).toBe(before[POLLED]);
  });

  await test.step("6. and so does something that is not a number", async () => {
    await interval.fill(".");
    await interval.blur();
    const before = await readCounts(page);
    await page.waitForTimeout(1_800);
    expect(
      (await readCounts(page))[POLLED],
      "an unparseable interval must not re-read either",
    ).toBe(before[POLLED]);
  });
});
