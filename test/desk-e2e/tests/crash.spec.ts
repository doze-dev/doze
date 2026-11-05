import { expect, test } from "@playwright/test";

// The crash fixture (shop with api in a crash loop), against the Crash
// board: the red dot and count on the tab, the row, the page's banner and
// the last lines it said, and ⌘K's Needs you.
test.use({ viewport: { width: 1280, height: 800 } });

test("a crashing app shows as the Crash board does", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.getByRole("tab", { name: /^shop/ });
  await expect(tab.locator(".dot")).toHaveClass(/r/);
  await expect(tab.locator(".badge")).toHaveText("1");
  await tab.click();
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Tiles" }).click();
  const api = page.locator(".tile", { hasText: "api" }).first();
  await expect(api).toContainText("crashing");
  await expect(api).toContainText("try 3 of 5");
  await api.dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
  await expect(page.locator(".head .path")).toContainText("go run ./cmd/api · crashing");
  await expect(page.getByRole("button", { name: /Stop trying/ })).toBeVisible();
  await expect(page.getByRole("button", { name: /Try now/ })).toBeVisible();
  const banner = page.locator(".banner.bad");
  await expect(banner).toContainText("It exits soon after starting. try 3 of 5; doze will try again, then stop and leave it off until you say.");
  await expect(banner).toContainText("What it said is below.");
  const said = page.locator(".card", { hasText: "What it said" });
  await expect(said).toContainText("panic: STRIPE_KEY is required");
  await expect(said.getByRole("button", { name: "Copy" })).toBeVisible();
  await expect(page.locator(".side .si.on")).toContainText("crashing");
  await page.screenshot({ path: `.desk-look/Crash-${info.project.name}.png` });
});

test("⌘K puts what needs you first", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: "Search or do" });
  await expect(pal.locator(".ph").first()).toHaveText("Needs you");
  const row = pal.getByRole("group", { name: "Needs you" }).locator(".it").first();
  await expect(row).toContainText("api");
  await expect(row).toContainText("in shop · crashing · try 3 of 5");
  await expect(row.locator(".k")).toHaveText("⏎ open");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
});
