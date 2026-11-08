// shot: screenshots of the page against a running `doze open --serve`, for
// looking at a screen beside its board. bun run shot.ts <url> <out-dir>
// [stack] [view...]. With a stack name it opens that tab and shoots each
// view asked for (tiles, list, map, grid; map:<card> with a card selected,
// grid:<row>/<column> with the pointer on a cell); without, it shoots the
// Library. bun run shot.ts board <out-dir> <board.dc.html...> shoots boards
// at the same size, to set beside them.
//
// bun run shot.ts docs <list.json> <out-dir> is the docs' screenshots
// (make docs-shots): it starts a fixture server for each fixture the list
// names, on a stopped clock, shoots each entry in the light (and, asked,
// the dark) scheme at the list's size and scale, and writes WebP.
//
// SHOT_SIZE (1280x800), SHOT_SCALE (device pixels per CSS pixel, 1) and
// SHOT_SCHEME (dark) set the browser for the first two forms.
import { chromium, type Browser, type Page } from "playwright";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";

const [url, out, stack, ...views] = process.argv.slice(2);
if (!url || !out) {
  console.error(
    "usage: bun run shot.ts <url> <out-dir> [stack] [tiles|list|map|grid|map:<card>|grid:<row>/<col> ...]\n" +
      "       bun run shot.ts board <out-dir> <board.dc.html...>\n" +
      "       bun run shot.ts docs <list.json> <out-dir>",
  );
  process.exit(2);
}

const api = (page: Page, route: string, body: unknown) =>
  page.evaluate(([r, b]) => fetch(r as string, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(b) }).then((x) => x.json().catch(() => null)), [route, body] as const);

// stackKey is the hub's key for a stack, by its name.
async function stackKey(page: Page, name: string) {
  const stacks: Array<{ key: string; name: string }> = await api(page, "/api/stacks", {});
  return stacks.find((s) => s.name === name)?.key ?? "";
}

async function openStack(page: Page, name: string) {
  await page.getByRole("tab", { name: new RegExp(`^${name}`) }).click();
  await page.waitForSelector(".strip", { timeout: 10000 });
}

// arrange brings the page to view v of stack, as a person would get there;
// it returns a suffix for the file name and what undoes it, if anything.
async function arrange(page: Page, name: string, key: string, v: string): Promise<{ file: string; full?: boolean; undo: () => Promise<void> }> {
  const none = async () => {};
  const back = async () => openStack(page, name);
  if (v === "needs") {
    // The Needs you panel, by its keys (⌘⇧N).
    await page.keyboard.press("ControlOrMeta+Shift+N");
    await page.getByRole("dialog", { name: "Needs you" }).waitFor({ timeout: 5000 });
    return { file: `${name}-needs`, undo: async () => { await page.keyboard.press("Escape"); } };
  }
  if (v === "settings" || v === "machine") {
    // ⌘, opens Settings; Machine is its second part.
    await page.keyboard.press("Meta+,");
    await page.waitForSelector(".sets", { timeout: 5000 });
    if (v === "machine") await page.locator(".sets .si", { hasText: "Machine" }).click();
    return { file: v, undo: async () => { await page.keyboard.press("Escape"); await back(); } };
  }
  if (v.startsWith("pal:")) {
    // ⌘K with something typed, as the PaletteTyped board shows.
    await page.keyboard.press("Meta+K");
    await page.waitForSelector(".pal input", { timeout: 5000 });
    await page.keyboard.type(v.slice("pal:".length));
    return { file: `${name}-pal-${v.slice("pal:".length).replace(/\W+/g, "_")}`, undo: async () => { await page.keyboard.press("Escape"); await page.keyboard.press("Escape"); } };
  }
  if (v === "logs" || v === "palette" || v === "help" || v === "timeline" || v === "sleep" || v.startsWith("sleep:")) {
    // Overlays and the logs page, from the overview by the same keys a
    // person would press.
    if (v === "logs") await page.keyboard.press("Shift+L");
    if (v === "timeline") await page.keyboard.press("Shift+T");
    if (v === "palette") await page.keyboard.press("Meta+K");
    if (v === "help") await page.keyboard.press("?");
    if (v === "sleep" || v.startsWith("sleep:")) {
      const who = v.startsWith("sleep:") ? v.slice("sleep:".length) : "db";
      await api(page, "/api/setview", { key, view: "list" });
      await page.waitForTimeout(600);
      await page.locator(".qr", { has: page.locator("b", { hasText: new RegExp(`^${who}$`) }) }).first().click();
      await page.waitForTimeout(200);
      await page.keyboard.press("s");
    }
    return {
      file: `${name}-${v.replace(/\W+/g, "_")}`,
      undo: async () => {
        await page.keyboard.press("Escape");
        await page.waitForTimeout(200);
        if (v === "logs" || v === "timeline") await back();
      },
    };
  }
  if (v.startsWith("inside:") || v.startsWith("console:") || v.startsWith("ilogs:")) {
    // A service's page: open it from the list, as a double click does;
    // console: and ilogs: then click that tab of the page.
    const who = v.slice(v.indexOf(":") + 1);
    const tab = v.startsWith("console:") ? '.ptab[data-tab^="console:"]' : v.startsWith("ilogs:") ? '.ptab[data-tab="logs"]' : "";
    await api(page, "/api/setview", { key, view: "list" });
    await page.waitForTimeout(600);
    await page.locator(".qr", { has: page.locator("b", { hasText: new RegExp(`^${who}$`) }) }).first().dblclick();
    await page.waitForSelector(".ptabs", { timeout: 10000 });
    if (tab) await page.locator(tab).first().click();
    return { file: `${name}-${who}${tab ? "-" + v.split(":")[0] : ""}`, undo: back };
  }
  if (v.startsWith("map:") || v.startsWith("grid:")) {
    // The map with a card selected (MapFlowSelected), or the grid with
    // the pointer on a cell (MapGrid's crosshair and tooltip).
    const [kind, what] = [v.slice(0, v.indexOf(":")), v.slice(v.indexOf(":") + 1)];
    await api(page, "/api/setview", { key, view: kind });
    await page.waitForTimeout(800);
    if (kind === "map") {
      await page.locator(`.flow [data-name="${what}"]`).click();
    } else {
      const [r, c] = what.split("/");
      await page.locator(`.g td[aria-label="${r} needs ${c}"], .g td[aria-label="${r} uses ${c}"]`).first().hover();
    }
    return { file: `${name}-${kind}-${what.replace(/\W+/g, "_")}`, undo: async () => { await page.keyboard.press("Escape"); await page.mouse.move(0, 0); } };
  }
  // The view is remembered in Go; the page follows the push.
  await api(page, "/api/setview", { key, view: v });
  return { file: `${name}-${v}`, full: v !== "list", undo: none };
}

