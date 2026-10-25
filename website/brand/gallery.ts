// gallery.html: every mark and icon, large and small, on light and dark.
const marks = ["doze", "doze-kafka", "doze-aws"];
const inline = async (n: string) => `data:image/svg+xml;base64,${Buffer.from(await Bun.file(`${n}.svg`).text()).toString("base64")}`;
let html = `<!doctype html><meta charset="utf-8"><title>doze logos</title><style>
body{font:15px system-ui;margin:0;background:#f4f2ee;color:#222}
h2{font-weight:600;margin:32px 32px 8px}
.grid{display:flex;flex-wrap:wrap;gap:24px;padding:0 32px}
.card{background:#fff;border-radius:16px;padding:24px;display:flex;flex-direction:column;align-items:center;gap:16px}
.card.dark{background:#141414;color:#eee}
.sizes{display:flex;align-items:end;gap:18px}
.label{font-weight:600}
</style>`;
for (const [title, suffix] of [["Icons", ""]]) {
  for (const dark of [false, true]) {
    html += `<h2>${title} on ${dark ? "dark" : "light"}</h2><div class="grid">`;
    for (const m of marks) {
      const src = await inline(m + suffix);
      html += `<div class="card${dark ? " dark" : ""}"><div class="label">${m}</div><img src="${src}" width="280" height="280"><div class="sizes">${[64, 32, 16].map((s) => `<img src="${src}" width="${s}" height="${s}">`).join("")}</div></div>`;
    }
    html += `</div>`;
  }
}
await Bun.write("gallery.html", html + `<div style="height:40px"></div>`);
