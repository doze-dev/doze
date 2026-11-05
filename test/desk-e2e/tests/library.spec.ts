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
  await expect(shop.locator(".fig")).toHaveText(/\d awake · \d waking · \d dozing · \d asleep/);
  await expect(shop.locator(".fig")).toContainText("551 MB · 4 connections");
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

test("a stopped stack is read from doze.hcl, ticked as last time, as the StackStopped board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".lrow", { hasText: "acme" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("acme");
  await expect(page.locator(".head .path")).toContainText("stopped · last up 3d ago");
  await expect(page.getByRole("button", { name: "Edit doze.hcl" })).toBeVisible();
  const banner = page.getByRole("status");
  await expect(banner).toContainText("Nothing is running. Tick what should start; the rest wakes when something connects to it.");
  await expect(banner).toContainText("Last time you started api, web and db. That is ticked for you.");
  await expect(banner.getByRole("button", { name: /Up as ticked/ })).toBeVisible();
  await expect(page.locator(".grp")).toHaveText([/APPS\s*3 · 2 ticked/, /MODULES\s*2 · wake when needed/, /TASKS\s*1 · run by hand/]);
  const api = page.locator(".lrow", { has: page.locator("b", { hasText: /^api$/ }) }).first();
  await expect(api.locator(".box")).toHaveClass(/on/);
  await expect(api).toContainText("db, cache · they start first");
  await expect(api).toContainText("api.acme.doze:8080");
  const web = page.locator(".lrow", { has: page.locator("b", { hasText: /^web$/ }) }).first();
  await expect(web).toContainText("https://dev.acme.com");
  await expect(web).toContainText("dev.acme.com");
  const worker = page.locator(".lrow", { has: page.locator("b", { hasText: /^worker$/ }) }).first();
  await expect(worker.locator(".box")).not.toHaveClass(/on/);
  await expect(worker).toContainText("nothing · not last time");
  await expect(page.locator(".lrow", { has: page.locator("b", { hasText: /^db$/ }) }).first()).toContainText("api needs it, so it starts first");
  await expect(page.locator(".lrow", { has: page.locator("b", { hasText: /^migrate$/ }) }).first()).toContainText("runs when you say, not on Up");
  await expect(page.locator(".model")).toContainText("Half-ticked means it is not started on Up");
  await page.screenshot({ path: `.desk-look/StackStopped-${info.project.name}.png` });

  // Space ticks the row the arrows are on; the heading counts it.
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press(" ");
  await expect(worker.locator(".box")).toHaveClass(/on/);
  await expect(page.locator(".grp").first()).toContainText("3 · 3 ticked");
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
  const themes = page.getByRole("radiogroup", { name: "Theme" });
  await expect(themes.getByRole("radio")).toHaveCount(6);
  await expect(themes).toContainText("violet");
  await expect(themes).toContainText("light");
  await page.screenshot({ path: `.desk-look/Settings-${info.project.name}.png` });

  // A theme is set at once and kept across a reload.
  await themes.getByRole("radio", { name: /teal/ }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "teal");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "teal");
  await page.keyboard.press("ControlOrMeta+,");
  await page.getByRole("radiogroup", { name: "Theme" }).getByRole("radio", { name: /violet/ }).click();
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
  await page.screenshot({ path: `.desk-look/Doctor-${info.project.name}.png` });

  await side.locator(".si", { hasText: "About" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("About");
  await expect(page.getByRole("button", { name: "doze.dev" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Library");
});
