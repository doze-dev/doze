import { expect, test, type Page } from "@playwright/test";

// The keyboard rules every page keeps, from the UX audit of 2026-11-06:
// a page key never acts on something other than what has focus, Tab is
// never taken, overlays own the keyboard while open.
test.use({ viewport: { width: 1280, height: 800 } });

/** tabTo presses Tab until a control whose text matches has focus, as a keyboard user would. */
// On macOS, Tab reaches only text fields unless keyboard navigation is on;
// Option-Tab reaches every control. WebKit here behaves as the app's web
// view does.
let TAB = "Tab";
test.beforeEach(({ browserName }) => {
  TAB = browserName === "webkit" ? "Alt+Tab" : "Tab";
});

async function tabTo(page: Page, text: RegExp) {
  for (let i = 0; i < 60; i++) {
    await page.keyboard.press(TAB);
    const t = await page.evaluate(() => (document.activeElement as HTMLElement | null)?.textContent?.trim() ?? "");
    if (text.test(t)) return;
  }
  throw new Error(`Tab never reached ${text}`);
}

/** calls records the verbs and stack actions the page sends. */
function calls(page: Page): string[] {
  const seen: string[] = [];
  page.on("request", (r) => {
    const m = r.url().match(/\/api\/(do|doall|stack)$/);
    if (m) seen.push(`${m[1]} ${r.postData() ?? ""}`);
  });
  return seen;
}

test("space on a focused button presses that button, not the selected stack's switch", async ({ page }) => {
  const seen = calls(page);
  await page.goto("/");
  await expect(page.locator(".lib")).toBeVisible();
  await tabTo(page, /^Settings/);
  await page.keyboard.press(" ");
  await page.waitForTimeout(300);
  expect(seen.filter((c) => c.startsWith("stack"))).toEqual([]);
});

test("esc in the stack's ▾ menu closes the menu and nothing else", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.getByRole("button", { name: "More ways to start shop" }).click();
  const menu = page.getByRole("menu");
  await expect(menu).toBeVisible();
  // Focus starts in the menu; the arrows move it.
  await expect(menu.locator("button").first()).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(menu.locator("button").nth(1)).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});

test("Tab reaches the controls of a service page", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.getByRole("button", { name: /^api,/ }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
  const reached = new Set<string>();
  for (let i = 0; i < 25; i++) {
    await page.keyboard.press(TAB);
    reached.add(await page.evaluate(() => (document.activeElement as HTMLElement | null)?.textContent?.trim().slice(0, 20) ?? ""));
  }
  expect([...reached].some((t) => t.startsWith("Restart"))).toBe(true);
  expect([...reached].some((t) => t.startsWith("Copy"))).toBe(true);
});

test("⏎ on a task page's button presses the button; it does not also run the task", async ({ page }) => {
  const seen = calls(page);
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.getByRole("button", { name: /^migrate,/ }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("migrate");
  await tabTo(page, /^(Copy output|Edit doze\.hcl)/);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  expect(seen.filter((c) => c.includes('"task"'))).toEqual([]);
});

test("a confirmation takes focus and keeps Tab inside it", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.getByRole("button", { name: /^db,/ }).click();
  await page.keyboard.press("s");
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute("aria-modal", "true");
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press(TAB);
    expect(await dialog.evaluate((d) => d.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
});
