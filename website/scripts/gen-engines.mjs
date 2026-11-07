// gen-engines — one docs page per registry module, under services/: the
// hand-written part (what it is, how to use it, recipes) from
// src/content/recipes/<engine>.md, then a reference generated from the signed
// registry's own data (catalog + index.yaml + meta.yaml): the versions you can
// declare, every field and nested block, and the module's releases. One page
// per engine, and the reference cannot drift from the module.
//
// Runs before `astro build` (see package.json). On a fetch failure it keeps
// the committed reference and refreshes only the hand-written part, so an
// edited recipe still lands and the build does not die because the registry
// hiccuped.
//
// The page's frontmatter is the recipe's (title, description), plus
// redirect_from: the old engines/<engine>/ address and whatever the recipe
// lists (the old recipe pages it absorbed).
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { parse, stringify } from 'yaml';

const BASE = process.env.DOZE_REGISTRY_BASE || 'https://doze.nerdmenot.in/registry';
// Where readers are sent: the registry's pages on the site, and its signed
// files on the registry host.
const SITE = 'https://doze-dev.com/registry';
const RAW = 'https://registry.doze-dev.com';
const OUT = new URL('../src/content/docs/services/', import.meta.url).pathname;
const RECIPES = new URL('../src/content/recipes/', import.meta.url).pathname;

// Display order, and the page each module gets when its slug is not its name.
const ORDER = ['postgres', 'valkey', 'kvrocks', 'ferret', 'mariadb', 'temporal', 'kafka', 'aws'];
const SLUG = { ferret: 'ferretdb' };
const slugOf = (name) => SLUG[name] || name;

// Corrections to a module's description of itself, until the module is
// fixed: postgres's Describe() gives grant a label "role", but role is a
// required field and grant takes no label (modules/postgres/config.go).
const LABEL_IS_FIELD = { 'postgres.grant': { name: 'role', type: 'string', required: true, desc: 'The role the privileges go to.' } };

// The example is the module's own, and may use fields its description does
// not list yet (aws queue content_dedup), so ground.ts skips it.
const MARK = { start: '<!-- recipe:start -->', end: '<!-- recipe:end -->' };

const platformLabel = (t) =>
	t
		.replace('aarch64-apple-darwin', 'macOS (Apple Silicon)')
		.replace('x86_64-apple-darwin', 'macOS (Intel)')
		.replace('aarch64-unknown-linux-gnu', 'Linux (arm64)')
		.replace('x86_64-unknown-linux-gnu', 'Linux (x86_64)');

const whereLine = (platforms) => {
	const mac = platforms.some((t) => t.includes('apple-darwin'));
	const linux = platforms.some((t) => t.includes('linux'));
	if (mac && linux) return 'macOS and Linux';
	if (!mac && platforms.length === 1) return platformLabel(platforms[0]) + ' only';
	return mac ? 'macOS only' : 'Linux only';
};

const esc = (s) => String(s ?? '').replaceAll('|', '\\|').replaceAll('\n', ' ');

// A module describes its block the way it decodes it: `postgres "db" { … }`.
// In a doze.hcl the same block is written `module postgres "db" { … }`: the
// keyword is doze's, not the module's, so the example gains it here.
const asModuleBlock = (name, example) =>
	oneArgPerLine(
		String(example ?? '')
			.trim()
			.replace(new RegExp(`^(#\\s*)?${name}(\\s+")`, 'gm'), `$1module ${name}$2`),
	);

// oneArgPerLine opens up a one-line block with several arguments,
// `queue "q" { fifo = true  dlq = "auto" }`, which HCL refuses (a one-line
// block takes one argument). Some modules' examples have them.
const oneArgPerLine = (src) =>
	src.replace(/^(\s*)([\w-]+(?:\s+"[^"]*")*)\s*\{\s*(.+?)\s*\}[ \t]*$/gm, (line, ind, head, inner) => {
		const args = inner.split(/\s+(?=[\w-]+\s*=(?!=))/);
		if (args.length < 2) return line;
		return `${ind}${head} {\n${args.map((a) => `${ind}  ${a.trim()}`).join('\n')}\n${ind}}`;
	});

function fieldTable(args) {
	if (!args.length) return '';
	let out = '| Field | Type | Default | Description |\n|---|---|---|---|\n';
	for (const a of args) {
		const def = a.default ? `\`${a.default}\`` : '—';
		const req = a.required ? ' **Required.**' : '';
		out += `| \`${a.name}\` | ${a.type || ''} | ${def} | ${esc(a.desc)}${req} |\n`;
	}
	return out;
}

