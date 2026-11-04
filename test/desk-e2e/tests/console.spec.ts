import { expect, test } from "@playwright/test";

// Module consoles over the modules fixture (the data stack): postgres'
// activity tab against the PgActivity board, kafka's panels laid out as the
// dash laid them, an action with input, a destructive one asked first.

test("postgres' activity tab draws as the PgActivity board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
  await expect(page.locator(".tabs .tab")).toHaveText(["overview", "activity", "logs", /Terminal/]);
  // The overview carries the module's databases table after the vitals.
  const dbs = page.locator(".card", { has: page.getByRole("heading", { name: "databases", exact: true }) });
  await expect(dbs).toContainText("shop_test");
  await expect(dbs).toContainText("38 MB");

  await page.locator(".tab", { hasText: "activity" }).click();
  const ctr = page.getByLabel("counters");
  await expect(ctr.locator("div")).toHaveCount(5);
  await expect(ctr).toContainText("active2");
  await expect(ctr).toContainText("longest4.2 s");
  const sessions = page.getByRole("grid", { name: "sessions" });
  await expect(sessions.locator(".tr.h")).toContainText("pid");
  await expect(sessions.locator(".tr.h")).not.toContainText("query");
  await expect(sessions.locator(".tr.row")).toHaveCount(4);
  await expect(sessions.locator(".tr.row.on")).toContainText("4412");
  await expect(sessions.locator(".tr.row.on")).toContainText("◐ waiting");
  const query = page.locator(".card", { has: page.getByRole("heading", { name: "query", exact: true }) });
  await expect(query).toContainText("UPDATE orders SET status = $1 WHERE id = $2");
  await expect(query).toContainText("pid 4412");
  const actions = page.locator(".card", { has: page.getByRole("heading", { name: "actions", exact: true }) });
  await expect(actions.locator(".act")).toHaveText([/C\s*Cancel query/, /T\s*Terminate session/]);
  await expect(actions.locator(".act.d")).toHaveText(/Terminate session/);
  await expect(page.locator(".keys")).toContainText("cancel query");
  await expect(page.locator(".keys")).toContainText("it never wakes db");
  await page.screenshot({ path: `.desk-look/PgActivity-${info.project.name}.png` });

  // ↓ selects the next session; the query follows it.
  await page.keyboard.press("ArrowDown");
  await expect(sessions.locator(".tr.row.on")).toContainText("4410");
  await expect(query).toContainText("SELECT * FROM orders WHERE customer_id = $1");

  // T asks before terminating, in the dash's words, and runs on the selected row.
  await page.keyboard.press("T");
  const dlg = page.getByRole("dialog", { name: "Terminate session?" });
  await expect(dlg).toContainText("for 4410 · db");
  await expect(dlg).toContainText("This deletes data and cannot be undone.");
  await expect(dlg.locator(".btns .btn")).toHaveText([/Terminate session/, /Cancel/]);
  await expect(dlg.locator(".btn.danger")).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(dlg).toBeHidden();
  await expect(page.locator(".toast").first()).toContainText("Terminate session done");

  // C runs at once: nothing to ask.
  await page.keyboard.press("C");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".toast", { hasText: "Cancel query done" })).toBeVisible();
});

test("kafka's console lays its tables side by side with the tail below, and produce asks for input", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "events" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("events");
  await expect(page.locator(".tabs .tab").first()).toHaveText("console");
  const topics = page.getByRole("grid", { name: "topics" });
  await expect(topics.locator(".tr.h")).toContainText("from");
  await expect(topics.locator(".tr.row")).toHaveCount(3);
  await expect(topics.locator(".tr.row").first()).toContainText("1,043");
  await expect(topics.locator(".tr.row").nth(2)).toContainText("runtime");
  const groups = page.getByRole("grid", { name: "consumer groups" });
  await expect(groups.locator(".tr.row").nth(1)).toContainText("12");
  const tail = page.getByLabel("tail");
  await expect(tail.locator(".tr.mono")).toHaveCount(2);
  await expect(tail).toContainText("ord_1043");
  await page.screenshot({ path: `.desk-look/KafkaConsole-${info.project.name}.png` });

  // The tail follows the selected topic.
  await topics.locator(".tr.row").nth(1).click();
  await expect(tail.locator(".tr.mono")).toHaveCount(1);
  await expect(tail).toContainText("c_88");

  // Tab moves to the next table; ↓ moves there.
  await page.keyboard.press("Tab");
  await page.keyboard.press("ArrowDown");
  await expect(groups.locator(".tr.row.on")).toContainText("mailer");

  await page.keyboard.press("P");
  const dlg = page.getByRole("dialog", { name: "Produce a message" });
  await expect(dlg).toContainText("for emails · events");
  const doIt = dlg.getByRole("button", { name: /Do it/ });
  await expect(doIt).toBeDisabled();
  await dlg.getByLabel("message value").fill('{"hello": 1}');
  await expect(doIt).toBeEnabled();
  await page.keyboard.press("Enter");
  await expect(dlg).toBeHidden();
  await expect(page.locator(".toast").first()).toContainText("Produce a message done");
});
