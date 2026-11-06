import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const emptyDir = process.env.DOZE_E2E_EMPTY_DIR ?? "";

// A folder with no doze.hcl, opened in the app, against the OpenFolder
// board: what doze found, the three ways to start, the third writing the
// starter file. The server opens emptyDir over the shop fixture.
test.use({ viewport: { width: 1280, height: 800 } });

test("a folder with no doze.hcl offers the three ways to start", async ({ page }, info) => {
  // Each browser starts from an empty folder: the last one wrote the file.
  fs.rmSync(path.join(emptyDir, "doze.hcl"), { force: true });
  await page.goto("/");
  // The server opened the folder at start; a browser before this one may
  // have closed its page, which lets it go, so it is opened again, as ⌘O does.
  await page.evaluate((d) => fetch("/api/open", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ dir: d }) }), emptyDir);
  const tab = page.locator(".stab.on");
  await expect(tab).toContainText(path.basename(emptyDir));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(path.basename(emptyDir));
  await expect(page.locator(".head .path")).toContainText("no doze.hcl yet");
  const banner = page.getByRole("status");
  await expect(banner).toContainText("Nothing has started.");
  await expect(banner).toContainText("Nothing runs until you bring the stack up.");
  const choices = page.locator(".tile");
  await expect(choices).toHaveText([/1\s*Set it up from this repo/, /2\s*Add one thing/, /3\s*Start with an empty file/]);
  await expect(choices.nth(2)).toContainText("writes a short doze.hcl, with comments, to fill in");
  await expect(choices.first()).toHaveClass(/sel/);
  await expect(page.locator(".model")).toContainText("doze never edits doze.hcl behind your back");
  await expect(page.locator(".keys")).toContainText("close without writing anything");
  await page.screenshot({ path: `.desk-look/OpenFolder-${info.project.name}.png` });

  // 2 asks for a name; esc; 3 writes the starter file and says so.
  await page.keyboard.press("2");
  const dlg = page.getByRole("dialog", { name: "Add one thing" });
  await expect(dlg).toContainText("name a module or a service");
  await expect(dlg.getByRole("button", { name: /Add/ })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dlg).toBeHidden();
  await page.keyboard.press("3");
  await expect(page.locator(".toast").first()).toContainText("wrote a starter doze.hcl");
  const written = fs.readFileSync(path.join(emptyDir, "doze.hcl"), "utf8");
  expect(written).toContain('stack = "my-project"');
});