// nestedBlocks lists the blocks a module takes inside its own. Modules say
// so two ways: config.blocks, with a label and fields of their own, or an
// argument of type "block" with only a description. The heading form,
// **`role "<name>" { … }`**, is what website/scripts/ground.ts reads.
function nestedBlocks(name, cfg) {
	const blocks = [];
	for (const b of cfg.blocks || []) {
		let args = b.arguments || [];
		let label = b.label;
		const fix = LABEL_IS_FIELD[`${name}.${b.name}`];
		if (fix) {
			label = undefined;
			args = [fix, ...args];
		} else if (label === undefined) {
			label = 'name'; // aws lists its blocks without one; every one is labelled
		}
		blocks.push({ name: b.name, label, desc: b.desc, args, listed: true });
	}
	for (const a of (cfg.arguments || []).filter((a) => a.type === 'block')) {
		const label = /label = name/.test(a.desc || '') ? 'name' : undefined;
		blocks.push({ name: a.name, label, desc: a.desc, args: [], listed: false });
	}
	if (!blocks.length) return '';
	let out = '\n### Nested blocks\n';
	for (const b of blocks) {
		const head = b.label ? `${b.name} "<${b.label}>" { … }` : `${b.name} { … }`;
		out += `\n**\`${head}\`** — ${esc(b.desc)}\n\n`;
		if (b.args.length) out += fieldTable(b.args);
		else out += b.listed ? '_No fields: the label is the declaration._\n' : "_The module does not list this block's fields yet._\n";
	}
	return out;
}

// backendVersions resolves what a user can actually DECLARE, from
// doze-binaries' published index: every series (the `version =` you write;
// a bare series pins its newest build) with its exact builds, filtered to the
// majors the current module release supports. null = no fetched backend
// (kafka's version is a protocol profile; aws takes none) or lookup failure.
const BINARIES = { postgres: 'postgres', valkey: 'valkey', kvrocks: 'kvrocks', mariadb: 'mariadb', temporal: 'temporal', ferret: 'ferretdb' };
const numDesc = (a, b) => b.localeCompare(a, undefined, { numeric: true });
async function backendVersions(name, supportedMajors) {
	const recipe = BINARIES[name];
	if (!recipe) return null;
	try {
		const res = await fetch(`https://github.com/doze-dev/doze-binaries/releases/download/${recipe}/index.yaml`, { redirect: 'follow' });
		if (!res.ok) throw new Error(`HTTP ${res.status}`);
		const eng = parse(await res.text())?.engines?.[recipe];
		const fulls = Object.keys(eng?.artifacts || {});
		const all = Object.entries(eng?.versions || {}).map(([ser, latest]) => ({
			series: ser,
			latest,
			supported: supportedMajors.some((m) => ser === m || ser.startsWith(m + '.')),
			fulls: fulls.filter((f) => f === ser || f.startsWith(ser + '.')).sort(numDesc),
		}));
		const series = all.filter((x) => x.supported).sort((a, b) => numDesc(b.series, a.series));
		const pending = all.filter((x) => !x.supported).map((x) => x.series).sort((a, b) => numDesc(b, a));
		return series.length ? { series, pending } : null;
	} catch (e) {
		console.warn(`  ⚠ ${name}: backend version lookup failed (${e.message})`);
		return null;
	}
}

async function get(path) {
	const res = await fetch(`${BASE}/${path}`, { redirect: 'follow' });
	if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
	return res.text();
}

// recipe reads the hand-written part: its frontmatter and its body.
function recipe(name) {
	const file = `${RECIPES}${slugOf(name)}.md`;
	if (!existsSync(file)) return { front: {}, body: '' };
	const text = readFileSync(file, 'utf8');
	const m = text.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
	return m ? { front: parse(m[1]) || {}, body: m[2].trim() } : { front: {}, body: text.trim() };
}

function frontmatter(name, fallbackTitle, fallbackDesc) {
	const { front } = recipe(name);
	const redirects = [...new Set([`/engines/${name}/`, ...(front.redirect_from || [])])];
	const fm = { title: front.title || fallbackTitle, description: front.description || fallbackDesc || '', redirect_from: redirects };
	if (front.sidebar) fm.sidebar = front.sidebar;
	return `---\n${stringify(fm, { lineWidth: 0 }).trim()}\n---\n`;
}

const recipeBlock = (name) => `${MARK.start}\n\n${recipe(name).body}\n\n${MARK.end}`;

// refresh rewrites the hand-written part of an already generated page.
function refresh(name) {
	const file = `${OUT}${slugOf(name)}.md`;
	if (!existsSync(file)) return false;
	const text = readFileSync(file, 'utf8');
	const a = text.indexOf(MARK.start);
	const b = text.indexOf(MARK.end);
	if (a < 0 || b < 0) return false;
	const old = text.match(/^---\n[\s\S]*?\n---\n/)?.[0] ?? '';
	const oldFront = parse(old.replace(/^---\n|---\n$/g, '')) || {};
	const head = frontmatter(name, oldFront.title, oldFront.description);
	writeFileSync(file, head + text.slice(old.length, a) + recipeBlock(name) + text.slice(b + MARK.end.length));
	return true;
}

