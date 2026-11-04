import type { view } from "../transport";

// The four cards across the top of home (the Main board's .vitals): memory
// and CPU as bars with a key line, the states, and the connections.
export function Vitals({ v }: { v: view.Vitals4 }) {
  return (
    <div className="vitals">
      <Bar label="Memory" total={v.mem.total} meter={v.mem} />
      <Bar label="CPU" total={v.cpu.total} meter={v.cpu} />
      <div className="vital">
        <span className="k">State</span>
        <span className="v">{v.state.awake} awake</span>
        <span className="counts">
          <span className="ok">● {v.state.awake} awake</span>
          {v.state.dozing > 0 && <span className="warn">◐ {v.state.dozing} dozing</span>}
          {v.state.waking > 0 && <span className="cool">◌ {v.state.waking} waking</span>}
          {v.state.asleep > 0 && <span className="dim">· {v.state.asleep} asleep</span>}
        </span>
        <span className="line">{v.state.sleeps}</span>
      </div>
      <div className="vital">
        <span className="k">Connections</span>
        <span className="v">{v.conns.open}</span>
        <span className="line">open across the stack</span>
        <span className="line">{v.conns.tasks}</span>
      </div>
    </div>
  );
}

function Bar({ label, total, meter }: { label: string; total: string; meter: view.Meter }) {
  const shares = meter.shares ?? [];
  return (
    <div className="vital">
      <span className="k">{label}</span>
      <span className="v">{total}</span>
      <div className="bar" role="img" aria-label={`${label} ${total}`}>
        {shares.map((s, i) => (
          <i key={s.name} className={"c" + (i % 6)} style={{ width: `${Math.max(s.part * 100, 1)}%` }} title={`${s.name} ${s.label}`} />
        ))}
      </div>
      <span className="line">{meter.key}</span>
    </div>
  );
}