if (url === "board") {
  // A board is a page of its own: its window, drawn at its size.
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const page = await newPage(browser, process.env.SHOT_SCHEME === "light" ? "light" : "dark");
  for (const f of [stack, ...views]) {
    await page.goto("file://" + path.resolve(f));
    await page.waitForTimeout(800);
    const file = `${out}/board-${path.basename(f, ".dc.html")}.png`;
    await page.locator(".win").first().screenshot({ path: file });
    console.log(file);
  }
  await browser.close();
} else if (url === "docs") {
  await docs(out, stack);
} else {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  const page = await newPage(browser, process.env.SHOT_SCHEME === "light" ? "light" : "dark");
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
    await openStack(page, stack);
    const key = await stackKey(page, stack);
    for (const v of views.length ? views : ["tiles"]) {
      const r = await arrange(page, stack, key, v);
      await page.waitForTimeout(v.startsWith("pal:") || ["logs", "palette", "help", "timeline"].includes(v) || v.startsWith("sleep") ? 900 : 800);
      await page.screenshot({ path: `${out}/${r.file}.png`, fullPage: !!r.full });
      console.log(`${out}/${r.file}.png`);
      await r.undo();
    }
  }
  await browser.close();
}

async function newPage(browser: Browser, scheme: "light" | "dark", size = process.env.SHOT_SIZE ?? "1280x800", scale = Number(process.env.SHOT_SCALE ?? 1), timezoneId?: string) {
  const [w, h] = size.split("x").map(Number);
  const page = await browser.newPage({ viewport: { width: w, height: h }, deviceScaleFactor: scale, colorScheme: scheme, reducedMotion: "reduce", timezoneId });
  page.on("pageerror", (e) => console.error("page error:", e.message));
  return page;
}

// ── the docs' screenshots ────────────────────────────────────────────────

type Shot = {
  // name is the file: <name>.webp, and <name>-dark.webp with dark.
  name: string;
  // fixture is the scripted stack the server serves (doze open --fixture).
  fixture: string;
  // stack is the tab to open; none shoots Home.
  stack?: string;
  // view is what arrange brings up: tiles, list, map, grid, map:<card>,
  // grid:<row>/<col>, inside:<name>, ilogs:<name>, console:<name>, logs,
  // palette, pal:<typed>, sleep:<name>, settings, machine, help, timeline.
  view?: string;
  dark?: boolean;
  // scheme shoots this one scheme alone, as <name>.webp (the site's dark captures).
  scheme?: "light" | "dark";
  // hold is a port to listen on while shooting, so the page has a holder
  // to name (the port clash).
  hold?: number;
  // full shoots the whole page, not the window.
  full?: boolean;
};
type List = { size: string; scale: number; now: string; shots: Shot[] };