async function generate() {
	const catalog = JSON.parse(await get('index.json'));
	const mods = catalog.namespaces?.doze?.modules ?? {};
	mkdirSync(OUT, { recursive: true });

	let n = 0;
	for (const name of ORDER) {
		const cat = mods[name];
		if (!cat) {
			console.warn(`  ⚠ ${name}: not in the catalog; skipping`);
			continue;
		}
		const meta = parse(await get(`doze/${name}/meta.yaml`));
		const index = parse(await get(`doze/${name}/index.yaml`));

		const title = meta.title || name;
		const versions = (cat.engineVersions || []).map(String);
		const runs = versions.length > 1 ? `${versions[0]} – ${versions[versions.length - 1]}` : versions[0];
		const stable = index?.channels?.stable;
		const releases = Object.entries(index?.releases || {})
			.map(([v, r]) => ({ v, engines: (r.engines || []).map(String), plats: Object.keys(r.artifacts || {}).length, stable: v === stable }))
			.sort((a, b) => b.v.localeCompare(a.v, undefined, { numeric: true }));
		const cfg = meta.config || {};
		const fields = (cfg.arguments || []).filter((a) => a.type !== 'block' && !a.arguments);
		const inv = await backendVersions(name, versions);
		const series = inv?.series;
		const pending = inv?.pending || [];

		const page = `${frontmatter(name, title, meta.tagline)}
<!-- GENERATED by website/scripts/gen-engines.mjs. The prose between the recipe marks comes from website/src/content/recipes/${slugOf(name)}.md; the reference below it from the module's signed registry data. Edit those, not this file. -->

**Runs ${title}${runs ? ` ${runs}` : ''} · ${whereLine(cat.platforms || [])}** · [registry page ↗](${SITE}/doze/${name}/)

${recipeBlock(name)}

## Reference

${meta.description || meta.tagline || ''}

### Every field at once

<!-- ground:skip -->
\`\`\`hcl
${asModuleBlock(name, meta.example)}
\`\`\`

${series ? `### Versions you can declare

The \`version =\` you write is the engine's own version. Declare a series and
doze pins its newest published build, or declare an exact build. Either way it
is fetched, verified and pinned in \`doze.lock\`, so it never moves on its own.

| \`version =\` | pins (today) | exact builds |
|---|---|---|
${series.map((sr) => `| \`${sr.series}\` | \`${sr.latest}\` | ${sr.fulls.length} |`).join('\n')}

<details>
<summary>Every exact build, per series</summary>

${series.map((sr) => `**${sr.series}**: ${sr.fulls.map((f) => `\`${f}\``).join(' · ')}`).join('\n\n')}

</details>
${pending.length ? `\n_The binaries mirror also publishes ${pending.map((p) => `\`${p}\``).join(' · ')}: declarable once the module supports ${pending.length === 1 ? 'that series' : 'those series'}._\n` : ''}
` : versions.length ? `### Versions you can declare

The \`version =\` you write is the engine's own version. doze fetches and
verifies it, pins it in \`doze.lock\`, and picks the module release that
provides it.

${versions.map((v) => `\`${v}\``).join(' · ')}
` : `### Versions

This engine takes no \`version =\`: it tracks current APIs.
`}
### Fields

${fieldTable(fields) || '_This engine has no top-level fields of its own._'}
Every module block also takes doze's own fields: \`port\`, \`listen\`, \`sleep\`,
\`after\`, \`enabled\`, \`routes\`, \`count\` and \`for_each\`
([doze.hcl reference](/reference/doze-hcl/)).
${nestedBlocks(name, cfg)}
### Module releases

The releases of the module that provide this engine. doze picks the newest
one that fits your doze and your \`version =\`, and pins it in \`doze.lock\`;
you never write these. Older releases stay available for pinned lockfiles.

| Release | Engine versions | Platforms | |
|---|---|---|---|
${releases.map((r) => `| \`${r.v}\` | ${r.engines.join(' · ')} | ${r.plats} | ${r.stable ? '**stable**' : ''} |`).join('\n')}

Signed data: [index.yaml ↗](${RAW}/doze/${name}/index.yaml) · [meta.yaml ↗](${RAW}/doze/${name}/meta.yaml)
`;
		writeFileSync(`${OUT}${slugOf(name)}.md`, page);
		n++;
	}
	console.log(`✓ generated ${n} service page(s) from ${BASE}`);
}

try {
	await generate();
} catch (e) {
	const kept = ORDER.filter(refresh);
	console.warn(`⚠ registry fetch failed (${e.message}): kept the committed reference, refreshed the recipes of ${kept.join(', ') || 'none'}`);
}
