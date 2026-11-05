import { expect, test } from "@playwright/test";

// A stack's overview in its three views, against the Services (tiles),
// ServicesList and Map boards, over the shop fixture.
test.use({ viewport: { width: 1280, height: 800 } });

async function openShop(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
}

test("tiles: one strip of figures, a tile per service with a switch", async ({ page }, info) => {
  await openShop(page);
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Tiles" }).click();
  await expect(page.locator(".head .path")).toContainText("~/code/shop · up");
  const strip = page.locator(".strip");
  await expect(strip.locator(".stat").nth(0)).toContainText(/\d{3} MB/);
  await expect(strip.locator(".stat").nth(0)).toContainText("resident");
  await expect(strip.locator(".stat").nth(0)).toContainText(/web 213 · mongo 148 · db 96 · \+\d more/);
  await expect(strip.locator(".stat").nth(1)).toContainText("cores");
  await expect(strip.locator(".stat").nth(2)).toContainText(/\d\s*awake/);
  await expect(strip.locator(".stat").nth(2)).toContainText(/\d waking · \d asleep/);
  await expect(strip.locator(".stat").nth(3)).toContainText("6");
  await expect(strip.locator(".stat").nth(3)).toContainText("tasks ✓ 1 done · ▸ 1 ready");
  await expect(page.getByRole("status")).toContainText("dev.shop.example is not reachable here yet");
  await expect(page.getByRole("status").getByRole("button", { name: /Set it up/ })).toBeVisible();
  await expect(page.locator(".gl")).toHaveText([/Apps\s*your code/, /Modules\s*from the doze registry/, /Outside\s*brought in by you/, /Tasks\s*run once, then out of the way/]);
  const api = page.locator(".tile", { hasText: "api" }).first();
  await expect(api).toContainText("awake · nothing holds it");
  await expect(api).toContainText("84 MB");
  await expect(api).toContainText("http://api.shop.doze:8080");
  await expect(api.getByRole("switch", { name: "api is awake" })).toHaveAttribute("aria-checked", "true");
  await expect(page.locator(".tile", { hasText: "cache" }).first()).toContainText("dozing · idle: sleeps soon");
  await expect(page.locator(".tile", { hasText: "db" }).first()).toContainText("api needs it · 4 connections");
  await expect(page.locator(".keys")).toContainText("wake / sleep");
  await page.screenshot({ path: `.desk-look/Services-${info.project.name}.png` });

  // The filter narrows every view; / focuses it.
  await page.keyboard.press("/");
  await page.getByRole("textbox", { name: "Filter the services" }).fill("db");
  await expect(page.locator(".tile")).toHaveCount(1);
  await page.keyboard.press("Escape");
});

test("list: rows with the details pane beside them; ⏎ opens the page", async ({ page }, info) => {
  await openShop(page);
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  const rows = page.locator(".ql .qr");
  await expect(rows.first()).toContainText("api");
  await expect(rows.first()).toHaveClass(/sel/);
  const pane = page.getByRole("complementary", { name: "api, in full" });
  await expect(pane).toContainText("process · go run ./cmd/api · awake 14m");
  await expect(pane).toContainText("api.shop.doze:8080");
  await expect(pane).toContainText("go run ./cmd/api");
  await expect(pane.locator(".chip")).toContainText(["db", "mongo", "cache", "migrate"]);
  await expect(pane.getByRole("button", { name: /Open/ })).toBeVisible();
  await page.screenshot({ path: `.desk-look/ServicesList-${info.project.name}.png` });

  // j moves, the pane follows; ⏎ opens the page; esc comes back.
  await page.keyboard.press("j");
  await expect(page.getByRole("complementary", { name: "web, in full" })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("web");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  // The view is remembered in Go: a reload lands on the list.
  await page.reload();
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.locator(".ql")).toBeVisible();
});

test("map: nodes by group with the edges of what needs what", async ({ page }, info) => {
  await openShop(page);
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Map" }).click();
  const map = page.getByRole("img", { name: /The stack as a map/ });
  await expect(map.locator(".node")).toHaveCount(12);
  await expect(map.locator(".node", { hasText: "api" }).first()).toContainText("84 MB");
  await expect(map.locator("svg path")).not.toHaveCount(0);
  await expect(page.locator(".legend")).toBeVisible();
  await page.screenshot({ path: `.desk-look/Map-${info.project.name}.png` });
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Tiles" }).click();
});

test("the switch on a row is sleep and wake: off sleeps it, on wakes it", async ({ page }) => {
  await openShop(page);
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Tiles" }).click();
  // mailpit is asleep and nothing depends on it: on wakes it, off sleeps it
  // without asking, and it ends as it began so the other checks see it so.
  await page.getByRole("switch", { name: "mailpit is asleep" }).click();
  await expect(page.getByRole("switch", { name: "mailpit is awake" })).toBeVisible();
  await expect(page.locator(".tile", { hasText: "mailpit" }).first()).toContainText("awake");
  await page.getByRole("switch", { name: "mailpit is awake" }).click();
  await expect(page.getByRole("switch", { name: "mailpit is asleep" })).toBeVisible();
  // db is held by api: its switch asks first, as Sleep does.
  await page.getByRole("switch", { name: "db is awake" }).click();
  await expect(page.getByRole("dialog", { name: "Sleep db?" })).toBeVisible();
  await page.keyboard.press("Escape");
});

test("all logs: every service merged, filtered in Go, doze itself among them", async ({ page }, info) => {
  await openShop(page);
  await page.keyboard.press("Shift+L");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("All logs");
  await expect(page.locator(".head .path")).toContainText("every service, merged");
  await expect(page.locator(".side")).toContainText("doze itself");
  await expect(page.locator(".ln.merged .who.acc").first()).toHaveText("doze");
  await page.getByRole("group", { name: "Level" }).getByRole("button", { name: "Warn+" }).click();
  await expect(page.locator(".lg .ln")).toHaveCount(1);
  await expect(page.locator(".lg .ln").first()).toContainText("slow query");
  await page.getByRole("group", { name: "Level" }).getByRole("button", { name: "Debug" }).click();
  await page.getByRole("textbox", { name: "Filter the lines" }).fill("vite");
  await expect(page.locator(".hl").first()).toHaveText(/vite/i);
  await page.screenshot({ path: `.desk-look/AllLogs-${info.project.name}.png` });
});

test("narrow: the strip and the head give way, nothing overlaps", async ({ page }, info) => {
  await page.setViewportSize({ width: 900, height: 620 });
  await openShop(page);
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await expect(page.locator(".strip")).toHaveClass(/narrow/);
  await expect(page.locator(".strip .stat").nth(1)).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("complementary", { name: "api, in full" })).toBeVisible();
  await page.screenshot({ path: `.desk-look/Narrow-${info.project.name}.png` });
});

test("esc goes up a level on every page; ⌘[ and ⌘] walk the way you came", async ({ page }) => {
  await openShop(page);
  await page.keyboard.press("Shift+L");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("All logs");
  await expect(page.locator(".keys")).toContainText("overview");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
  await page.keyboard.press("ControlOrMeta+[");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+[");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("All logs");
  await page.keyboard.press("ControlOrMeta+]");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  // The sidebar's Overview row is the same way back, by mouse.
  await page.keyboard.press("Shift+L");
  await page.locator(".side .si", { hasText: "Overview" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  // A service page too, and Settings.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+,");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("General");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
});
