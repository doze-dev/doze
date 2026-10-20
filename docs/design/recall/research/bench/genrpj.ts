// Crude rust-project.json generator from Cargo.toml path deps (no cargo). Usage: bun genrpj.ts <repo>
import { readFileSync, existsSync, writeFileSync, readdirSync } from "fs";
import { join, resolve, dirname } from "path";
const repo = resolve(process.argv[2]);
type C = { dir: string; name: string; root: string; deps: string[] };
const crates = new Map<string, C>();
function load(dir: string) {
  const t = readFileSync(join(dir, "Cargo.toml"), "utf8");
  const pkg = /\[package\][^\[]*?name\s*=\s*"([^"]+)"/s.exec(t);
  if (!pkg) return;
  const name = pkg[1];
  if ([...crates.values()].some((c) => c.dir === dir)) return;
  let root = ["src/lib.rs", "src/main.rs"].map((p) => join(dir, p)).find(existsSync);
  const bin = /\[\[bin\]\][^\[]*?path\s*=\s*"([^"]+)"/s.exec(t);
  if (!root && bin) root = join(dir, bin[1]);
  const lib = /\[lib\][^\[]*?name\s*=\s*"([^"]+)"/s.exec(t);
  const deps: string[] = [];
  // only [dependencies] section path deps
  const depSec = /\n\[dependencies\]\n([\s\S]*?)(\n\[|$)/.exec(t);
  for (const m of (depSec?.[1] || "").matchAll(/^([\w-]+)\s*=\s*\{[^}]*path\s*=\s*"([^"]+)"/gm)) {
    deps.push(resolve(dir, m[2]));
  }
  crates.set(dir, { dir, name: (lib?.[1] || name).replace(/-/g, "_"), root: root!, deps });
  for (const d of deps) load(d);
}
load(repo);
for (const d of existsSync(join(repo, "crates")) ? readdirSync(join(repo, "crates")) : []) if (existsSync(join(repo, "crates", d, "Cargo.toml"))) load(join(repo, "crates", d));
const list = [...crates.values()].filter((c) => c.root);
const idx = new Map(list.map((c, i) => [c.dir, i]));
const out = {
  crates: list.map((c) => ({
    root_module: c.root, edition: "2021", is_workspace_member: true,
    deps: c.deps.filter((d) => idx.has(d)).map((d) => ({ crate: idx.get(d), name: list[idx.get(d)!].name })),
    cfg: [], is_proc_macro: false,
  })),
};
writeFileSync(join(repo, "rust-project.json"), JSON.stringify(out, null, 1));
console.log("crates", list.length);
