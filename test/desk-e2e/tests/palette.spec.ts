import { expect, test } from "@playwright/test";

// ⌘K and the Keys sheet, against the Palette, PaletteTyped and Help
// boards, over the shop fixture.
test.use({ viewport: { width: 1280, height: 800 } });

const box = (page: import("@playwright/test").Page) => page.getByRole("textbox", { name: "A service, a stack, or what to do" });

test("nothing typed: Recent, then Do, with a verb's command at the right", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  // Opening a service's page puts it in Recent.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await page.locator(".ql .qr", { has: page.locator("b", { hasText: /^db$/ }) }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: "Search or do" });
  await expect(pal.locator(".ph")).toHaveText(["Recent", "Do"]);
  const recent = pal.getByRole("group", { name: "Recent" });
  await expect(recent.locator(".it").first()).toContainText("db");
  await expect(recent.locator(".it").first()).toContainText("in shop · copy URL, sleep, restart");
  const doRows = pal.getByRole("group", { name: "Do" }).locator(".it");
  await expect(doRows.nth(0)).toContainText("Sleep everything in shop");
  await expect(doRows.nth(0)).toContainText("wakes on connect; nothing is lost");
  await expect(doRows.nth(0).locator(".k")).toHaveText("sleep all");
  await expect(doRows.nth(1)).toContainText("Up acme");
  await expect(doRows.nth(1)).toContainText("stopped · as last time: api, web, db");
  await expect(doRows.nth(2)).toContainText("Open folder…");
  await expect(pal.locator(".foot")).toContainText("type a name, or a verb then a name: sleep db · logs api · copy web");
  await page.screenshot({ path: `.desk-look/Palette-${info.project.name}.png` });

  // Tab shows the row's actions; esc hides them, then closes.
  await page.keyboard.press("Tab");
  await expect(pal.locator(".it.on .acts kbd").first()).toHaveText("copy URL");
  await page.keyboard.press("Escape");
  await expect(pal.locator(".acts")).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(pal).toBeHidden();
});

test("a verb then a name: what it applies to, whether it asks, Did you mean", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: /Search or do/ });
  await box(page).fill("sleep ca");
  await expect(pal.locator(".ph")).toHaveText(["Sleep", "Did you mean"]);
  const cache = pal.locator(".it.on");
  await expect(cache).toContainText("cache");
  await expect(cache).toContainText(/in shop · (dozing, 11 MB · quiet; sleeps soon|awake)/);
  await expect(cache.locator(".k")).toHaveText("⏎ sleep now");
  await expect(cache.locator(".hl2").first()).toHaveText("ca");
  await expect(pal.getByRole("group", { name: "Did you mean" })).toContainText("Sleep everything in shop");
  await expect(pal.locator(".foot")).toContainText("verbs: up · down · sleep · wake · restart · pin · logs · copy · open · run");
  await page.screenshot({ path: `.desk-look/PaletteTyped-${info.project.name}.png` });

  // A service that something else needs asks first.
  await box(page).fill("sleep db");
  await expect(pal.locator(".it.on .k")).toHaveText("⏎ asks first");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "Sleep db?" })).toBeVisible();
  await page.keyboard.press("Escape");

  // A name alone finds services and stacks; ⏎ opens.
  await page.keyboard.press("ControlOrMeta+k");
  await box(page).fill("sho");
  await expect(pal.locator(".ph")).toHaveText(["Open"]);
  await expect(pal.locator(".it.on")).toContainText("shop");
  await expect(pal.locator(".it.on")).toContainText("~/code/shop");
  await box(page).fill("mig");
  await expect(pal.locator(".it.on")).toContainText("Run migrate");
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
});

test("nothing called that: the closest name, with the verb carried over", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: /Search or do/ });
  await box(page).fill("restart kafkaa");
  await expect(pal).toContainText("Nothing called kafkaa in shop or acme.");
  await expect(pal).toContainText("Closest: events (kafka, in shop).");
  await expect(pal.locator(".k")).toHaveText("⏎ use events");
  await expect(pal.locator(".foot")).toContainText("a verb alone lists what it can apply to; a name alone opens it with its actions");
  await page.screenshot({ path: `.desk-look/PaletteNothing-${info.project.name}.png` });
  // ⏎ puts the corrected command in the box to be read; it runs nothing.
  await page.keyboard.press("Enter");
  await expect(box(page)).toHaveValue("restart events");
  await expect(pal).toBeVisible();
});

test("? opens the Keys sheet, as the Help board draws it", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("?");
  const sheet = page.getByRole("dialog", { name: "Help" });
  await expect(sheet.locator("h3")).toHaveText(["Anywhere", "On a row or tile", "The words"]);
  await expect(sheet).toContainText("search a name, or type a verb and a name");
  await expect(sheet).toContainText("wake · sleep: the same switch as on a tile");
  await expect(sheet).toContainText("running, quiet for a while; sleeps when its timer runs out");
  await expect(sheet).toContainText("Every key here is a doze command too");
  await page.screenshot({ path: `.desk-look/Help-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});
