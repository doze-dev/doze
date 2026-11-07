// ground: every doze command, flag and doze.hcl field the docs name must
// exist. It reads website/src/content/docs/**/*.md(x) and checks
//
//   - each `doze <command> [--flags]`, in inline code and in shell code
//     blocks, against the real CLI: `doze <command> --help`, walked
//     command by command (aliases and nested commands included);
//   - each field and block in ```hcl blocks against the editor schema,
//     public/schema/doze-hcl.json (generated from internal/config), and,
//     inside `module <engine> "…" { }`, against that engine's generated
//     page (services/<engine>.md), which lists the module's own fields;
//   - that every fenced code block says its language.
//
// A deliberate mistake is kept by writing <!-- ground:skip --> (or, in MDX,
// {/* ground:skip */}) on the line before it, or before the opening fence
// of a whole block. A page that quotes old commands on purpose, such as an
// upgrade guide, says `ground: historical` in its frontmatter.
//
// bun website/scripts/ground.ts [docs-dir]. DOZE_BIN is the doze to ask
// (default ./doze, from make build). It prints file:line and what is wrong,
// then counts by kind, and exits 1 on any finding, unless
// GROUND_REPORT_ONLY=1 (CI, until the docs rewrite lands).
import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dir, "../..");
const docsDir = path.resolve(process.argv[2] ?? path.join(root, "website/src/content/docs"));
const bin = path.resolve(root, process.env.DOZE_BIN ?? "doze");
const schemaFile = path.join(root, "website/public/schema/doze-hcl.json");
const enginesDir = path.join(docsDir, "services");
// a module whose page is not named after it
const enginePage: Record<string, string> = { ferret: "ferretdb" };

type Finding = { file: string; line: number; kind: string; what: string };
const findings: Finding[] = [];
const say = (file: string, line: number, kind: string, what: string) => findings.push({ file: path.relative(root, file), line, kind, what });

// ── the CLI, as its help describes it ──────────────────────────────────

type Cmd = { path: string[]; subs: Map<string, string>; flags: Set<string>; valued: Set<string>; args: boolean; passthrough: boolean };
const cmds = new Map<string, Cmd>();
const rootHelp = help([]);

function help(argv: string[]) {
  const r = Bun.spawnSync([bin, ...argv, "--help"], { env: { ...process.env, NO_COLOR: "1" } });
  if (r.exitCode !== 0 && argv.length === 0) {
    console.error(`ground: cannot run ${bin} --help (make build first, or set DOZE_BIN)`);
    process.exit(2);
  }
  return r.stdout.toString();
}

// command is the help of doze <argv>, parsed: its subcommands (with their
// aliases), its flags (with the global ones), and whether it takes
// arguments. Undefined when the help is not that command's.
function command(argv: string[]): Cmd | undefined {
  const key = argv.join(" ");
  if (cmds.has(key)) return cmds.get(key);
  const text = argv.length ? help(argv) : rootHelp;
  const usage = [...text.matchAll(/^Usage:\n((?:  .*\n)+)/gm)].flatMap((m) => m[1].trim().split("\n").map((l) => l.trim()));
  const self = ["doze", ...argv].join(" ");
  if (!usage.some((u) => u === self || u.startsWith(self + " "))) return undefined;
  const subs = new Map<string, string>();
  const flags = new Set<string>();
  const valued = new Set<string>(); // flags that take the next word: --config string
  let section = "";
  for (const line of text.split("\n")) {
    const head = line.match(/^(\S.*):$/);
    if (head) {
      section = head[1];
      continue;
    }
    if (!line.startsWith("  ")) continue;
    if (/^(Global )?Flags$/.test(section)) {
      const m = line.match(/^\s+(?:-(\w), )?--([\w-]+)( \S+)?/) ?? line.match(/^\s+-(\w)\b/);
      if (m) {
        const names = [m[1] ? "-" + m[1] : "", m[2] ? "--" + m[2] : ""].filter(Boolean);
        for (const f of names) {
          flags.add(f);
          if (m[3]) valued.add(f);
        }
      }
    } else if (section === "Aliases") {
      // the first is the command's own name; the rest answer for it
    } else if (!/^(Usage|Examples?|Flags|Global Flags|Additional help topics)$/.test(section)) {
      const m = line.match(/^  (\S+)\s{2,}/);
      if (m) subs.set(m[1], m[1]);
    }
  }
  for (const [name] of [...subs]) {
    const sub = help([...argv, name]).match(/^Aliases:\n\s+(.*)$/m);
    for (const a of sub?.[1].split(",").map((s) => s.trim()).slice(1) ?? []) subs.set(a, name);
  }
  const rest = usage.map((u) => u.slice(self.length).replace(/\[flags\]|\[command\]/g, "").trim());
  const cmd: Cmd = {
    path: argv,
    subs,
    flags,
    valued,
    args: rest.some((r) => r !== ""),
    passthrough: usage.some((u) => u.includes(" -- ")),
  };
  cmds.set(key, cmd);
  return cmd;
}

