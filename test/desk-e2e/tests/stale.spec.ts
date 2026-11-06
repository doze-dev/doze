import { expect, test } from "@playwright/test";

// The stale fixture: the shop's daemon answered once and then stopped. The
// page keeps the last word heard, says so, and nothing can be switched.
test.use({ viewport: { width: 1280, height: 800 } });

test("a daemon that stopped answering: the banner, faded figures, and switches that cannot act", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByText(/daemon not answering/).first()).toBeVisible({ timeout: 10000 });
  await expect(page.locator(".strip")).toHaveClass(/faded/);
  const sw = page.getByRole("switch", { name: /^api is/ });
  await expect(sw).toBeDisabled();
  // The tab says it on every page, and so does the Library row.
  await expect(page.getByRole("tab", { name: /^shop/ })).toHaveAttribute("title", /daemon not answering/);
  await page.getByRole("tab", { name: /Home/ }).click();
  await expect(page.locator(".lrow", { hasText: "shop" }).first()).toContainText("daemon not answering");
});
