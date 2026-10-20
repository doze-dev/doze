// Generic LSP bulk-extraction benchmark.
// usage: bun lspbench.ts <root> <ext,ext> <langId> <maxFiles> -- <server cmd...>
import { spawn } from "bun";
import { readdirSync, readFileSync, statSync } from "fs";
import { join } from "path";

const argv = process.argv.slice(2);
const sep = argv.indexOf("--");
const [root, exts, langId, maxFilesS] = argv.slice(0, sep);
const cmd = argv.slice(sep + 1);
const maxFiles = Number(maxFilesS);
const extList = exts.split(",");

function walk(d: string, out: string[]) {
  for (const n of readdirSync(d)) {
    if (n === "node_modules" || n.startsWith(".") || n === "target" || n === "dist") continue;
    const p = join(d, n);
    const s = statSync(p);
    if (s.isDirectory()) walk(p, out);
    else if (extList.some((e) => n.endsWith(e))) out.push(p);
  }
}
const files: string[] = [];
walk(root, files);
files.sort();
const sel = files.slice(0, maxFiles);

const t0 = performance.now();
const proc = spawn({ cmd, cwd: root, stdin: "pipe", stdout: "pipe", stderr: "ignore" });
let peakRss = 0;
const rssTimer = setInterval(async () => {
  const p = spawn({ cmd: ["ps", "-o", "rss=", "-g", String(proc.pid)], stdout: "pipe" });
  const txt = await new Response(p.stdout).text();
  // sum rss of the process group (servers that spawn children)
  const p2 = spawn({ cmd: ["sh", "-c", `ps -A -o pid=,ppid=,rss= | awk -v r=${proc.pid} '{pid[$1]=$2; rss[$1]=$3} END {s=0; for (p in pid){q=p; while(q!=""&&q!=0){ if(q==r){s+=rss[p];break}; q=pid[q]}} print s}'`], stdout: "pipe" });
  const t2 = await new Response(p2.stdout).text();
  const v = Number(t2.trim()) || Number(txt.trim()) || 0;
  if (v > peakRss) peakRss = v;
}, 200);

let id = 0;
const pending = new Map<number, (v: any) => void>();
let progressActive = new Set<string>();
let progressEnded = 0;
function send(msg: any) {
  const body = JSON.stringify({ jsonrpc: "2.0", ...msg });
  proc.stdin.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
  proc.stdin.flush();
}
function request(method: string, params: any, timeoutMs = 60000): Promise<any> {
  const myId = ++id;
  send({ id: myId, method, params });
  return new Promise((res) => {
    const t = setTimeout(() => { pending.delete(myId); res({ __timeout: true }); }, timeoutMs);
    pending.set(myId, (v) => { clearTimeout(t); res(v); });
  });
}
(async () => {
  let buf = Buffer.alloc(0);
  for await (const chunk of proc.stdout) {
    buf = Buffer.concat([buf, Buffer.from(chunk)]);
    while (true) {
      const h = buf.indexOf("\r\n\r\n");
      if (h < 0) break;
      const m = /Content-Length: (\d+)/i.exec(buf.subarray(0, h).toString());
      const len = Number(m![1]);
      if (buf.length < h + 4 + len) break;
      const msg = JSON.parse(buf.subarray(h + 4, h + 4 + len).toString());
      buf = buf.subarray(h + 4 + len);
      if (msg.id !== undefined && msg.method) {
        // server->client request: answer generically
        let result: any = null;
        if (msg.method === "workspace/configuration") result = (msg.params.items || []).map(() => null);
        send({ id: msg.id, result });
        continue;
      }
      if (msg.method === "$/progress") {
        const k = msg.params?.value?.kind;
        if (k === "begin") progressActive.add(String(msg.params.token));
        if (k === "end") { progressActive.delete(String(msg.params.token)); progressEnded++; }
        continue;
      }
      if (msg.id !== undefined && pending.has(msg.id)) { pending.get(msg.id)!(msg.error ? { __error: msg.error } : msg.result); pending.delete(msg.id); }
    }
  }
})();

const uri = (p: string) => "file://" + p;
const init = await request("initialize", {
  processId: process.pid, rootUri: uri(root), rootPath: root,
  workspaceFolders: [{ uri: uri(root), name: "root" }],
  capabilities: {
    window: { workDoneProgress: true },
    workspace: { configuration: true, workspaceFolders: true, symbol: {} },
    textDocument: { documentSymbol: { hierarchicalDocumentSymbolSupport: true }, references: {}, definition: { linkSupport: false }, callHierarchy: {} },
  },
}, 120000);
send({ method: "initialized", params: {} });
const tInit = performance.now();
const caps = init?.capabilities || {};

