import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

// The badconfig fixture: the shop, running, with its doze.hcl saved with a
// port that is not a number. Against the ConfigError board: the stack keeps
// running, the page says what is wrong and where, and a save that fixes the
// file clears it.
test.use({ viewport: { width: 1280, height: 800 } });

test("a doze.hcl that stops loading is shown with its line, and a fix clears it", async ({ page }, info) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  const banner = page.getByRole("alert");
  await expect(banner).toContainText(/doze\.hcl changed .* ago and does not load: the stack keeps running as it was\./);
  await expect(banner.getByRole("button", { name: /Edit doze\.hcl/ })).toBeVisible();
  const src = page.getByRole("figure", { name: /doze\.hcl around line 10/ });
  await expect(src.locator(".bad-l")).toHaveText('  port  = "eighty-eighty-one"');
  await expect(page.locator(".srccard")).toContainText("Unsuitable value type");
  await expect(page.locator(".srccard")).toContainText("a number is required");
  // The services are still there: nothing stopped.
  await expect(page.locator(".tile", { hasText: "api" }).first()).toContainText("awake");
  await page.screenshot({ path: `.desk-look/ConfigError-${info.project.name}.png` });

  // Save a fix: the page reads the file again and the banner goes.
  const dir = (await page.locator(".head .path").textContent())!.split(" · ")[0];
  const file = path.join(dir, "doze.hcl");
  const fixed = fs.readFileSync(file, "utf8").replace('"eighty-eighty-one"', "8081");
  fs.writeFileSync(file, fixed);
  await expect(page.getByRole("alert")).toHaveCount(0, { timeout: 8000 });
  // And break it again for the other browser.
  fs.writeFileSync(file, fixed.replace("port  = 8081", 'port  = "eighty-eighty-one"'));
});
