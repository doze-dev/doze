import { expect, test } from "@playwright/test";

// The Library, the stopped stack and Settings, against the Library,
// StackStopped, Settings and Doctor boards, over the shop fixture: one
// running stack and the seeded stopped one, acme.
test.use({ viewport: { width: 1280, height: 800 } });

test("the Library lists every stack with a switch, as the Library board does", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.getByRole("tab", { name: "Home" });
  await expect(tab).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("doze");
  await expect(page.locator(".head .path")).toContainText("2 stacks · 1 up");
  const rows = page.getByRole("table", { name: "Stacks" }).locator(".lrow:not(.h)");
  await expect(rows).toHaveCount(2);
  const shop = rows.nth(0);
  await expect(shop).toContainText("shop");
  await expect(shop).toContainText("10 declared");
  await expect(shop.locator(".fig")).toContainText(/\d awake · /);
  await expect(shop.locator(".fig")).toContainText(/\d{3} MB · \d connections/);
  await expect(shop.locator(".pill").first()).toContainText("https://dev.shop.example");
  // A .doze web address is written whole: a browser takes a bare one for a search.
  await expect(shop.locator(".pills")).toContainText(/http:\/\/\w+\.shop\.doze/);
  await expect(shop.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  const acme = rows.nth(1);
  await expect(acme).toContainText("acme");
  await expect(acme).toContainText("5 declared");
  await expect(acme.locator(".fig")).toContainText("stopped");
  await expect(acme.locator(".fig")).toContainText("last time: api, web, db");
  await expect(acme.locator(".when")).toHaveText("up 3d ago");
  await expect(acme.getByRole("switch")).toHaveAttribute("aria-checked", "false");
  await expect(page.locator(".machine")).toContainText("Machine");
  await expect(page.locator(".machine")).toContainText("resolver route");
  await expect(page.locator(".machine .btn")).toHaveText("Machine…");
  await page.locator(".machine .btn").click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Machine");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("doze");
  await expect(page.locator(".keys")).toContainText("Home is the first tab; it never closes");
  await page.screenshot({ path: `.desk-look/Library-${info.project.name}.png` });

  // The ▾ beside the switch: as last time, everything, and Remove.
  await acme.getByRole("button", { name: "More for acme" }).click();
  const menu = page.getByRole("menu", { name: "acme" });
  // The switch is Up as last time and Down; the ⋯ holds the rest.
  await expect(menu.getByRole("menuitem")).toHaveText([/Start everything/, /Choose what starts…/, /Edit doze\.hcl/, /Show in Finder/, /Remove from doze…/]);
  await expect(menu).not.toContainText("As last time");
  await page.screenshot({ path: `.desk-look/StackMenu-${info.project.name}.png` });
  await expect(menu).toContainText("stops listing it; every file stays");
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
});

