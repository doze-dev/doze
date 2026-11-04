import type { view } from "../transport";

// One vital as the Instance board draws it: a card with the latest reading
// as the figure, and an area chart of the last four minutes, from Go's
// points and scale. Redrawn only when a push brings new points.
export function ChartCard({ title, chart, color, tall }: { title: string; chart: view.Chart; color: string; tall?: boolean }) {
  return (
    <section className="card">
      <h3>
        {title} · last {chart.span}
      </h3>
      <div className="figure">
        <b>{chart.now}</b>
        <span className="dim">{chart.sub}</span>
      </div>
      <Area chart={chart} color={color} height={tall ? 90 : 70} />
    </section>
  );
}

export function Area({ chart, color, height, width = 400 }: { chart: view.Chart; color: string; height: number; width?: number }) {
  const pts = chart.points ?? [];
  const span = chart.hi - chart.lo || 1;
  const xy = pts.map((v, i) => [(i / Math.max(pts.length - 1, 1)) * width, height - ((v - chart.lo) / span) * (height - 6) - 3] as const);
  const line = xy.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  return (
    <svg viewBox={`0 0 ${width} ${height}`} width="100%" height={height} aria-label={`peak ${chart.peak}, mean ${chart.mean}`} role="img">
      {pts.length > 1 ? (
        <>
          <polyline fill="none" stroke={`var(--${color})`} strokeWidth="2" points={line} />
          <polygon fill={`var(--${color})`} fillOpacity=".12" points={`${line} ${width},${height} 0,${height}`} />
        </>
      ) : (
        <text x="4" y={height / 2} className="dim" fontSize="11" fill="var(--dim)">
          no readings yet
        </text>
      )}
    </svg>
  );
}

/** Sparkline is the small line in the logs page's "this run" box. */
export function Sparkline({ points, color }: { points: number[]; color: string }) {
  const w = 120;
  const h = 26;
  const lo = Math.min(...points);
  const hi = Math.max(...points);
  const span = hi - lo || 1;
  const line = points.map((v, i) => `${((i / Math.max(points.length - 1, 1)) * w).toFixed(1)},${(h - ((v - lo) / span) * (h - 6) - 3).toFixed(1)}`).join(" ");
  return (
    <svg className="sparkline" viewBox={`0 0 ${w} ${h}`} width={w} height={20} aria-hidden>
      <polyline fill="none" stroke={`var(--${color})`} strokeWidth="1.6" points={line} />
    </svg>
  );
}
