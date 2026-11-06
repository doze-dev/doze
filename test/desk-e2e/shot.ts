// shot: screenshots of the page against a running `doze open --serve`, for
// looking at a screen beside its board. bun run shot.ts <url> <out-dir>
// [stack] [view...]. With a stack name it opens that tab and shoots each
// view asked for (tiles, list, map); without, it shoots the Library.
import { chromium } from "playwright";
import fs from "node:fs";

const [url, out, stack, ...views] = process.argv.slice(2);
if (!url || !out) {
  console.error("usage: bun run shot.ts <url> <out-dir> [stack] [tiles|list|map ...]");
  process.exit(2);
}
fs.mkdirSync(out, { recursive: true });

const browser = await chromium.launch();
const [w, h] = (process.env.SHOT_SIZE ?? "1280x800").split("x").map(Number);
const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: 1, colorScheme: (process.env.SHOT_SCHEME as "dark" | "light") ?? "dark" });
page.on("pageerror", (e) => console.error("page error:", e.message));
await page.goto(url);
await page.waitForSelector(".stab", { timeout: 10000 });
if (!stack) {
  await page.waitForSelector(".lib, .ways, .empty, .banner", { timeout: 10000 });
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${out}/library.png` });
  console.log(`${out}/library.png`);
} else if (views[0] === "stopped") {
  // A stopped stack, opened from the Library by its name.
  await page.waitForSelector(".lib", { timeout: 10000 });
  await page.locator(".lrow", { has: page.locator("b", { hasText: new RegExp(`^${stack}$`) }) }).first().dblclick();
  await page.waitForSelector(".strip", { timeout: 10000 });
  // The same page as a running stack, in each of its views.
  for (const [i, v] of ["tiles", "list", "map"].entries()) {
    await page.keyboard.press(String(i + 1));
    await page.waitForTimeout(500);
    await page.screenshot({ path: `${out}/${stack}-stopped-${v}.png` });
    console.log(`${out}/${stack}-stopped-${v}.png`);
  }
} else {
  await page.getByRole("tab", { name: new RegExp(`^${stack}`) }).click();
  await page.waitForSelector(".strip", { timeout: 10000 });
  const stacks: Array<{ key: string; name: string }> = await page.evaluate(() => fetch("/api/stacks", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).then((r) => r.json()));
  const key = stacks.find((s) => s.name === stack)?.key ?? "";
  for (const v of views.length ? views : ["tiles"]) {
    if (v === "settings" || v === "machine") {
      // ⌘, opens Settings; Machine is its second part.
      await page.keyboard.press("Meta+,");
      await page.waitForSelector(".sets", { timeout: 5000 });
      if (v === "machine") await page.locator(".sets .si", { hasText: "Machine" }).click();
      await page.waitForTimeout(600);
      await page.screenshot({ path: `${out}/${v}.png` });
      console.log(`${out}/${v}.png`);
      await page.keyboard.press("Escape");
      await page.getByRole("tab", { name: new RegExp(`^${stack}`) }).click();
      await page.waitForSelector(".strip", { timeout: 10000 });
      continue;
    }
    if (v.startsWith("pal:")) {
      // ⌘K with something typed, as the PaletteTyped board shows.
      await page.keyboard.press("Meta+K");
      await page.waitForSelector(".pal input", { timeout: 5000 });
      await page.keyboard.type(v.slice("pal:".length));
      await page.waitForTimeout(900);
      const file = `${out}/${stack}-pal-${v.slice("pal:".length).replace(/\W+/g, "_")}.png`;
      await page.screenshot({ path: file });
      console.log(file);
      await page.keyboard.press("Escape");
      await page.keyboard.press("Escape");
      await page.waitForTimeout(200);
      continue;
    }
    if (v === "logs" || v === "palette" || v === "help" || v === "sleep" || v === "timeline") {
      // Overlays and the logs page, from the overview by the same keys a
      // person would press.
      if (v === "logs") await page.keyboard.press("Shift+L");
      if (v === "timeline") await page.keyboard.press("Shift+T");
      if (v === "palette") await page.keyboard.press("Meta+K");
      if (v === "help") await page.keyboard.press("?");
      if (v === "sleep") {
        await page.evaluate(([k]) => fetch("/api/setview", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: k, view: "list" }) }), [key]);
        await page.waitForTimeout(600);
        await page.locator(".qr", { has: page.locator("b", { hasText: /^db$/ }) }).first().click();
        await page.waitForTimeout(200);
        await page.keyboard.press("s");
      }
      await page.waitForTimeout(900);
      await page.screenshot({ path: `${out}/${stack}-${v}.png` });
      console.log(`${out}/${stack}-${v}.png`);
      await page.keyboard.press("Escape");
      await page.waitForTimeout(200);
      if (v === "logs" || v === "timeline") {
        await page.getByRole("tab", { name: new RegExp(`^${stack}`) }).click();
        await page.waitForSelector(".strip", { timeout: 10000 });
      }
      continue;
    }
    if (v.startsWith("inside:") || v.startsWith("console:") || v.startsWith("ilogs:")) {
      // A service's page: open it from the list, as a double click does;
      // console: and ilogs: then click that tab of the page.
      const name = v.slice(v.indexOf(":") + 1);
      const want = v.startsWith("console:") ? /console/i : v.startsWith("ilogs:") ? /^logs/i : null;
      await page.evaluate(([k]) => fetch("/api/setview", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: k, view: "list" }) }), [key]);
      await page.waitForTimeout(600);
      await page.locator(".qr", { has: page.locator("b", { hasText: new RegExp(`^${name}$`) }) }).first().dblclick();
      await page.waitForSelector(".ptabs", { timeout: 10000 });
      if (want) {
        await page.locator(v.startsWith("console:") ? ".ptab[data-tab^=\"console:\"]" : ".ptab[data-tab=\"logs\"]").first().click();
      }
      await page.waitForTimeout(800);
      const file = `${out}/${stack}-${name}${want ? "-" + v.split(":")[0] : ""}.png`;
      await page.screenshot({ path: file });
      console.log(file);
      await page.getByRole("tab", { name: new RegExp(`^${stack}`) }).click();
      await page.waitForSelector(".strip", { timeout: 10000 });
      continue;
    }
    // The view is remembered in Go; the page follows the push.
    await page.evaluate(([k, view]) => fetch("/api/setview", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: k, view }) }), [key, v]);
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${out}/${stack}-${v}.png`, fullPage: v !== "list" });
    console.log(`${out}/${stack}-${v}.png`);
  }
}
await browser.close();
