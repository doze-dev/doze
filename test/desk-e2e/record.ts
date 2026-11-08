// record: the site's videos of the app (make site-media). bun run record.ts
// <list.json> <out-dir> [name...] plays each clip of the list against its
// fixture server (doze open --serve --fixture) and writes <name>.webm.
//
// A clip is a fixture, a stack, a view to start on, and steps a person
// would take: point and click, press keys, type, wait. Chromium's
// screencast gives the frames at the list's scale (2 for a Retina screen);
// Playwright's own ffmpeg turns them into VP9 at a steady 30 frames a
// second. A drawn pointer follows the mouse, so a viewer sees what was
// clicked.
import { chromium, type Page } from "playwright";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

type Step =
  | { wait: number }
  | { click: string; pause?: number }
  | { dblclick: string; pause?: number }
  | { move: string; pause?: number }
  | { key: string; pause?: number }
  | { type: string; pause?: number };
type Clip = { name: string; fixture: string; stack?: string; view?: string; steps: Step[] };
type List = { size: string; scale: number; now: string; clips: Clip[] };

const [listFile, outDir, ...only] = process.argv.slice(2);
if (!listFile || !outDir) {
  console.error("usage: bun run record.ts <list.json> <out-dir> [name...]");
  process.exit(2);
}
const list: List = JSON.parse(fs.readFileSync(listFile, "utf8"));
const bin = path.resolve(process.env.DOZE_BIN ?? "../../doze");
const ffmpeg = findFFmpeg();
const FPS = 30;
// at is where the pointer is, for glide.
let at = { x: 0, y: 0 };
fs.mkdirSync(outDir, { recursive: true });

const browser = await chromium.launch();
const failed: string[] = [];
for (const clip of list.clips.filter((c) => !only.length || only.includes(c.name))) {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "doze-site-media-"));
  const proc = Bun.spawn([bin, "open", "--serve", "127.0.0.1:0", "--fixture", clip.fixture], {
    env: { ...process.env, DOZE_FIXTURE_NOW: list.now, TZ: "UTC", HOME: home, DOZE_FIXTURE_HOME: home },
    stdout: "ignore",
    stderr: "pipe",
  });
  const [w, h] = list.size.split("x").map(Number);
  // The screencast is at CSS pixels whatever the device scale, so the page
  // is drawn at scale times the size and zoomed by scale: the layout of a
  // w×h window, at Retina sharpness.
  const page = await browser.newPage({ viewport: { width: w * list.scale, height: h * list.scale }, colorScheme: "dark", timezoneId: "UTC" });
  await page.addInitScript((z) => addEventListener("DOMContentLoaded", () => (document.documentElement.style.zoom = String(z))), list.scale);
  page.on("pageerror", (e) => console.error("page error:", e.message));
  try {
    const base = await firstURL(proc.stderr);
    await page.clock.setFixedTime(new Date(list.now));
    await page.addInitScript(pointer);
    await page.goto(base);
    await page.waitForSelector(".stab", { timeout: 10000 });
    if (clip.stack) {
      await page.getByRole("tab", { name: new RegExp(`^${clip.stack}`) }).click();
      await page.waitForSelector(".strip", { timeout: 10000 });
      if (clip.view) {
        const stacks: Array<{ key: string; name: string }> = await page.evaluate(() => fetch("/api/stacks", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).then((r) => r.json()));
        const key = stacks.find((s) => s.name === clip.stack)?.key;
        await page.evaluate(([k, v]) => fetch("/api/setview", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: k, view: v }) }), [key, clip.view]);
      }
    }
    await page.evaluate(() => document.fonts.ready);
    await page.mouse.move(w * list.scale * 0.62, h * list.scale * 0.55);
    at = { x: w * list.scale * 0.62, y: h * list.scale * 0.55 };
    await page.waitForTimeout(1200);
    const file = path.join(outDir, `${clip.name}.webm`);
    await record(page, file, async () => {
      for (const s of clip.steps) await play(page, s);
    });
    console.log(file);
  } catch (e) {
    failed.push(`${clip.name}: ${(e as Error).message.split("\n")[0]}`);
  } finally {
    await page.close();
    proc.kill();
    await proc.exited;
    fs.rmSync(home, { recursive: true, force: true });
  }
}
await browser.close();
if (failed.length) {
  console.error("not recorded:\n  " + failed.join("\n  "));
  process.exit(1);
}

