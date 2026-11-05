import { expect, test } from "@playwright/test";

// P2 parity against the shop fixture: the dialogs in the dash's words
// (boards Confirm, Dialogs, Reset), the ⌘K palette (Search), toasts and
// recent messages (Notices). Screenshots land in .desk-look at the boards'
// size for the comparison.
test.use({ viewport: { width: 1280, height: 800 } });

test("sleeping db asks, as the Confirm board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
  await page.getByRole("button", { name: /^Sleep/ }).click();
  const dlg = page.getByRole("dialog", { name: "Sleep db?" });
  await expect(dlg.locator("h2")).toHaveText("Sleep db?");
  await expect(dlg.locator(".say")).toContainText("api is running and needs it, and 4 connections are open to it right now.");
  await expect(dlg.locator(".row2").nth(0)).toContainText("What happens");
  await expect(dlg.locator(".row2").nth(0)).toContainText("Connections drop.");
  await expect(dlg.locator(".row2").nth(1)).toContainText("Kept. Nothing is deleted.");
  await expect(dlg.locator(".btns .btn")).toHaveText([/Sleep db and api/, "Sleep db only", /Cancel/]);
  await expect(dlg.locator(".btns .btn").first()).toHaveClass(/pri/);
  await expect(dlg.locator(".foot")).toContainText("doze only asks when something else is affected.");
  await page.screenshot({ path: `.desk-look/Confirm-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(dlg).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
});

test("sleeping web never asks", async ({ page }) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "web" }).first().click();
  await expect(page.locator(".tr.row.on")).toContainText("web");
  await page.keyboard.press("s");
  await expect(page.getByRole("dialog")).toHaveCount(0);
});

test("restart db asks as the Dialogs board does, and Enter restarts", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().click();
  await page.keyboard.press("r");
  const dlg = page.getByRole("dialog", { name: "Restart db?" });
  await expect(dlg.locator("h2")).toHaveText("Restart db?");
  await expect(dlg.locator(".say")).toContainText("api is running and needs it, and 4 connections are open to it right now.");
  await expect(dlg.locator(".row2").nth(0)).toContainText("It stops and starts again.");
  await expect(dlg.locator(".btns .btn")).toHaveText([/Restart db/, /Cancel/]);
  await expect(dlg.locator(".foot")).toContainText("doze only asks when something is using it.");
  await page.screenshot({ path: `.desk-look/Dialogs-${info.project.name}.png` });
  await page.keyboard.press("Enter");
  await expect(dlg).toBeHidden();
});

test("reset db wants the name typed and defaults to Cancel, as the Reset board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().click();
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByLabel("Do what?").fill("reset db");
  await expect(page.locator(".sr.on")).toContainText("reset db");
  await expect(page.locator(".sr.on")).toContainText("wipes its data · asks first");
  await page.keyboard.press("Enter");
  const dlg = page.getByRole("dialog", { name: /reset db/ });
  await expect(dlg.locator("h2")).toContainText("reset db · this deletes data");
  await expect(dlg).toHaveClass(/danger/);
  const lines = dlg.locator(".lines");
  await expect(lines).toContainText("deletes");
  await expect(lines).toContainText("everything in ~/.doze/shop/db");
  await expect(lines).toContainText("stops");
  await expect(lines).toContainText("then");
  await expect(lines).toContainText("db starts empty on its next wake");
  await expect(lines).toContainText("way back");
  await expect(lines).toContainText("none. There is no snapshot.");
  const resetBtn = dlg.getByRole("button", { name: "Reset db" });
  const cancel = dlg.getByRole("button", { name: /Cancel/ });
  await expect(resetBtn).toBeDisabled();
  await expect(cancel).toHaveClass(/ring2/);
  await expect(dlg.locator(".foot")).toContainText("Cancel is the default button.");
  await expect(dlg.locator(".foot")).toContainText("From a script: doze reset db --yes");
  await dlg.getByLabel("Type db to confirm").fill("d");
  await expect(resetBtn).toBeDisabled();
  await page.screenshot({ path: `.desk-look/Reset-${info.project.name}.png` });
  await dlg.getByLabel("Type db to confirm").fill("db");
  await expect(resetBtn).toBeEnabled();
  // Typing the name moves the default onto Reset, as the dash did: ⏎ resets.
  await expect(resetBtn).toHaveClass(/ring2/);
  await page.keyboard.press("Enter");
  await expect(dlg).toBeHidden();
  await expect(page.locator(".toast", { hasText: "reset db failed" })).toHaveCount(0);
});

test("⌘K lists actions with their commands, as the Search board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "db" }).first().click();
  await page.getByRole("button", { name: /Command/ }).click();
  const pal = page.getByRole("dialog", { name: "Command palette" });
  await expect(pal.locator(".grp")).toHaveText("Actions");
  await expect(pal.locator(".sr").first()).toContainText("sleep db");
  await expect(pal.locator(".sr").first().locator(".cmd")).toHaveText("doze sleep db");
  await pal.getByLabel("Do what?").fill("sl");
  await expect(pal.locator(".sr.on")).toContainText("sleep db");
  await expect(pal.locator(".sr.on .hl2").first()).toHaveText("sl");
  await expect(pal.locator(".sr", { hasText: "sleep everything that is awake" }).locator(".cmd")).toHaveText("doze sleep");
  await expect(pal.locator(".ft")).toContainText("Every row is a shell command. The right column shows it.");
  await page.screenshot({ path: `.desk-look/Search-${info.project.name}.png` });
  await pal.getByLabel("Do what?").fill("stripe");
  await expect(pal.locator(".none")).toHaveText("nothing does that");
  await page.keyboard.press("Escape");
  await expect(pal).toBeHidden();
});

test("tab in ⌘K copies the command and says so", async ({ page, context, browserName }) => {
  test.skip(browserName === "webkit", "WebKit's clipboard needs a user gesture outside automation");
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByLabel("Do what?").fill("sleep ev");
  await expect(page.locator(".sr.on")).toContainText("sleep everything");
  await page.keyboard.press("Tab");
  await expect(page.locator(".toast")).toContainText("copied the command doze sleep");
  await expect(page.getByRole("dialog", { name: "Command palette" })).toBeHidden();
});

test("what was said is toasted and kept in recent messages, as the Notices board does", async ({ page }, info) => {
  await page.goto("/");
  await page.locator(".tr.row", { hasText: "migrate" }).first().click();
  // Running a task says so at once, as the TUI did.
  await page.keyboard.press("r");
  await expect(page.locator(".toast").first()).toContainText("running migrate…");
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByLabel("Do what?").fill("recent");
  await expect(page.locator(".sr.on")).toContainText("recent messages");
  await page.keyboard.press("Enter");
  const msgs = page.getByRole("region", { name: "Recent messages" });
  await expect(msgs.locator(".h")).toContainText("the last 20 · from ⌘K › recent messages");
  await expect(msgs.locator(".l").first()).toContainText("running migrate…");
  await expect(msgs.locator(".l").first().locator(".mono")).toHaveText(/^\d\d:\d\d:\d\d$/);
  await page.screenshot({ path: `.desk-look/Notices-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(msgs).toBeHidden();
  await expect(page.locator(".keys")).toContainText("the same keys as the TUI");
});

test("the theme switches from ⌘K and is remembered", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByLabel("Do what?").fill("theme light");
  await expect(page.locator(".sr.on")).toContainText("theme · light");
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByLabel("Do what?").fill("theme system");
  await expect(page.locator(".sr.on")).toContainText("theme · system");
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).not.toHaveAttribute("data-theme", /./);
});

test("? opens the Keys sheet, as the Help board draws it", async ({ page }, info) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.keyboard.press("?");
  const sheet = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(sheet.locator("h2")).toHaveText("Keys");
  await expect(sheet).toContainText("the TUI's keys, kept, plus the window's own");
  await expect(sheet.locator(".cols h3")).toHaveText(["Home", "This page", "Logs", "Everywhere", "Mouse"]);
  await expect(sheet).toContainText("copy address");
  await expect(sheet).toContainText("search and do anything");
  await expect(sheet.locator(".meters")).toContainText("cpu is cores in use");
  await page.screenshot({ path: `.desk-look/Help-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  // The page beneath did not get the Escape.
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
});
