// Builds the doze, doze-kafka and doze-aws icons from the traced bat in
// bat-source.svg: the bat on a rounded tile in the product's accent colour.
// Run: bun make.ts
const src = await Bun.file("bat-source.svg").text();
const paths = [...src.matchAll(/<path fill="([^"]+)" d="([^"]+)"\/>/g)].map((m) => m[2]);

// Traced pieces: 1–3 and 6 are the body (1 includes the feet and a bar they
// grip), 4–5 the eyes. The rest (the two upper bars, trace specks) is unused.
const body = [paths[1], paths[2], paths[3], paths[6]].map((d) => `<path d="${d}" />`).join("");
const eyes = [paths[4], paths[5]].map((d) => `<path d="${d}" />`).join("");

// "AWS" in Poppins Bold, as outlines centred on (0,0), so it renders the same
// without the font installed.
const AWS = "M-39.88 20.45L-42.31 13.25L-57.51 13.25L-59.94 20.45L-70.33 20.45L-55.59-20.27L-44.11-20.27L-29.38 20.45L-39.88 20.45ZM-54.90 5.60L-44.86 5.60L-49.91-9.31L-54.90 5.60ZM22.77-20.27L33.44-20.27L22.82 20.45L10.82 20.45L4.32-6.35L-2.41 20.45L-14.41 20.45L-24.74-20.27L-14.12-20.27L-8.26 9.37L-1.01-20.27L9.89-20.27L16.85 9.37L22.77-20.27ZM55.48 20.85Q51.01 20.85 47.47 19.40Q43.94 17.95 41.82 15.11Q39.70 12.27 39.59 8.26L39.59 8.26L50.14 8.26Q50.37 10.53 51.71 11.72Q53.04 12.91 55.19 12.91L55.19 12.91Q57.39 12.91 58.67 11.89Q59.94 10.88 59.94 9.08L59.94 9.08Q59.94 7.57 58.93 6.58Q57.91 5.60 56.43 4.96Q54.96 4.32 52.23 3.51L52.23 3.51Q48.29 2.29 45.79 1.07Q43.30-0.14 41.50-2.52Q39.70-4.90 39.70-8.73L39.70-8.73Q39.70-14.41 43.82-17.63Q47.94-20.85 54.55-20.85L54.55-20.85Q61.28-20.85 65.40-17.63Q69.51-14.41 69.80-8.67L69.80-8.67L59.07-8.67Q58.96-10.64 57.62-11.77Q56.29-12.91 54.20-12.91L54.20-12.91Q52.40-12.91 51.30-11.95Q50.20-10.99 50.20-9.19L50.20-9.19Q50.20-7.22 52.06-6.12Q53.91-5.02 57.86-3.74L57.86-3.74Q61.80-2.41 64.26-1.19Q66.73 0.03 68.53 2.35Q70.33 4.67 70.33 8.32L70.33 8.32Q70.33 11.80 68.56 14.64Q66.79 17.49 63.42 19.17Q60.06 20.85 55.48 20.85L55.48 20.85Z";

type Perch = "branch" | "twigs" | "cloud";

// doze's branch, grown into a small network: two twigs end in ring nodes,
// and the branch ends in the largest ring. The branch covers the traced bar.
function twigs(color: string) {
  return `<g stroke="${color}" fill="none" stroke-linecap="round">
    <path d="M356 210H668" stroke-width="28" />
    <path d="M576 206Q582 176 588 156" stroke-width="18" />
    <path d="M428 206Q420 186 412 170" stroke-width="18" />
  </g>
  <g stroke="${color}" fill="none" stroke-width="20">
    <circle cx="592" cy="126" r="30" />
    <circle cx="404" cy="146" r="26" />
    <circle cx="712" cy="210" r="38" />
  </g>`;
}

function cloud(color: string, letters: string) {
  return `<g fill="${color}">
    <rect x="398" y="166" width="254" height="66" rx="33" />
    <circle cx="430" cy="200" r="32" />
    <circle cx="482" cy="166" r="48" />
    <circle cx="550" cy="140" r="62" />
    <circle cx="614" cy="182" r="44" />
  </g>
  <path transform="translate(525 186)" fill="${letters}" d="${AWS}" />`;
}

function z(x: number, y: number, s: number, w: number) {
  return `<path d="M${x} ${y}h${s}l${-s} ${s}h${s}" stroke-width="${w}" />`;
}

function icon(o: { bat: string; accent: string; perch: Perch; sleep: boolean }) {
  const perch = o.perch === "cloud" ? cloud(o.bat, o.accent) : o.perch === "twigs" ? twigs(o.bat) : "";
  // A sleeping bat moves left to make room for its z's, which rise to the right.
  const cx = o.sleep ? 455 : o.perch === "twigs" ? 500 : 520;
  const cy = o.perch === "twigs" ? 452 : 440;
  const zs = o.sleep
    ? `<g fill="none" stroke="${o.bat}" stroke-linecap="round" stroke-linejoin="round">
        ${z(640, 476, 34, 11)}${z(688, 368, 46, 13)}${z(744, 240, 60, 15)}
      </g>`
    : "";
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="160 80 720 720" width="512" height="512">
  <rect x="160" y="80" width="720" height="720" rx="160" fill="${o.accent}" />
  <g transform="translate(${cx} ${cy}) scale(0.92) translate(-520 -434)">
    <g fill="${o.bat}">${body}</g>
    ${perch}
    <g fill="${o.accent}">${eyes}</g>
  </g>
  ${zs}
</svg>
`;
}

const icons = {
  doze: { bat: "#3A2448", accent: "#F0A24B", perch: "branch" as Perch, sleep: true },
  "doze-kafka": { bat: "#16150F", accent: "#FFD60A", perch: "twigs" as Perch, sleep: false },
  "doze-aws": { bat: "#1F2A44", accent: "#5BB8E8", perch: "cloud" as Perch, sleep: false },
};

for (const [name, v] of Object.entries(icons)) await Bun.write(`${name}.svg`, icon(v));
console.log("wrote", Object.keys(icons).join(", "));