async function play(page: Page, s: Step) {
  if ("wait" in s) return page.waitForTimeout(s.wait);
  if ("key" in s) await page.keyboard.press(s.key);
  else if ("type" in s) await page.keyboard.type(s.type, { delay: 110 });
  else {
    const sel = "click" in s ? s.click : "dblclick" in s ? s.dblclick : s.move;
    const box = await page.locator(sel).first().boundingBox();
    if (!box) throw new Error(`nothing at ${sel}`);
    await glide(page, box.x + box.width / 2, box.y + box.height / 2);
    await page.waitForTimeout(250);
    if ("click" in s) await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    if ("dblclick" in s) await page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2);
  }
  await page.waitForTimeout(s.pause ?? 700);
}

// glide moves the pointer there over about half a second, eased.
async function glide(page: Page, x: number, y: number) {
  const from = { ...at };
  const n = 24;
  for (let i = 1; i <= n; i++) {
    const t = i / n;
    const e = t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
    await page.mouse.move(from.x + (x - from.x) * e, from.y + (y - from.y) * e);
    await page.waitForTimeout(16);
  }
  at = { x, y };
}

// record screencasts the page while act runs, then encodes what it saw at a
// steady rate: a frame comes only when the page changes, so each is held
// until the next.
async function record(page: Page, file: string, act: () => Promise<void>) {
  const cdp = await page.context().newCDPSession(page);
  const frames: { t: number; jpg: Buffer }[] = [];
  cdp.on("Page.screencastFrame", async (f: { data: string; sessionId: number; metadata: { timestamp: number } }) => {
    frames.push({ t: f.metadata.timestamp, jpg: Buffer.from(f.data, "base64") });
    await cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }).catch(() => {});
  });
  const vp = page.viewportSize()!;
  await cdp.send("Page.startScreencast", { format: "jpeg", quality: 92, maxWidth: vp.width, maxHeight: vp.height, everyNthFrame: 1 });
  const start = Date.now() / 1000;
  await page.waitForTimeout(300);
  await act();
  const end = Date.now() / 1000;
  await cdp.send("Page.stopScreencast");
  if (!frames.length) throw new Error("no frames");
  // Its ffmpeg reads no pipes: the frames go through a file.
  const tmp = `${file}.mjpeg`;
  const out = fs.openSync(tmp, "w");
  let i = 0;
  for (let t = start; t < end; t += 1 / FPS) {
    while (i + 1 < frames.length && frames[i + 1].t <= t) i++;
    fs.writeSync(out, frames[i].jpg);
  }
  fs.closeSync(out);
  const enc = Bun.spawn(
    [ffmpeg, "-loglevel", "error", "-y", "-f", "image2pipe", "-framerate", String(FPS), "-c:v", "mjpeg", "-i", tmp,
      "-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p", "-crf", "34", "-b:v", "0", "-deadline", "good", "-cpu-used", "2", "-row-mt", "1", "-an", file],
    { stderr: "inherit" },
  );
  const code = await enc.exited;
  fs.rmSync(tmp, { force: true });
  if (code !== 0) throw new Error("ffmpeg failed");
}

// pointer draws a macOS-like arrow where the mouse is.
function pointer() {
  addEventListener("DOMContentLoaded", () => {
    const p = document.createElement("div");
    p.innerHTML = '<svg width="22" height="28" viewBox="0 0 22 28"><path d="M2 2 L2 22 L7.5 17 L11.5 26 L15 24.5 L11 15.5 L18.5 15.5 Z" fill="#111" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>';
    Object.assign(p.style, { position: "fixed", left: "0", top: "0", zIndex: "2147483647", pointerEvents: "none", transform: "translate(-2px,-2px)" });
    document.body.appendChild(p);
    // The page is zoomed (see above): place it in the zoomed units.
    addEventListener("mousemove", (e) => { const z = Number(document.documentElement.style.zoom || 1); p.style.left = e.clientX / z + "px"; p.style.top = e.clientY / z + "px"; }, true);
  });
}

// findFFmpeg is Playwright's own ffmpeg (it has VP9), or one on the PATH.
function findFFmpeg() {
  const cache = path.join(os.homedir(), os.platform() === "darwin" ? "Library/Caches/ms-playwright" : ".cache/ms-playwright");
  const dirs = fs.existsSync(cache) ? fs.readdirSync(cache).filter((d) => d.startsWith("ffmpeg-")).sort().reverse() : [];
  for (const d of dirs) {
    for (const f of ["ffmpeg-mac", "ffmpeg-linux", "ffmpeg"]) {
      const p = path.join(cache, d, f);
      if (fs.existsSync(p)) return p;
    }
  }
  return "ffmpeg";
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