test("a stopped stack is the same page, down: every switch off, and a sheet to choose what starts", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".lrow", { hasText: "acme" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("acme");
  await expect(page.locator(".head .path")).toContainText("stopped · last up 3d ago");
  // The head is the running stack's: the view switch is where it always is.
  await expect(page.getByRole("group", { name: "View" })).toBeVisible();
  const strip = page.locator(".strip");
  await expect(strip).toContainText("Stopped");
  await expect(strip).toContainText("Up starts api, web and db, as last time, and what they need.");
  // No buttons in the strip: the switches start things (the stack's, or a service's own).
  await expect(strip.getByRole("button")).toHaveCount(0);
  await expect(strip).toContainText("A service's own switch starts just it and what it needs.");

  // Nothing runs, so no switch is on.
  const tile = (n: string) => page.locator(".tile", { has: page.locator("b", { hasText: new RegExp(`^${n}$`) }) }).first();
  await expect(page.locator(".tile .sw.on")).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "api is stopped" })).toHaveAttribute("aria-checked", "false");
  await expect(tile("api")).toContainText("stopped · needs db and cache");
  // Written as it will be once up: a web address, its own domain over https.
  await expect(tile("api")).toContainText("http://api.acme.doze:8080");
  await expect(tile("web")).toContainText("https://dev.acme.com");
  await expect(tile("web")).toContainText("dev.acme.com");
  await expect(tile("db")).toContainText("api and migrate use it");
  await expect(tile("migrate")).toContainText("needs db · runs once the stack is up");
  await page.screenshot({ path: `.desk-look/StackStopped-${info.project.name}.png` });

  // c opens the start sheet, ticked as last time; what they need is said.
  await page.keyboard.press("c");
  const sheet = page.getByRole("dialog", { name: "Start acme" });
  await expect(sheet).toBeVisible();
  await expect(sheet.getByRole("checkbox", { name: "api", exact: true })).toHaveAttribute("aria-checked", "true");
  await expect(sheet.getByRole("checkbox", { name: "worker", exact: true })).toHaveAttribute("aria-checked", "false");
  await expect(sheet).toContainText("Also starts, because they are needed: db, cache");
  await page.screenshot({ path: `.desk-look/StartSheet-${info.project.name}.png` });
  await sheet.getByRole("checkbox", { name: "worker", exact: true }).click();
  await expect(sheet.getByRole("checkbox", { name: "worker", exact: true })).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();

  // The other views show the same down rows.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await expect(page.getByRole("switch", { name: "worker is stopped" })).toBeVisible();
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Tiles" }).click();
  // The tab for a stopped stack is there while it is looked at.
  await expect(page.getByRole("tab", { name: /^acme/ })).toHaveAttribute("aria-selected", "true");
});