async function docs(listFile: string, outDir: string) {
  if (!outDir) {
    console.error("usage: bun run shot.ts docs <list.json> <out-dir>");
    process.exit(2);
  }
  const list: List = JSON.parse(fs.readFileSync(listFile, "utf8"));
  const bin = path.resolve(process.env.DOZE_BIN ?? "../../doze");
  const sharp = (await import("sharp")).default;
  fs.mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch();
  const failed: string[] = [];
  try {
    // One server per fixture, all on the list's clock, started as each is
    // first needed and stopped at the end.
    // The fixture lives in home, which is also the server's HOME: the page
    // says ~/code/larder on every run. One server at a time, since they
    // share it.
    const home = path.join(os.tmpdir(), "doze-docs-shots", "home");
    const servers = new Map<string, { url: string; proc: ReturnType<typeof Bun.spawn> }>();
    const serve = async (fixture: string) => {
      let s = servers.get(fixture);
      if (s) return s.url;
      const proc = Bun.spawn([bin, "open", "--serve", "127.0.0.1:0", "--fixture", fixture], {
        env: { ...process.env, DOZE_FIXTURE_NOW: list.now, TZ: "UTC", HOME: home, DOZE_FIXTURE_HOME: home },
        stdout: "ignore",
        stderr: "pipe",
      });
      const url = await firstURL(proc.stderr);
      servers.set(fixture, { url, proc });
      return url;
    };
    try {
      for (const shot of list.shots) {
        for (const scheme of shot.scheme ? [shot.scheme] : shot.dark ? (["light", "dark"] as const) : (["light"] as const)) {
          const file = path.join(outDir, `${shot.name}${scheme === "dark" && !shot.scheme ? "-dark" : ""}.webp`);
          // A fresh server per shot: nothing one shot did (a view chosen,
          // a card selected) is seen by the next.
          for (const s of servers.values()) {
            s.proc.kill();
            await s.proc.exited;
          }
          servers.clear();
          const base = await serve(shot.fixture);
          const holder = shot.hold ? await hold(shot.hold) : null;
          const page = await newPage(browser, scheme, list.size, list.scale, "UTC");
          try {
            await page.clock.setFixedTime(new Date(list.now));
            await page.goto(base);
            await page.waitForSelector(".stab", { timeout: 10000 });
            const full = !!shot.full;
            if (!shot.stack) {
              await page.waitForSelector(".lib, .ways, .empty", { timeout: 10000 });
            } else {
              await openStack(page, shot.stack);
              await arrange(page, shot.stack, await stackKey(page, shot.stack), shot.view ?? "tiles");
            }
            await page.evaluate(() => document.fonts.ready);
            await page.waitForTimeout(1200);
            const png = await page.screenshot({ fullPage: full, animations: "disabled", caret: "hide" });
            await sharp(png).webp({ quality: 85 }).toFile(file);
            console.log(file);
          } catch (e) {
            failed.push(`${shot.name} (${scheme}): ${(e as Error).message.split("\n")[0]}`);
          } finally {
            await page.close();
            holder?.close();
          }
        }
      }
    } finally {
      for (const s of servers.values()) s.proc.kill();
    }
  } finally {
    await browser.close();
  }
  if (failed.length) {
    console.error("not shot:\n  " + failed.join("\n  "));
    process.exit(1);
  }
}

// firstURL reads a fixture server's stderr until it says where it is.
async function firstURL(stream: ReadableStream<Uint8Array>) {
  const reader = stream.getReader();
  let text = "";
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const { value, done } = await reader.read();
    if (done) break;
    text += new TextDecoder().decode(value);
    const m = text.match(/doze app at (http:\/\/\S+)/);
    if (m) {
      reader.releaseLock();
      return m[1];
    }
  }
  throw new Error("the fixture server did not start: " + text.trim());
}

// hold listens on port, so the page has something to name as its holder.
async function hold(port: number) {
  const srv = net.createServer().listen(port, "127.0.0.1");
  // Taken already: then that program is the holder the page names.
  const ok = await new Promise<boolean>((done) => {
    srv.once("listening", () => done(true));
    srv.once("error", () => done(false));
  });
  return ok ? srv : null;
}
