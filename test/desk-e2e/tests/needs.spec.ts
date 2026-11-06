import { expect, test } from "@playwright/test";

// Needs you (D19), over the crash fixture: api in shop keeps crashing.
// Against the NeedsYouBell, NeedsYouPanel and BellEmpty boards.
test.use({ viewport: { width: 1280, height: 800 } });

const bell = (page: import("@playwright/test").Page) => page.getByRole("button", { name: /^Needs you/ });

test("the bell counts what is broken on every page, and its panel has each fix", async ({ page }, info) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(bell(page)).toHaveAccessibleName(/1 broken now/);
  await expect(bell(page).locator(".badge")).toHaveText("1");
  await page.screenshot({ path: `.desk-look/NeedsYouBell-${info.project.name}.png` });

  await bell(page).click();
  const panel = page.getByRole("dialog", { name: "Needs you" });
  await expect(panel).toContainText("Broken now");
  const api = panel.locator(".ni", { hasText: "api keeps crashing" });
  await expect(api).toContainText("· shop");
  await expect(api).toContainText("try 3 of 5");
  await expect(api.getByRole("button", { name: "Restart" })).toBeVisible();
  await expect(api.getByRole("button", { name: "Logs" })).toBeVisible();
  await expect(api.getByRole("button", { name: "Not now" })).toBeVisible();
  await page.screenshot({ path: `.desk-look/NeedsYouPanel-${info.project.name}.png` });

  // Its page is a click away, and the panel closes.
  await api.getByRole("button", { name: "Logs" }).click();
  await expect(panel).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
  // The bell is on this page too.
  await expect(bell(page)).toHaveAccessibleName(/1 broken now/);
});

test("⌘⇧N opens it, esc closes it and nothing else", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+Shift+N");
  const panel = page.getByRole("dialog", { name: "Needs you" });
  await expect(panel).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});

test("Not now puts an item aside until it says something new; the empty panel says what would show", async ({ page }, info) => {
  await page.goto("/");
  const post = (name: string, data: unknown) => page.evaluate(([n, d]) => fetch("/api/" + n, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(d) }).then((r) => r.json()), [name, data] as const);
  const before = (await post("attention", {})) as { items: { id: string }[] };
  await bell(page).click();
  const panel = page.getByRole("dialog", { name: "Needs you" });
  // Put everything aside (this machine's own setup may show here too).
  for (let i = 0; i < 8 && (await panel.locator(".ni:not(.fixed)").count()) > 0; i++) {
    const n = await panel.locator(".ni:not(.fixed)").count();
    await panel.locator(".ni:not(.fixed)").first().getByRole("button", { name: "Not now" }).click();
    await expect(panel.locator(".ni:not(.fixed)")).toHaveCount(n - 1);
  }
  await expect(panel).toContainText("Nothing new needs you.");
  await expect(panel).toContainText("put aside");
  await expect(panel.locator(".what")).toContainText("Broken now");
  await expect(panel.locator(".what")).toContainText("Worth doing");
  await expect(bell(page).locator(".badge")).toHaveCount(0);
  await page.screenshot({ path: `.desk-look/BellEmpty-${info.project.name}.png` });
  // Back as it was, for the other checks.
  for (const it of before.items) await post("notnow", { id: it.id, print: "" });
  await expect(bell(page).locator(".badge")).toHaveText("1");
});

test("⌘K's Needs you section lists the troubled service first", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.locator(".pal");
  await expect(pal).toContainText("Needs you");
  await expect(pal.locator(".it").first()).toContainText("api");
});
