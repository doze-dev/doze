// Writes the app's mark (internal/desk/frontend/src/components/bat.tsx) from
// doze.svg, whole: the tile, the bat, its eyes (cut out in the tile's colour)
// and the z's. Run after changing doze.svg: bun website/brand/bat-tsx.ts
const here = import.meta.dir;
const svg = await Bun.file(`${here}/doze.svg`).text();
const open = svg.match(/<svg[^>]*viewBox="([^"]+)"[^>]*>/);
if (!open) throw new Error("doze.svg has no viewBox");
const inner = svg
  .slice(svg.indexOf(open[0]) + open[0].length, svg.lastIndexOf("</svg>"))
  .replace(/\s+\/>/g, " />")
  .replace(/stroke-width=/g, "strokeWidth=")
  .replace(/stroke-linecap=/g, "strokeLinecap=")
  .replace(/stroke-linejoin=/g, "strokeLinejoin=")
  .split("\n")
  .map((l) => l.trim())
  .filter(Boolean)
  .map((l) => "      " + l)
  .join("\n");
const out = `// The doze mark, from website/brand/doze.svg, whole: the bat on its tile,
// its eyes and the z's, in the brand's own colours. Written by
// website/brand/bat-tsx.ts: do not edit by hand. Inline, so the tab and
// Home need no request.
export function Bat({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="${open[1]}" aria-hidden="true" style={{ display: "block", flex: "none" }}>
${inner}
    </svg>
  );
}
`;
await Bun.write(`${here}/../../internal/desk/frontend/src/components/bat.tsx`, out);
console.log("wrote components/bat.tsx from doze.svg");