// checkCommand checks the words after `doze` on one line.
function checkCommand(file: string, line: number, words: string[]) {
  let at = command([])!;
  let positional = false;
  for (let i = 0; i < words.length; i++) {
    const w = words[i];
    if (w === "--") return;
    if (w.startsWith("-") && w.length > 1 && !/^-\d/.test(w)) {
      const name = w.split("=")[0];
      const known = name.startsWith("--") ? at.flags.has(name) : [...name.slice(1)].every((c) => at.flags.has("-" + c));
      if (!known) say(file, line, "unknown-flag", `doze ${[...at.path, name].join(" ")}: no flag ${name}`);
      if (at.valued.has(name) && !w.includes("=")) i++; // its value
      continue;
    }
    if (positional && at.passthrough) return; // doze run -- <command>: the rest is the command's
    if (/^[<[{$"'`(…]|\.\.\.|…/.test(w) || positional) {
      positional = true;
      continue;
    }
    const sub = at.subs.get(w);
    if (sub) {
      const next = command([...at.path, sub]);
      if (next) {
        at = next;
        continue;
      }
    }
    if (at.args) {
      positional = true;
      continue;
    }
    const hidden = command([...at.path, w]);
    say(file, line, hidden ? "hidden-command" : "unknown-command", `doze ${[...at.path, w].join(" ")}` + (hidden ? " is hidden from doze --help" : ": no such command"));
    return;
  }
}

// commandsIn finds `doze …` where a command starts in one line of shell (at
// the start, after ; && || | ( $( or a backquote, or after sudo, exec, time),
// and the words after it, up to the end of that command. "the doze daemon"
// in a diagram is not a command.
function commandsIn(text: string): string[][] {
  const out: string[][] = [];
  for (const m of text.matchAll(/(?:^\s*|[;&|(`]\s*|\b(?:sudo|exec|time|then|do|nohup|xargs)\s+)doze((?:\s+[^\s;&|)`>]+)*)/g)) {
    const words = m[1].trim().split(/\s+/).filter(Boolean);
    // a sentence in a comment, not a command: "doze starts it"
    if (words.length && /^[a-z][\w-]*$/.test(words[0]) === false && !words[0].startsWith("-") && !/^[<[]/.test(words[0])) continue;
    out.push(words);
  }
  return out;
}

// ── doze.hcl, as the schema and the engine pages describe it ───────────

type Block = { attributes: Record<string, unknown>; block_types?: Record<string, { block: Block }>; open?: boolean };
const schema: Block = JSON.parse(fs.readFileSync(schemaFile, "utf8")).block;

// engine is a module's own vocabulary, from its generated page: the field
// table, then a table per nested block.
type Engine = { fields: Set<string>; blocks: Map<string, Set<string>> };
const engines = new Map<string, Engine | null>();
// unlisted are the engines whose page lists no nested blocks (the committed
// snapshot may predate them): their blocks are not checked, and the report
// says so.
const unlisted = new Set<string>();
function engine(name: string): Engine | null {
  if (engines.has(name)) return engines.get(name)!;
  const file = path.join(enginesDir, (enginePage[name] ?? name) + ".md");
  if (!fs.existsSync(file)) {
    engines.set(name, null);
    return null;
  }
  const e: Engine = { fields: new Set(), blocks: new Map() };
  let into = e.fields;
  for (const line of fs.readFileSync(file, "utf8").split("\n")) {
    const b = line.match(/^\*\*`([\w-]+)(?: "<\w+>")? \{ … \}`\*\*/);
    if (b) {
      e.blocks.set(b[1], (into = new Set()));
      continue;
    }
    const f = line.match(/^\| `([\w-]+)` \|/);
    if (f) into.add(f[1]);
  }
  engines.set(name, e);
  return e;
}

type Frame = { kind: "schema"; block: Block; name: string } | { kind: "engine"; e: Engine; name: string } | { kind: "fields"; fields: Set<string>; name: string } | { kind: "open"; name: string };

function checkHCL(file: string, start: number, lines: string[], skip: Set<number>) {
  const stack: Frame[] = [{ kind: "schema", block: schema, name: "doze.hcl" }];
  let value = 0; // depth of an open value: a map or list spread over lines
  let heredoc = "";
  lines.forEach((raw, i) => {
    const n = start + i;
    if (heredoc) {
      if (raw.trim() === heredoc) heredoc = "";
      return;
    }
    const hd = raw.match(/<<-?(\w+)\s*$/);
    // strings out, then comments, so a # in a string is not a comment
    const code = raw.replace(/"(?:[^"\\]|\\.)*"/g, '""').replace(/(#|\/\/).*$/, "").trim();
    const depth = (s: string) => (s.match(/[{[(]/g)?.length ?? 0) - (s.match(/[}\])]/g)?.length ?? 0);
    if (value > 0) {
      value += depth(code);
      if (hd) heredoc = hd[1];
      return;
    }
    if (hd) heredoc = hd[1];
    if (!code || /^(\.\.\.|…)/.test(code)) return;
    if (code.startsWith("}")) {
      if (stack.length > 1) stack.pop();
      return;
    }
    const top = stack[stack.length - 1];
    const attr = code.match(/^([\w-]+)\s*=(?!=)\s*(.*)$/);
    if (attr) {
      if (!skip.has(n)) checkField(file, n, top, attr[1], "field");
      value = Math.max(0, depth(attr[2]));
      return;
    }
    const blk = code.match(/^([\w-]+)((?:\s+(?:""|[\w-]+))*)\s*\{(.*)$/);
    if (!blk) return;
    const [, name, labelText, after] = blk;
    const labels = labelText.trim().split(/\s+/).filter(Boolean);
    let next: Frame = { kind: "open", name };
    if (top.kind === "schema" && top.name === "doze.hcl" && name === "module") {
      const type = labels[0];
      const e = type && type !== '""' ? engine(type) : null;
      if (!skip.has(n) && type && !e && fs.existsSync(enginesDir)) say(file, n, "unknown-module-type", `module ${type}: no engine page services/${enginePage[type] ?? type}.md`);
      next = e ? { kind: "engine", e, name: `module ${type}` } : { kind: "open", name };
    } else if (top.kind === "schema") {
      const bt = top.block.block_types?.[name];
      if (bt) next = { kind: "schema", block: bt.block, name };
      else if (!top.block.open && !skip.has(n)) say(file, n, "unknown-block", `${top.name}: no block ${name}`);
      if (name === "locals") next = { kind: "open", name };
    } else if (top.kind === "engine") {
      const fields = top.e.blocks.get(name);
      // a block the module names without listing its fields is not checked
      if (fields) next = fields.size ? { kind: "fields", fields, name: `${top.name} ${name}` } : { kind: "open", name };
      else if (top.e.fields.has(name)) next = { kind: "open", name };
      else if (top.e.blocks.size === 0) unlisted.add(top.name.replace("module ", ""));
      else if (!skip.has(n) && !(schema.block_types?.module.block.attributes ?? {})[name]) say(file, n, "unknown-block", `${top.name}: no block ${name}`);
    }
    // a block opened and closed on one line: `module valkey "cache" { version = 9 }`
    const inline = after.replace(/\}\s*$/, "");
    if (/\}\s*$/.test(after)) {
      for (const a of inline.matchAll(/([\w-]+)\s*=(?!=)/g)) if (!skip.has(n)) checkField(file, n, next, a[1], "field");
      return;
    }
    stack.push(next);
  });
}

function checkField(file: string, line: number, at: Frame, name: string, _kind: string) {
  if (at.kind === "open") return;
  if (at.kind === "schema") {
    if (at.block.open || name in at.block.attributes) return;
    if (at.name === "doze.hcl" || !(at.block.block_types?.[name])) say(file, line, "unknown-field", `${at.name}: no field ${name}`);
    return;
  }
  if (at.kind === "engine") {
    if (at.e.fields.has(name) || name in (schema.block_types?.module.block.attributes ?? {})) return;
    say(file, line, "unknown-field", `${at.name}: no field ${name}`);
    return;
  }
  if (!at.fields.has(name)) say(file, line, "unknown-field", `${at.name}: no field ${name}`);
}

// ── the docs ───────────────────────────────────────────────────────────

const shells = new Set(["", "sh", "bash", "shell", "zsh", "console", "terminal", "fish", "text", "txt"]);
const skipMark = /<!--\s*ground:skip\s*-->|\{\/\*\s*ground:skip\s*\*\/\}/;

function walk(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((d) => (d.isDirectory() ? walk(path.join(dir, d.name)) : /\.mdx?$/.test(d.name) ? [path.join(dir, d.name)] : []));
}

for (const file of walk(docsDir).sort()) {
  const lines = fs.readFileSync(file, "utf8").split("\n");
  const front = lines[0] === "---" ? lines.slice(1, lines.indexOf("---", 1)).join("\n") : "";
  if (/^ground:\s*historical\s*$/m.test(front)) continue;
  let fence: { mark: string; lang: string; start: number; skip: boolean } | null = null;
  let body: string[] = [];
  const skipped = new Set<number>();
  for (let i = 0; i < lines.length; i++) {
    const n = i + 1;
    const line = lines[i];
    const skipNext = i > 0 && skipMark.test(lines[i - 1]);
    if (skipNext) skipped.add(n);
    const f = line.match(/^\s*(`{3,}|~{3,})\s*([\w+-]*)/);
    if (!fence && f) {
      fence = { mark: f[1], lang: f[2].toLowerCase(), start: n, skip: skipNext };
      body = [];
      if (!f[2] && !skipNext) say(file, n, "bare-block", "a code block with no language");
      continue;
    }
    if (fence && line.trim().startsWith(fence.mark) && line.trim().replace(/[`~]/g, "") === "") {
      if (!fence.skip) {
        if (fence.lang === "hcl") checkHCL(file, fence.start + 1, body, skipped);
        else if (shells.has(fence.lang)) {
          body.forEach((l, j) => {
            const ln = fence!.start + 1 + j;
            if (skipped.has(ln)) return;
            if (fence!.lang === "console" && !/^\s*[$#>]\s/.test(l)) return;
            const shell = l.replace(/^\s*[$>]\s+/, "").replace(/(^|\s)#.*$/, "");
            for (const words of commandsIn(shell)) checkCommand(file, ln, words);
          });
        }
      }
      fence = null;
      continue;
    }
    if (fence) {
      body.push(line);
      continue;
    }
    if (skipNext || n === 1) continue;
    for (const m of line.matchAll(/(`+)(doze(?:\s[^`]*)?)\1/g)) {
      for (const words of commandsIn(m[2])) checkCommand(file, n, words);
    }
  }
}

// ── the report ─────────────────────────────────────────────────────────

findings.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line);
for (const f of findings) console.log(`${f.file}:${f.line}: ${f.kind}: ${f.what}`);
const counts = new Map<string, number>();
for (const f of findings) counts.set(f.kind, (counts.get(f.kind) ?? 0) + 1);
console.log(`\n${findings.length} finding${findings.length === 1 ? "" : "s"} in ${new Set(findings.map((f) => f.file)).size} files` + (counts.size ? ": " + [...counts].sort((a, b) => b[1] - a[1]).map(([k, n]) => `${n} ${k}`).join(", ") : ""));
if (unlisted.size) console.log(`not checked: the nested blocks of ` + [...unlisted].sort().join(", ") + ` (their engine pages list none; regenerate them with gen-engines.mjs)`);
if (findings.length && process.env.GROUND_REPORT_ONLY !== "1") process.exit(1);
