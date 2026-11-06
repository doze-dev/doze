import { expect, test } from "@playwright/test";

// ⌘K and the Keys sheet, against the Palette, PaletteTyped and Help
// boards, over the shop fixture.
test.use({ viewport: { width: 1280, height: 800 } });

const box = (page: import("@playwright/test").Page) => page.getByRole("textbox", { name: "A service, a stack, or what to do" });

test("nothing typed: commands for the page on screen, what you did, every stack", async ({ page }, info) => {
  await page.goto("/");
  // A failed start another spec left (the fixture's acme cannot start) is
  // rightly in Needs you; put away, so this checks the rest.
  await expect.poll(async () => ((await (await page.request.post("/api/library", { data: {} })).json()).stacks ?? []).filter((x: { busy?: string }) => x.busy).length, { timeout: 10000 }).toBe(0);
  const stacks = await (await page.request.post("/api/stacks", { data: {} })).json();
  for (const st of stacks) await page.request.post("/api/dismiss", { data: { key: st.key } });
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await page.locator(".ql .qr", { has: page.locator("b", { hasText: /^db$/ }) }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: "Search or do" });
  // On db's page, db's commands come first.
  await expect(pal.locator(".ph").first()).toHaveText("Suggested");
  const suggested = pal.getByRole("group", { name: "Suggested" }).locator(".it");
  await expect(suggested.first()).toContainText(/Sleep db|Wake db/);
  // The selected row shows its CLI form.
  await expect(suggested.first().locator(".k")).toContainText(/doze |asks first/);
  await expect(pal.getByRole("group", { name: "Stacks" })).toContainText("shop");
  await expect(pal.getByRole("group", { name: "Stacks" })).toContainText("acme");
  await page.screenshot({ path: `.desk-look/Palette-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(pal).toBeHidden();
});

test("tab goes into a stack, then a service; a copy says so in place; ⌫ steps out", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: /Search or do/ });
  await box(page).fill("shop");
  await expect(pal.locator(".it.on")).toContainText("shop");
  await page.keyboard.press("Tab");
  await expect(pal.locator(".crumb")).toHaveText(["shop ›"]);
  await expect(pal.locator(".ph")).toHaveText(["shop", "Services"]);
  await box(page).fill("db");
  await expect(pal.getByRole("group", { name: "Services" }).locator(".it").first()).toContainText("db");
  await pal.getByRole("group", { name: "Services" }).locator(".it").first().hover();
  await page.keyboard.press("Tab");
  await expect(pal.locator(".crumb")).toHaveText(["shop ›", "db ›"]);
  await box(page).fill("psql");
  const row = pal.locator(".it.on");
  await expect(row).toContainText("Copy the psql line");
  await page.keyboard.press("Enter");
  await expect(row.locator(".k")).toHaveText("Copied ✓");
  await expect(pal).toBeVisible();
  await page.screenshot({ path: `.desk-look/PaletteScoped-${info.project.name}.png` });
  await box(page).fill("");
  await page.keyboard.press("Backspace");
  await expect(pal.locator(".crumb")).toHaveText(["shop ›"]);
  await page.keyboard.press("Backspace");
  await expect(pal.locator(".crumb")).toHaveCount(0);
  await page.keyboard.press("Escape");
});

test("a name then words: a command for it, or its logs searched; a verb still works", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.keyboard.press("ControlOrMeta+k");
  const pal = page.getByRole("dialog", { name: /Search or do/ });
  await box(page).fill("db restart");
  await expect(pal.locator(".it.on")).toContainText("Restart db");
  await box(page).fill("api timeout");
  await expect(pal.locator(".it.on")).toContainText("Search api's logs for “timeout”");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("textbox", { name: "Filter the lines" })).toHaveValue("timeout");
  // api alone is ticked.
  await expect(page.getByRole("checkbox", { name: "api", exact: true })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: "web", exact: true })).not.toBeChecked();
  // A verb, and an alias for one.
  await page.keyboard.press("ControlOrMeta+k");
  await box(page).fill("stop ca");
  await expect(pal.locator(".ph").first()).toHaveText("Sleep");
  await box(page).fill("sleep db");
  await expect(pal.locator(".it.on .k")).toHaveText("⏎ asks first");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "Sleep db?" })).toBeVisible();
  await page.keyboard.press("Escape");
  // App commands are found by name.
  await page.keyboard.press("ControlOrMeta+k");
  await box(page).fill("theme te");
  await expect(pal.getByRole("group", { name: "Commands" })).toContainText("Theme: teal");
  await page.screenshot({ path: `.desk-look/PaletteTyped-${info.project.name}.png` });
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
  await expect(sheet.locator("h3")).toHaveText(["Anywhere", "Home", "On a row or tile", "Logs", "A crashing service", "The words"]);
  await expect(sheet).toContainText("search a name, or type a verb and a name");
  await expect(sheet).toContainText("wake · sleep: the same switch as on a tile");
  await expect(sheet).toContainText("running, quiet for a while; sleeps when its timer runs out");
  await expect(sheet).toContainText("Every key here is a doze command too");
  await page.screenshot({ path: `.desk-look/Help-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});