let docSymTotal = 0, refCalls = 0, refLocs = 0, refTimeouts = 0, refErrors = 0;
let firstRefMs = -1;
const refLat: number[] = [];
type Sym = { line: number; character: number; name: string; kind: number };
function flatten(syms: any[], out: Sym[]) {
  for (const s of syms || []) {
    const r = s.selectionRange || s.location?.range;
    if (r && [5, 6, 9, 10, 11, 12, 23, 26].includes(s.kind)) out.push({ line: r.start.line, character: r.start.character, name: s.name, kind: s.kind });
    if (s.children) flatten(s.children, out);
  }
}
const allSyms: { file: string; s: Sym }[] = [];
for (const f of sel) {
  send({ method: "textDocument/didOpen", params: { textDocument: { uri: uri(f), languageId: langId, version: 1, text: readFileSync(f, "utf8") } } });
}
let tReady = performance.now();
if (process.env.WAIT_PROGRESS) {
  // wait until at least one progress ended and none active for 3s (or 600s cap)
  let quietSince = performance.now();
  while (performance.now() - tInit < 600000) {
    await Bun.sleep(200);
    if (progressActive.size > 0) quietSince = performance.now();
    if (progressEnded > 0 && progressActive.size === 0 && performance.now() - quietSince > 3000) break;
  }
  tReady = performance.now();
  console.error("ready after ms", Math.round(tReady - tInit), "progress ended", progressEnded);
}
const tOpen = performance.now();
for (const f of sel) {
  const ds = await request("textDocument/documentSymbol", { textDocument: { uri: uri(f) } }, 120000);
  const out: Sym[] = [];
  if (Array.isArray(ds)) flatten(ds, out);
  docSymTotal += out.length;
  for (const s of out) allSyms.push({ file: f, s });
}
const tSym = performance.now();
const maxRefs = Number(process.env.MAXREFS || 400);
const refTargets = allSyms.slice(0, maxRefs);
for (const { file, s } of refTargets) {
  const a = performance.now();
  const r = await request("textDocument/references", { textDocument: { uri: uri(file) }, position: { line: s.line, character: s.character }, context: { includeDeclaration: false } }, 60000);
  const ms = performance.now() - a;
  if (firstRefMs < 0) firstRefMs = ms;
  refLat.push(ms);
  refCalls++;
  if (r?.__timeout) refTimeouts++; else if (r?.__error) refErrors++; else if (Array.isArray(r)) { refLocs += r.length; if (process.env.DUMP) console.error(s.name + "\t" + r.length + "\t" + new Set(r.map((x: any) => x.uri)).size); }
}
const tRefs = performance.now();
// targeted definition calls at symbol positions in other files (cheap check)
const defLat: number[] = [];
for (const { file, s } of refTargets.slice(0, 100)) {
  const a = performance.now();
  await request("textDocument/definition", { textDocument: { uri: uri(file) }, position: { line: s.line, character: s.character } }, 30000);
  defLat.push(performance.now() - a);
}
// call hierarchy on first 50 functions/methods
let chOk = 0, chEdges = 0;
const chLat: number[] = [];
if (caps.callHierarchyProvider) {
  for (const { file, s } of refTargets.filter((x) => [6, 12].includes(x.s.kind)).slice(0, 50)) {
    const a = performance.now();
    const items = await request("textDocument/prepareCallHierarchy", { textDocument: { uri: uri(file) }, position: { line: s.line, character: s.character } }, 30000);
    if (process.env.DUMPCH) console.error("CH", s.name, s.kind, JSON.stringify(items).slice(0,150));
    if (Array.isArray(items) && items.length) {
      const out = await request("callHierarchy/outgoingCalls", { item: items[0] }, 30000);
      if (Array.isArray(out)) { chOk++; chEdges += out.length; }
    }
    chLat.push(performance.now() - a);
  }
}
const pct = (a: number[], p: number) => { if (!a.length) return 0; const b = [...a].sort((x, y) => x - y); return Math.round(b[Math.min(b.length - 1, Math.floor(p * b.length))]); };
clearInterval(rssTimer);
const res = {
  server: cmd.join(" "), root, files: sel.length, initMs: Math.round(tInit - t0),
  openMs: Math.round(tOpen - tInit), docSymMs: Math.round(tSym - tOpen), docSyms: docSymTotal,
  refCalls, refLocs, refTimeouts, refErrors, refsTotalMs: Math.round(tRefs - tSym), firstRefMs: Math.round(firstRefMs),
  refP50: pct(refLat, 0.5), refP95: pct(refLat, 0.95), defP50: pct(defLat, 0.5), defP95: pct(defLat, 0.95),
  callHierarchy: !!caps.callHierarchyProvider, chOk, chEdges, chP50: pct(chLat, 0.5),
  workspaceSymbol: !!caps.workspaceSymbolProvider, peakRssMB: Math.round(peakRss / 1024), progressEnded,
};
console.log(JSON.stringify(res));
await request("shutdown", null, 5000);
send({ method: "exit" });
setTimeout(() => { proc.kill(); process.exit(0); }, 500);
