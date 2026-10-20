// Measures how far an operation catalogue (operation -> resource-naming input member)
// can be generated from AWS's own data: Smithy models + the Service Authorization Reference.
const models = "/Users/srini/Code/doze-dev/doze-aws/.audit-models/";
const sar = "/private/tmp/claude-501/-Users-srini-Code-doze-dev/b509a744-8a01-474e-a793-3cc95f697879/scratchpad/dl/";
const pairs: [string, string][] = [
  ["sqs.json", "sqs"], ["sns.json", "sns"], ["s3.json", "s3"], ["dynamodb.json", "dynamodb"],
  ["kinesis.json", "kinesis"], ["lambda.json", "lambda"], ["eventbridge.json", "events"],
  ["sfn.json", "states"], ["secrets-manager.json", "secretsmanager"], ["ssm.json", "ssm"],
];
const tot = { ops: 0, withRes: 0, ctx: 0, name: 0, none: 0 };
for (const [mf, sf] of pairs) {
  const m = JSON.parse(await Bun.file(models + mf).text());
  const s = JSON.parse(await Bun.file(sar + sf + "-sar.json").text());
  const res = new Map<string, string[]>();
  for (const a of s.Actions) res.set(a.Name, (a.Resources ?? []).map((r: any) => r.Name));
  const shapes = m.shapes;
  let ops = 0, withRes = 0, ctx = 0, byName = 0, none = 0;
  const misses: string[] = [];
  for (const [id, sh] of Object.entries<any>(shapes)) {
    if (sh.type !== "operation") continue;
    ops++;
    const op = id.split("#")[1];
    const rtypes = (res.get(op) ?? []).filter((r) => r);
    if (!rtypes.length) continue; // account-level action (ListQueues, CreateTopic with "*")
    withRes++;
    const inp = sh.input ? shapes[sh.input.target] : undefined;
    const members = Object.entries<any>(inp?.members ?? {});
    const c = members.find(([, v]) => v.traits?.["smithy.rules#contextParam"]);
    if (c) { ctx++; continue; }
    const words = rtypes.map((r) => r.toLowerCase().replace(/[^a-z]/g, ""));
    const n = members.find(([k]) => words.some((w) => k.toLowerCase().startsWith(w) || k.toLowerCase().includes(w)));
    if (n) { byName++; continue; }
    none++;
    if (misses.length < 6) misses.push(`${op}[${rtypes.join(",")}]{${members.map(([k]) => k).slice(0, 5).join(",")}}`);
  }
  console.log(`${sf.padEnd(15)} ops=${ops} resource-scoped=${withRes} contextParam=${ctx} nameMatch=${byName} unresolved=${none}  e.g. ${misses.join(" ")}`);
  tot.ops += ops; tot.withRes += withRes; tot.ctx += ctx; tot.name += byName; tot.none += none;
}
console.log("TOTAL", tot, "resolved%", (((tot.ctx + tot.name) / tot.withRes) * 100).toFixed(1));