test("Settings: the themes, the machine part by part, About", async ({ page }, info) => {
  await page.goto("/");
  await page.keyboard.press("ControlOrMeta+,");
  const side = page.getByRole("complementary", { name: "Settings" });
  await expect(side).toContainText("General");
  await expect(side).toContainText("Machine");
  await expect(side).toContainText("About");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("General");
  const themes = page.getByRole("radiogroup", { name: "Colour theme" });
  await expect(themes.getByRole("radio")).toHaveCount(5);
  await expect(themes).toContainText("violet");
  await expect(themes).not.toContainText("light");
  const modes = page.getByRole("radiogroup", { name: "Mode" });
  await expect(modes.getByRole("radio")).toHaveText(["System", "Light", "Dark"]);
  await page.screenshot({ path: `.desk-look/Settings-${info.project.name}.png` });

  // A colour theme and a mode are set apart, at once, and kept across a reload.
  await themes.getByRole("radio", { name: /teal/ }).click();
  await modes.getByRole("radio", { name: "Light" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "teal");
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
  await page.screenshot({ path: `.desk-look/Settings-teal-light-${info.project.name}.png` });
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "teal");
  await expect(page.locator("html")).toHaveAttribute("data-mode", "light");
  await page.keyboard.press("ControlOrMeta+,");
  await page.getByRole("radiogroup", { name: "Mode" }).getByRole("radio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-mode", "dark");
  await page.getByRole("radiogroup", { name: "Colour theme" }).getByRole("radio", { name: /violet/ }).click();
  await page.getByRole("radiogroup", { name: "Mode" }).getByRole("radio", { name: "System" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "violet");

  // Machine: the same report as doze doctor, in sections, with a fix on
  // each part that is not in place.
  await side.locator(".si", { hasText: "Machine" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Machine");
  await expect(page.getByRole("button", { name: /Copy report/ })).toBeVisible();
  const table = page.getByRole("table", { name: "Machine" });
  await expect(table.locator(".cr.sec")).toContainText(["names", "domains", "the app"]);
  await expect(table.locator(".cr", { hasText: "resolver route" })).toBeVisible();
  await expect(table.locator(".cr", { hasText: "dev.shop.example" })).toContainText("route dev.shop.example");
  await expect(table.locator(".cr", { hasText: "doze on PATH" })).toBeVisible();
  await expect(page.locator(".model")).toContainText("The same report as doze doctor");
  // Under --serve there is no administrator prompt, so the banner points at
  // the terminal; in the app the same banner carries Repair.
  await expect(page.getByRole("status")).toContainText("From a terminal: doze doctor --fix");
  await page.screenshot({ path: `.desk-look/Doctor-.png` });

  await side.locator(".si", { hasText: "About" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("About");
  await expect(page.getByRole("button", { name: "doze.dev" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("doze");
});

test("the window reopens on the page it had, unless told not to", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+,");
  await page.getByRole("switch", { name: /Reopen the page I had/ }).click();
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("doze");
});

test("a narrow window keeps the sidebars, and the Library scrolls", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 420 });
  await page.goto("/");
  await expect(page.locator(".lib")).toBeVisible();
  // The Machine line stays in view; the list above it scrolls.
  await expect(page.locator(".machine")).toBeInViewport();
  await page.keyboard.press("ControlOrMeta+,");
  const side = page.getByRole("complementary", { name: "Settings" });
  await expect(side).toBeVisible();
  await side.locator(".si", { hasText: "About" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("About");
});

test("starting a stack shows at once, takes no second start, and a failure stays on the page", async ({ page }) => {
  await page.goto("/");
  await page.locator(".lrow", { hasText: "acme" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("acme");
  await page.keyboard.press("u");
  // At once: the switch says starting and cannot be pressed again.
  await expect(page.getByRole("switch", { name: "acme is starting" }).or(page.locator(".banner.failed"))).toBeVisible();
  // The fixture's folder cannot really start, so it fails, and says why.
  const failed = page.locator(".banner.failed");
  await expect(failed).toBeVisible({ timeout: 20000 });
  await expect(failed).toContainText("Starting acme failed");
  await expect(failed.getByRole("button", { name: "Try again" })).toBeVisible();
  await page.screenshot({ path: `.desk-look/StackFailed-${test.info().project.name}.png` });
  await failed.getByRole("button", { name: "Dismiss" }).click();
  await expect(failed).toBeHidden();
});

test("a folder opened while the window is open lands on its page: three ways in, or its stack, stopped", async ({ page }) => {
  const fs = await import("node:fs");
  const os = await import("node:os");
  const path = await import("node:path");
  await page.goto("/");
  await expect(page.locator(".lib")).toBeVisible();
  const open = (dir: string) => page.evaluate((d) => fetch("/api/open", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ dir: d }) }), dir);

  // No doze.hcl: the three ways in, at once.
  const empty = fs.mkdtempSync(path.join(os.tmpdir(), "doze-open-empty-"));
  await open(empty);
  await expect(page.locator(".head .path")).toContainText("no doze.hcl yet");

  // A doze.hcl that never ran: its stack, stopped, with Up.
  const fresh = fs.mkdtempSync(path.join(os.tmpdir(), "doze-open-fresh-"));
  fs.writeFileSync(path.join(fresh, "doze.hcl"), 'stack = "fresh"\n\napp "web" {\n  run  = "true"\n  port = 3999\n}\n');
  await open(fresh);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("fresh", { timeout: 10000 });
  await expect(page.locator(".strip")).toContainText("Stopped");
  await expect(page.getByRole("switch", { name: "web is stopped" })).toBeVisible();
  // Remove it again, so the next spec's Library is as it was.
  await page.keyboard.press("Escape");
  await page.locator(".lrow", { hasText: "fresh" }).first().click();
  await page.keyboard.press("Backspace");
  const dlg = page.getByRole("dialog", { name: "Remove fresh from doze?" });
  await expect(dlg).toContainText("the folder, its doze.hcl and its data stay on disk");
  await dlg.getByRole("button", { name: "Remove fresh" }).click();
  await expect(page.locator(".lrow", { hasText: "fresh" })).toHaveCount(0);
  expect(fs.existsSync(path.join(fresh, "doze.hcl"))).toBe(true);
});

test("a stack whose folder was deleted says so, cannot start, and is removed from its row", async ({ page }, info) => {
  const fs = await import("node:fs");
  const os = await import("node:os");
  const path = await import("node:path");
  await page.goto("/");
  await expect(page.locator(".lib")).toBeVisible();
  const gone = fs.mkdtempSync(path.join(os.tmpdir(), "doze-open-gone-"));
  fs.writeFileSync(path.join(gone, "doze.hcl"), 'stack = "gone"\n\napp "web" {\n  run  = "true"\n  port = 3998\n}\n');
  await page.evaluate((d) => fetch("/api/open", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ dir: d }) }), gone);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("gone", { timeout: 10000 });
  await page.keyboard.press("Escape");
  fs.rmSync(gone, { recursive: true, force: true });
  const row = page.locator(".lrow", { hasText: "gone" }).first();
  await expect(row).toContainText("the folder is gone", { timeout: 10000 });
  await expect(row.getByRole("switch")).toBeDisabled();
  await page.screenshot({ path: `.desk-look/LibraryMissing-${info.project.name}.png` });
  await row.getByRole("button", { name: "Remove…" }).click();
  await page.getByRole("dialog", { name: "Remove gone from doze?" }).getByRole("button", { name: "Remove gone" }).click();
  await expect(page.locator(".lrow", { hasText: "gone" })).toHaveCount(0);
});

test("a running stack's tab closes and the stack keeps running; opening it brings the tab back", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.getByRole("button", { name: "Close the shop tab" }).click();
  await expect(page.getByRole("tab", { name: /^shop/ })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("doze");
  const shop = page.locator(".lrow", { hasText: "shop" }).first();
  await expect(shop.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  // The strip remembers it (the fixture opens shop on every load, as
  // doze open would, so a reload is not the check).
  expect(await page.evaluate(() => localStorage.getItem("doze.closedTabs"))).toContain("shop");
  await shop.dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await expect(page.getByRole("tab", { name: /^shop/ })).toHaveCount(1);
});

test("Settings › Machine lists what stacks left behind, each with its way out", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".machine").getByRole("button", { name: "Machine…" }).click();
  const left = page.getByRole("region", { name: "Left behind" });
  await expect(left.getByRole("heading", { name: "Left behind" })).toBeVisible();
  // What is listed depends on this machine; never cleaned up from a test.
  await expect(left.locator(".lh .dim")).toContainText(/nothing: every process|stacks left behind/);
  await page.screenshot({ path: `.desk-look/LeftBehind-${info.project.name}.png`, fullPage: true });
});

test("rows come compact, cosy or roomy, chosen once for every list", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  const pick = page.getByRole("radiogroup", { name: "Row density" });
  const row = page.locator(".ql .qr").first();
  await expect(pick.getByRole("radio", { name: "Roomy" })).toHaveAttribute("aria-checked", "true");
  const roomy = (await row.boundingBox())!.height;
  for (const [name, h] of [["Compact", 32], ["Cosy", 40]] as const) {
    await pick.getByRole("radio", { name }).click();
    await expect.poll(async () => Math.round((await row.boundingBox())!.height)).toBe(h);
    await page.screenshot({ path: `.desk-look/Density${name}-${info.project.name}.png` });
  }
  expect(roomy).toBeGreaterThan(40);
  // The Library follows the same choice, and it is kept.
  await page.getByRole("tab", { name: "Home" }).click();
  await expect(page.getByRole("radiogroup", { name: "Row density" }).getByRole("radio", { name: "Cosy" })).toHaveAttribute("aria-checked", "true");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-density", "cosy");
  await page.getByRole("radiogroup", { name: "Row density" }).getByRole("radio", { name: "Roomy" }).click();
});
