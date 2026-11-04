import type { view } from "../transport";

// One vital as a small line chart: SVG, drawn from Go's points and scale,
// redrawn only when a push brings new points (at most once a second).
export function Chart({ label, chart }: { label: string; chart: view.Chart }) {
  const pts = chart.points ?? [];
  const w = 240;
  const h = 48;
  const span = chart.hi - chart.lo || 1;
  const path = pts
    .map((v, i) => {
      const x = (i / Math.max(pts.length - 1, 1)) * w;
      const y = h - ((v - chart.lo) / span) * (h - 2) - 1;
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(" ");
  return (
    <figure className="chart">
      <figcaption>
        <span className="chart-label">{label}</span>
        <span className="muted">
          peak {chart.peak} · mean {chart.mean} · {chart.span}
        </span>
      </figcaption>
      <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} role="img" aria-label={`${label}: peak ${chart.peak}, mean ${chart.mean}`}>
        {pts.length > 1 ? <path d={path} fill="none" stroke="var(--accent)" strokeWidth="1.5" /> : <text x="4" y="28" className="muted">no readings yet</text>}
      </svg>
    </figure>
  );
}
