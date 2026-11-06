import { expect, test } from "@playwright/test";

// The changed fixture: the shop runs a doze.hcl it read ten minutes ago, and
// the file was saved since.
test.use({ viewport: { width: 1280, height: 800 } });

test("a doze.hcl saved after the stack read it offers Restart the stack", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  const banner = page.locator(".banner", { hasText: "doze.hcl changed" });
  await expect(banner).toContainText("after the stack read it");
  await expect(banner.getByRole("button", { name: "Restart the stack" })).toBeVisible();
});
