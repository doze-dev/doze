import { expect, test } from "@playwright/test";

// Module consoles over the modules fixture (the data stack), against the
// ConsoleTab board's frame: postgres' activity tab, kafka's panels, an
// action asked first.
test.use({ viewport: { width: 1280, height: 800 } });

async function open(page: import("@playwright/test").Page, name: string) {
  await page.goto("/");
  await page.getByRole("tab", { name: /^data/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("data");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await page.locator(".ql .qr", { has: page.locator("b", { hasText: new RegExp(`^${name}$`) }) }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(name);
}

test("postgres' activity tab is one frame of counters, sessions, the query and the actions", async ({ page }, info) => {
  await open(page, "db");
  await expect(page.locator(".ptab")).toHaveText(["Overview", "activity", "Logs"]);
  await page.locator('.ptab[data-tab^="console:"]').first().click();
  const frame = page.locator(".frame");
  await expect(frame.locator(".ctop")).toContainText("db");
  await expect(frame.locator(".ctop")).toContainText("activity");
  await expect(frame.locator(".ctop")).toContainText("re-read every 2s while awake; it never wakes db");
  const stats = frame.getByLabel("counters");
  await expect(stats.locator(".cstat")).toHaveCount(5);
  await expect(stats).toContainText("active2");
  await expect(stats).toContainText("longest4.2 s");
  const sessions = page.getByRole("grid", { name: "sessions" });
  await expect(sessions.locator(".ctr.h")).toContainText("pid");
  await expect(sessions.locator(".ctr.row")).toHaveCount(4);
  await expect(sessions.locator(".ctr.row.on")).toContainText("4412");
  await expect(sessions.locator(".ctr.row.on")).toContainText("waiting");
  const query = page.locator(".card", { has: page.getByRole("heading", { name: "query", exact: true }) });
  await expect(query).toContainText("UPDATE orders SET status = $1 WHERE id = $2");
  await expect(query).toContainText("pid 4412");
  const actions = page.locator(".card", { has: page.getByRole("heading", { name: "actions", exact: true }) });
  await expect(actions.locator(".cact")).toHaveText([/Cancel query/, /Terminate session/]);
  await expect(actions.locator(".cact.d")).toContainText("Terminate session");
  await expect(page.locator(".keys")).toContainText("cancel query");
  await page.screenshot({ path: `.desk-look/ConsoleTab-${info.project.name}.png` });

  // ↓ moves the selection; Go resolves the query for the new row.
  await page.keyboard.press("ArrowDown");
  await expect(sessions.locator(".ctr.row.on")).toContainText("4410");
  // A destructive action asks first.
  await page.keyboard.press("T");
  const dlg = page.getByRole("dialog");
  await expect(dlg).toContainText("Terminate session");
  await page.keyboard.press("Escape");
  await expect(dlg).toBeHidden();
});

test("kafka's console lays its panels out: topics beside groups, the tail under them", async ({ page }, info) => {
  await open(page, "events");
  await page.locator('.ptab[data-tab^="console:"]').first().click();
  const frame = page.locator(".frame");
  await expect(frame.locator(".con-row").first().locator(".ctable")).toHaveCount(2);
  await expect(frame.getByRole("grid", { name: "topics" }).locator(".ctr.row")).toHaveCount(3);
  await expect(frame.getByRole("grid", { name: "topics" }).locator(".ctr.row.on")).toContainText("orders");
  await expect(frame.locator(".ctable", { hasText: "tail" })).toContainText("ord_1043");
  await expect(frame.locator(".cact")).toHaveText([/Produce a message/, /Keep topic in doze.hcl/]);
  await page.screenshot({ path: `.desk-look/ConsoleKafka-${info.project.name}.png` });
});

test("a module with its own web console opens it from the head", async ({ page }) => {
  await open(page, "cloud");
  await expect(page.getByRole("button", { name: /Open cloud.data.doze/ })).toBeVisible();
  await page.locator('.ptab[data-tab^="console:"]').first().click();
  await expect(page.locator(".frame .ctop")).toContainText("cloud");
  await expect(page.locator(".frame .ctable").first()).toContainText("uploads");
});
