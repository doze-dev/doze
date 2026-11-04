import { expect, test } from "@playwright/test";

// The daily loop against the shop fixture, checked against the boards'
// words (docs/design/desktop/boards/Main, Instance, Logs, AppLogs).
// Screenshots land in .desk-look at the boards' size for the comparison.
test.use({ viewport: { width: 1280, height: 800 } });

test("home shows the shop stack as the Main board does", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.locator(".stab", { hasText: "shop" });
  await expect(tab).toHaveClass(/on/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await expect(page.locator(".path")).toHaveText("~/code/shop");

  const vitals = page.locator(".vital");
  await expect(vitals).toHaveCount(4);
  await expect(vitals.nth(0)).toContainText("Memory");
  await expect(vitals.nth(0)).toContainText("MB");
  await expect(vitals.nth(2)).toContainText("4 awake");
  await expect(vitals.nth(2)).toContainText("1 dozing");
  await expect(vitals.nth(2)).toContainText("2 waking");
  await expect(vitals.nth(2)).toContainText("3 asleep");
  await expect(vitals.nth(3)).toContainText("tasks ✓ 1 done · ▸ 1 ready");

  await expect(page.locator(".tr.h")).toContainText("Engine");
  await expect(page.locator(".tr.h")).toContainText("Because");
  await expect(page.locator(".tr.grp")).toHaveText([/APPS\s*3 · your code/, /MODULES\s*4 · from the doze registry/, /OUTSIDE\s*3 · brought in by you/, /TASKS\s*2 · run once, then out of the way/]);

  const db = page.locator(".tr.row", { hasText: "db" }).first();
  await expect(db).toContainText("postgres 17");
  await expect(db).toContainText("awake");
  await expect(db).toContainText("api needs it · 4 connections");
  const cache = page.locator(".tr.row", { hasText: "cache" }).first();
  await expect(cache).toContainText("valkey 9");
  await expect(cache).toContainText("dozing");
  await expect(cache).toContainText("idle: sleeps soon");

  // The first row is selected and the dock follows it.
  await expect(page.locator(".tr.row.on")).toContainText("api");
  await expect(page.locator(".dock .cap")).toContainText("api");
  await expect(page.locator(".dock .ln").last()).toContainText("GET /healthz 200 0ms");
  await expect(page.locator(".keys")).toContainText("the same keys as the TUI");
  await page.screenshot({ path: `.desk-look/Main-${info.project.name}.png` });
});

test("an instance page lays out as the Instance board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
  await expect(page.locator(".head .path")).toContainText("postgres 17 · awake for 14m · pid 612");
  await expect(page.locator(".tabs .tab")).toHaveText(["overview", "logs", /Terminal/]);
  await expect(page.locator(".side .space.compact.on")).toContainText("db");
  const connect = page.locator(".card", { has: page.getByRole("heading", { name: "Connect", exact: true }) });
  await expect(connect).toContainText("postgres://shop@db.shop.doze:5432/shop");
  await expect(connect).toContainText("DATABASE_URL");
  await expect(connect).toContainText("~/.doze/shop/db");
  const why = page.locator(".card", { has: page.getByRole("heading", { name: "Why it's awake", exact: true }) });
  await expect(why).toContainText("api needs it");
  await expect(why).toContainText("4 connections");
  await expect(why).toContainText("With neither, it sleeps after 5:00");
  await expect(page.locator(".card", { hasText: "Memory · last" })).toContainText("96 MB");
  await expect(page.locator(".card", { hasText: "Recent" })).toContainText("woke");
  await page.screenshot({ path: `.desk-look/Instance-${info.project.name}.png` });

  await page.locator(".tab", { hasText: "logs" }).click();
  await expect(page.locator(".dock .cap")).toContainText("4 lines");
  await expect(page.locator(".rcol")).toContainText("levels");
  await page.screenshot({ path: `.desk-look/AppLogs-${info.project.name}.png` });
});

test("all logs filter in Go and show doze itself", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".dock .copy", { hasText: "All logs" }).click();
  await expect(page.locator(".dock .cap")).toContainText("all logs");
  await expect(page.locator(".side")).toContainText("doze itself");
  await expect(page.locator(".ln.merged .who.acc").first()).toHaveText("doze");
  await expect(page.locator(".dock .cap")).toContainText("lines");
  await page.getByLabel("Level").selectOption("2");
  await expect(page.locator(".dock .cap")).toContainText("1 lines");
  await expect(page.locator(".ln").first()).toContainText("slow query");
  await page.getByLabel("Level").selectOption("0");
  await page.getByLabel("Search logs").fill("vite");
  await expect(page.locator(".head")).toContainText("2 matches");
  await expect(page.locator(".hl").first()).toHaveText(/vite/i);
  await page.screenshot({ path: `.desk-look/Logs-${info.project.name}.png` });
});

test("a task's page is its last run", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "migrate" }).first().dblclick();
  await expect(page.locator(".head .path")).toContainText("task · done · ran 2m ago in 1.2s");
  await expect(page.locator(".box", { hasText: "history" })).toContainText("0015_orders_index.sql changed");
  await expect(page.locator(".dock .cap")).toContainText("its output");
  await page.screenshot({ path: `.desk-look/Task-${info.project.name}.png` });
});

test("keys: arrows move the selection, w wakes, Enter opens", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(".tr.row.on")).toContainText("api");
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".tr.row.on")).toContainText("web");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("web");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});
