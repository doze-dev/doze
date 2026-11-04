import { expect, test } from "@playwright/test";

// Home against the shop fixture: the tab, the meters, the four groups and
// the dash's words in the rows. Screenshots land in .desk-look for the
// board comparison.
test("home shows the shop stack in the dash's words", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.getByRole("tab", { name: /shop/ });
  await expect(tab).toBeVisible();
  await expect(tab).toHaveAttribute("aria-selected", "true");

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await expect(page.getByText("5 of 10 awake")).toBeVisible();

  const groups = page.locator(".group-title");
  await expect(groups).toHaveText(["APPS", "MODULES", "OUTSIDE", "TASKS"]);

  const db = page.getByRole("row").filter({ hasText: "db" }).first();
  await expect(db).toContainText("awake");
  await expect(db).toContainText("db.shop.doze:5432");
  await expect(db).toContainText("api needs it · 4 connections");

  const cache = page.getByRole("row").filter({ hasText: "cache" }).first();
  await expect(cache).toContainText("dozing");
  await expect(cache).toContainText("idle: sleeps soon");

  await expect(page.locator(".meter").first()).toContainText("mem");
  await page.screenshot({ path: `.desk-look/home-${info.project.name}.png` });
});

test("an instance page opens from a row and has its tabs", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("row").filter({ hasText: "db" }).first().click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("db");
  const tabs = page.getByRole("tab").filter({ hasNotText: "shop" });
  await expect(tabs).toHaveText(["overview", "logs"]);
  await expect(page.locator(".facts")).toContainText("postgres://shop@db.shop.doze:5432/shop");
  await expect(page.locator(".chart").first()).toContainText("memory");
  await page.screenshot({ path: `.desk-look/instance-db-${info.project.name}.png` });

  await page.getByRole("tab", { name: "logs" }).click();
  await expect(page.locator(".line").first()).toBeVisible();
  await expect(page.locator(".logs-bar")).toContainText("4 lines");
});

test("all logs filter by level and search in Go", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("button", { name: "all logs" }).click();
  await expect(page.locator(".logs-bar")).toContainText("23 lines");
  await page.getByLabel("Level").selectOption("2");
  await expect(page.locator(".logs-bar")).toContainText("1 lines");
  await expect(page.locator(".line").first()).toContainText("slow query");
  await page.getByLabel("Level").selectOption("0");
  await page.getByLabel("Search").fill("vite");
  await expect(page.locator(".logs-bar")).toContainText("2 lines");
  await page.screenshot({ path: `.desk-look/logs-${info.project.name}.png` });
});

test("a verb reaches the daemon", async ({ page }) => {
  await page.goto("/");
  const cloud = page.getByRole("row").filter({ hasText: "cloud" }).first();
  await cloud.hover();
  await cloud.getByRole("button", { name: "wake" }).click();
  // The fixture records the verb; the row does not change state on its own.
  await expect(page.locator(".notice.bad")).toHaveCount(0);
});
