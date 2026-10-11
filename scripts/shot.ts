// shot renders a screen to a PNG so it can be looked at.
//
//   bun scripts/shot.ts frame.ans out.png [width height]   a dash frame (ANSI)
//   bun scripts/shot.ts Main.dc.html out.png [width height] a board of the canvas
//
// The dash frame is drawn on the board's own background, type and line height
// (docs/design/next/boards/term.css), so the two images are the same size and
// can be put side by side.
import { readFileSync, writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const [src, out, w = "1200", h = "760"] = process.argv.slice(2);
if (!src || !out) {
  console.error("usage: bun scripts/shot.ts <frame.ans|board.html> <out.png> [width height]");
  process.exit(2);
}

const CHROME = process.env.CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";

function esc(s: string): string {
  // A terminal puts every character on a grid of equal cells; a browser gives a
  // fallback glyph its own width. Anything outside ASCII gets a cell of its own.
  let o = "";
  for (const ch of s) {
    if (ch === "&") o += "&amp;";
    else if (ch === "<") o += "&lt;";
    else if (ch === ">") o += "&gt;";
    else if (ch.codePointAt(0)! > 0x7e) o += `<span style="display:inline-block;width:1ch;overflow:visible;white-space:pre">${ch}</span>`;
    else o += ch;
  }
  return o;
}

// ansiToHtml turns SGR colour sequences into spans. It handles what lipgloss
// writes: 24-bit and 256-colour foreground and background, bold, faint,
// italic, underline, reverse and reset.
function ansiToHtml(text: string): string {
  const BG = "#15131d", FG = "#cfcbe3";
  let fg: string | null = null, bg: string | null = null;
  let bold = false, faint = false, italic = false, under = false, rev = false;
  let html = "", buf = "";
  const flush = () => {
    if (!buf) return;
    let f = fg ?? FG, b = bg;
    if (rev) [f, b] = [b ?? BG, f];
    const st: string[] = [`color:${f}`];
    if (b) st.push(`background:${b}`);
    if (bold) st.push("font-weight:700");
    if (faint) st.push("opacity:.6");
    if (italic) st.push("font-style:italic");
    if (under) st.push("text-decoration:underline");
    html += `<span style="${st.join(";")}">${esc(buf)}</span>`;
    buf = "";
  };
  const c256 = (n: number): string => {
    if (n < 16) {
      const base = ["#000", "#c00", "#0a0", "#c90", "#00c", "#c0c", "#0cc", "#ccc", "#666", "#f55", "#5f5", "#ff5", "#55f", "#f5f", "#5ff", "#fff"];
      return base[n];
    }
    if (n >= 232) { const v = 8 + (n - 232) * 10; return `rgb(${v},${v},${v})`; }
    n -= 16;
    const k = [0, 95, 135, 175, 215, 255];
    return `rgb(${k[Math.floor(n / 36)]},${k[Math.floor(n / 6) % 6]},${k[n % 6]})`;
  };
  const re = /\x1b\[([0-9;:]*)m|\x1b\[[0-9;?]*[A-Za-ln-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g;
  let last = 0, m: RegExpExecArray | null;
  const sgr = (params: string) => {
    const p = params === "" ? [0] : params.split(/[;:]/).map((x) => (x === "" ? 0 : parseInt(x, 10)));
    for (let i = 0; i < p.length; i++) {
      const c = p[i];
      if (c === 0) { fg = bg = null; bold = faint = italic = under = rev = false; }
      else if (c === 1) bold = true;
      else if (c === 2) faint = true;
      else if (c === 3) italic = true;
      else if (c === 4) under = true;
      else if (c === 7) rev = true;
      else if (c === 22) bold = faint = false;
      else if (c === 23) italic = false;
      else if (c === 24) under = false;
      else if (c === 27) rev = false;
      else if (c === 39) fg = null;
      else if (c === 49) bg = null;
      else if (c >= 30 && c <= 37) fg = c256(c - 30);
      else if (c >= 90 && c <= 97) fg = c256(c - 90 + 8);
      else if (c >= 40 && c <= 47) bg = c256(c - 40);
      else if (c >= 100 && c <= 107) bg = c256(c - 100 + 8);
      else if (c === 38 || c === 48) {
        let col: string | null = null;
        if (p[i + 1] === 2) { col = `rgb(${p[i + 2]},${p[i + 3]},${p[i + 4]})`; i += 4; }
        else if (p[i + 1] === 5) { col = c256(p[i + 2]); i += 2; }
        if (col) { if (c === 38) fg = col; else bg = col; }
      }
    }
  };
  while ((m = re.exec(text))) {
    buf += text.slice(last, m.index);
    last = re.lastIndex;
    if (m[1] !== undefined) { flush(); sgr(m[1]); }
  }
  buf += text.slice(last);
  flush();
  return html;
}

function page(body: string, wpx: number, hpx: number): string {
  return `<!doctype html><meta charset="utf-8"><style>
html,body{margin:0;background:#15131d}
pre{margin:0;box-sizing:border-box;width:${wpx}px;height:${hpx}px;padding:18px 22px;background:#15131d;color:#cfcbe3;
font:400 14px/17px 'JetBrains Mono',Menlo,Monaco,monospace;font-variant-ligatures:none;overflow:hidden}
</style><link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&display=swap" rel="stylesheet">
<pre>${body}</pre>`;
}

let url: string;
if (src.endsWith(".html")) {
  url = "file://" + resolve(src);
} else {
  const dir = mkdtempSync(join(tmpdir(), "doze-shot-"));
  const file = join(dir, "frame.html");
  writeFileSync(file, page(ansiToHtml(readFileSync(src, "utf8")), Number(w), Number(h)));
  url = "file://" + file;
}

const r = spawnSync(CHROME, [
  "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
  "--virtual-time-budget=4000", `--window-size=${w},${h}`, `--screenshot=${resolve(out)}`, url,
], { encoding: "utf8" });
if (r.status !== 0) {
  console.error(r.stderr);
  process.exit(1);
}
console.log(resolve(out));
