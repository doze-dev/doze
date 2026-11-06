import { expect, test } from "@playwright/test";

// The Library, the stopped stack and Settings, against the Library,
// StackStopped, Settings and Doctor boards, over the shop fixture: one
// running stack and the seeded stopped one, acme.
test.use({ viewport: { width: 1280, height: 800 } });

test("the Library lists every stack with a switch, as the Library board does", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.getByRole("tab", { name: "Library" });
  await expect(tab).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
  await expect(page.locator(".head .path")).toHaveText("2 stacks · 1 up");
  const rows = page.getByRole("table", { name: "Stacks" }).locator(".lrow:not(.h)");
  await expect(rows).toHaveCount(2);
  const shop = rows.nth(0);
  await expect(shop).toContainText("shop");
  await expect(shop).toContainText("10 declared");
  await expect(shop.locator(".fig")).toContainText(/\d awake · /);
  await expect(shop.locator(".fig")).toContainText(/\d{3} MB · \d connections/);
  await expect(shop.locator(".pill").first()).toContainText("https://dev.shop.example");
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
  await expect(page.locator(".machine .btn")).toHaveText("Doctor");
  await expect(page.locator(".keys")).toContainText("the first tab; it never closes");
  await page.screenshot({ path: `.desk-look/Library-${info.project.name}.png` });

  // The ▾ beside the switch: as last time, everything, and Forget for a
  // stopped stack.
  await acme.getByRole("button", { name: "More ways to start acme" }).click();
  const menu = page.getByRole("menu", { name: "Ways to start acme" });
  await expect(menu).toContainText("As last time");
  await expect(menu).toContainText("api, web, db, and what they need");
  await expect(menu).toContainText("Everything");
  await expect(menu).toContainText("Forget this stack");
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
  await expect(strip.getByRole("button", { name: /Up as last time/ })).toBeVisible();

  // Nothing runs, so no switch is on.
  const tile = (n: string) => page.locator(".tile", { has: page.locator("b", { hasText: new RegExp(`^${n}$`) }) }).first();
  await expect(page.locator(".tile .sw.on")).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "api is down" })).toHaveAttribute("aria-checked", "false");
  await expect(tile("api")).toContainText("stopped · needs db and cache");
  await expect(tile("api")).toContainText("api.acme.doze:8080");
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
  await expect(page.getByRole("switch", { name: "worker is down" })).toBeVisible();
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
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
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
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
});
