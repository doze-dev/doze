import type { view } from "../transport";

// The two bars at the top of home: who holds how much memory and CPU, with
// the figures Go computed. Each share is a run of the bar; the key under it
// names them largest first.
export function Meters({ meters }: { meters: view.Meters }) {
  return (
    <div className="meters">
      <Meter label="mem" unit="MB" meter={meters.mem} />
      <Meter label="cpu" unit="" meter={meters.cpu} />
    </div>
  );
}

function Meter({ label, unit, meter }: { label: string; unit: string; meter: view.Meter }) {
  const shares = meter.shares ?? [];
  return (
    <div className="meter">
      <div className="meter-head">
        <span className="meter-label">{label}</span>
        <span className="meter-total">
          {meter.total}
          {unit && <span className="unit"> {unit}</span>}
        </span>
      </div>
      <div className="bar" role="img" aria-label={`${label} ${meter.total} ${unit}`}>
        {shares.map((s, i) => (
          <span key={s.name} className={"run c" + (i % 6)} style={{ flexGrow: Math.max(s.part, 0.01) }} title={`${s.name} ${s.label}`} />
        ))}
        {shares.length === 0 && <span className="run none" />}
      </div>
      <div className="meter-key">
        {shares.map((s, i) => (
          <span key={s.name} className="key">
            <span className={"swatch c" + (i % 6)} aria-hidden /> {s.name} <span className="muted">{s.label}</span>
          </span>
        ))}
        {meter.idle > 0 && <span className="key muted">{meter.idle} idle</span>}
      </div>
    </div>
  );
}
