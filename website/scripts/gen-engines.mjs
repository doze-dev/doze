// gen-engines — generate one docs page per engine module from the signed
// registry's own data (catalog + index.yaml + meta.yaml), so the docs show
// exactly what each module runs — engine versions, platforms, every published
// release, and the full config reference — without hand-maintained duplicates
// that drift.
//
// Runs before `astro build` (see package.json). On any fetch failure it keeps
// the committed snapshot in src/content/docs/engines/ and warns — the docs
// build must not die because the registry hiccuped.
import { writeFileSync, mkdirSync } from 'node:fs';
import { parse } from 'yaml';

const BASE = process.env.DOZE_REGISTRY_BASE || 'https://doze.nerdmenot.in/registry';
const OUT = new URL('../src/content/docs/engines/', import.meta.url).pathname;

// Display order — the same order as the engines overview.
const ORDER = ['postgres', 'valkey', 'kvrocks', 'ferret', 'mariadb', 'temporal', 'kafka', 'aws'];

const platformLabel = (t) =>
	t
		.replace('aarch64-apple-darwin', 'macOS (Apple Silicon)')
		.replace('aarch64-unknown-linux-gnu', 'Linux (arm64)')
		.replace('x86_64-unknown-linux-gnu', 'Linux (x86_64)');

const whereLine = (platforms) => {
	const mac = platforms.some((t) => t.includes('apple-darwin'));
	const linux = platforms.some((t) => t.includes('linux'));
	if (mac && linux) return 'macOS & Linux';
	if (!mac && platforms.length === 1) return platformLabel(platforms[0]) + ' only';
	return mac ? 'macOS only' : 'Linux only';
};

const esc = (s) => String(s ?? '').replaceAll('|', '\\|').replaceAll('\n', ' ');

function argTable(args) {
	const rows = args.filter((a) => !a.arguments);
	if (!rows.length) return '';
	let out = '| Field | Type | Default | Description |\n|---|---|---|---|\n';
	for (const a of rows) {
		const def = a.default ? `\`${a.default}\`` : '—';
		const req = a.required ? ' **Required.**' : '';
		out += `| \`${a.name}\` | ${a.type || ''} | ${def} | ${esc(a.desc)}${req} |\n`;
	}
	return out;
}

function blockSections(args) {
	let out = '';
	for (const a of args.filter((a) => a.arguments)) {
		out += `\n**\`${a.name} "<name>" { … }\`** — ${esc(a.desc)}\n\n`;
		out += argTable(a.arguments) || '_No fields — the label is the declaration._\n';
	}
	return out;
}

// backendVersions resolves what a user can actually DECLARE, from
// doze-binaries' published index: every series (the `version =` you write —
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

try {
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
		const args = meta.config?.arguments || [];
		const inv = await backendVersions(name, versions);
		const series = inv?.series;
		const pending = inv?.pending || [];

		const page = `---
title: "${title}"
description: "${(meta.tagline || '').replaceAll('"', '\\"')}"
---

<!-- GENERATED by website/scripts/gen-engines.mjs from the signed registry — do not edit by hand. -->

**Runs ${title}${runs ? ` ${runs}` : ''} · ${whereLine(cat.platforms || [])}** · [registry page ↗](https://doze.nerdmenot.in/registry/doze/${name}/)

${meta.description || meta.tagline || ''}

## Usage

\`\`\`hcl
${(meta.example || '').trim()}
\`\`\`

${series ? `## Versions you can declare

The \`version =\` you write is the **engine's own version** — the only version
that's yours. Declare a series and doze pins its newest published build, or
declare an exact build; either way it's fetched, verified, and pinned in
\`doze.lock\` (so it never moves on its own).

| \`version =\` | pins (today) | exact builds |
|---|---|---|
${series.map((sr) => `| \`${sr.series}\` | \`${sr.latest}\` | ${sr.fulls.length} |`).join('\n')}

<details>
<summary>Every exact build, per series</summary>

${series.map((sr) => `**${sr.series}** — ${sr.fulls.map((f) => `\`${f}\``).join(' · ')}`).join('\n\n')}

</details>
${pending.length ? `\n_The mirror also publishes ${pending.map((p) => `\`${p}\``).join(' · ')} — declarable once the module adds support for ${pending.length === 1 ? 'that series' : 'those series'}._\n` : ''}
` : versions.length ? `## Versions you can declare

The \`version =\` you write is the **engine's own version** — the only version
that's yours. doze fetches and verifies it, pins it in \`doze.lock\`, and picks
the module release providing it automatically.

${versions.map((v) => `\`${v}\``).join(' · ')}
` : `## Versions

This engine takes no \`version =\` — it tracks current APIs.
`}
## Configuration

${argTable(args) || '_This engine has no top-level arguments._'}
${blockSections(args)}
## Module releases

Machinery, for the curious: the plugin releases that provide this engine.
doze selects the newest compatible one and pins it in \`doze.lock\` — you
never write these. Older releases stay resolvable for pinned lockfiles.

| Release | Engine versions | Platforms | |
|---|---|---|---|
${releases.map((r) => `| \`${r.v}\` | ${r.engines.join(' · ')} | ${r.plats} | ${r.stable ? '**stable**' : ''} |`).join('\n')}

Raw signed data: [index.yaml ↗](https://doze.nerdmenot.in/registry/doze/${name}/index.yaml) · [meta.yaml ↗](https://doze.nerdmenot.in/registry/doze/${name}/meta.yaml)
`;
		writeFileSync(`${OUT}${name}.md`, page);
		n++;
	}
	console.log(`✓ generated ${n} engine page(s) from ${BASE}`);
} catch (e) {
	console.warn(`⚠ engine-page generation skipped (${e.message}) — using the committed snapshot`);
}
