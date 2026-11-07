// gen-llms: writes website/public/llms.txt, the map of the doze docs for
// language models (https://llmstxt.org). The page list is the docs' own
// order, from doze-website's design/docs-ia.json; each line takes the page's
// title and description from its frontmatter here, so the file cannot drift
// from the pages. The summary at the top is written by hand, below.
//
//   bun website/scripts/gen-llms.ts            (make llms)
//
// DOCS_IA points at another docs-ia.json (default: ../doze-website/design/
// docs-ia.json beside this repo). --check writes nothing and exits 1 when the
// committed file differs.
import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dir, "../..");
const iaFile = path.resolve(process.env.DOCS_IA ?? path.join(root, "../doze-website/design/docs-ia.json"));
const docsDir = path.join(root, "website/src/content/docs");
const outFile = path.join(root, "website/public/llms.txt");
const base = "https://doze-dev.com/docs/doze/";

const summary = `# doze

> doze runs your whole local development stack as native processes on macOS (Apple silicon) and Linux, with no Docker and no VM. You declare the stack in one HCL file, doze.hcl. Each service starts on its first connection and sleeps when nothing has used it for a while (2 minutes by default). doze is a desktop app (one window for every stack, a menu bar icon) and a CLI; both are the same binary. It is for local development, not production.

Key facts:
- Install: the app with \`brew install --cask doze-dev/tap/doze\` on macOS; deb and rpm packages on Linux. The CLI is the same binary. Running \`doze\` opens the app; every app action is also a command.
- doze.hcl has four kinds of block: \`module <type> "<name>"\` (an engine from the doze registry, e.g. \`module postgres "db" { version = 18 }\`), \`app\` (your own code), \`service\` (any other long-running program, e.g. mailpit) and \`task\` (runs to completion, e.g. a migration). \`after = [task.migrate, module.db]\` orders them; references such as \`module.db.url\` wire them. Root fields: \`stack\`, \`doze\`; \`defaults { sleep = "2m"  names = true }\`.
- Modules come only from the doze registry (registry.doze-dev.com for doze 0.2): postgres, valkey, kvrocks, ferret (FerretDB, MongoDB wire protocol), mariadb, temporal, kafka and aws. \`aws\` is one module with S3, SQS, SNS, DynamoDB, Lambda, EventBridge, KMS, SSM, Secrets Manager, STS and Step Functions. Anything else runs as your own \`service\` (\`doze add <name> --as installed|container|download\`). New modules are contributed by pull request to github.com/doze-dev/doze-modules.
- Modules are signed; the registry key is compiled into doze. doze.lock pins engine binaries, modules and downloads; commit it. Pins move only with \`doze modules upgrade\`.
- Names: with \`names = true\` each service is \`<name>.<stack>.doze\` on an address of its own, so every Postgres can use 5432. A front door serves apps on :80 and :443. One-time machine setup: \`doze dns-setup\` (undo with \`--uninstall\`); \`doze trust\` for an app's own https \`domains\`.
- States: awake, dozing, waking, asleep, crashing, failed, stopped. \`sleep = "never"\` or \`doze pin\` keeps a service awake; \`doze off\` keeps it stopped.
- Commands: \`doze init\`, \`add\`, \`up\` (\`--last\`, \`--all\`), \`down\`, \`status\`, \`logs\`, \`wake\`, \`sleep\`, \`restart\`, \`reset\`, \`task\`, \`pin\`, \`on\`, \`off\`, \`url\`, \`env\`, \`run --env -- <cmd>\`, \`migrate\` (Postgres majors), \`lint\`, \`fmt\`, \`doctor\`, \`modules upgrade\`. \`--json\` gives one envelope; exit codes are stable.
- Upgrading from doze 0.1: \`process\` is now \`app\`/\`service\`/\`task\`, \`depends_on\` is \`after\`, \`idle_timeout\` is \`sleep\`, \`name\` is \`stack\`, engine blocks are \`module <type> "<name>"\`.
`;

type Page = { slug: string; title: string };
const ia: Record<string, Record<string, Page[]>> = JSON.parse(fs.readFileSync(iaFile, "utf8"));

// frontmatter reads title and description from a page's YAML frontmatter.
function frontmatter(slug: string): { title?: string; description?: string } | undefined {
  for (const ext of [".md", ".mdx"]) {
    const file = path.join(docsDir, slug + ext);
    if (!fs.existsSync(file)) continue;
    const m = fs.readFileSync(file, "utf8").match(/^---\n([\s\S]*?)\n---/);
    if (!m) return {};
    const field = (k: string) => {
      const f = m[1].match(new RegExp(`^${k}:\\s*(.*)$`, "m"));
      return f ? f[1].trim().replace(/^(["'])(.*)\1$/, "$2") : undefined;
    };
    return { title: field("title"), description: field("description") };
  }
  return undefined;
}

const missing: string[] = [];
let out = summary;
for (const [group, pages] of Object.entries(ia.doze)) {
  out += `\n## ${group}\n\n`;
  for (const p of pages) {
    const fm = frontmatter(p.slug);
    if (!fm) missing.push(p.slug);
    const title = fm?.title || p.title;
    const desc = fm?.description ? `: ${fm.description}` : "";
    out += `- [${title}](${base}${p.slug}/)${desc}\n`;
  }
}
out += `\n## Optional\n\n- [doze AWS docs](https://doze-dev.com/docs/aws/): the local AWS on its own\n- [doze Kafka docs](https://doze-dev.com/docs/kafka/): the local Kafka on its own\n- [The module registry](https://doze-dev.com/registry/): every module, its versions and fields\n`;

if (missing.length) console.error(`gen-llms: no page yet for ${missing.join(", ")} (listed with its working title)`);

if (process.argv.includes("--check")) {
  const now = fs.existsSync(outFile) ? fs.readFileSync(outFile, "utf8") : "";
  if (now !== out) {
    console.error("gen-llms: website/public/llms.txt is stale; run make llms");
    process.exit(1);
  }
  process.exit(0);
}
fs.writeFileSync(outFile, out);
console.log(`gen-llms: wrote ${path.relative(root, outFile)}`);
