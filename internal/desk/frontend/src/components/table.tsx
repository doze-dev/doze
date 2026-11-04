import type { view } from "../transport";

// The stack as a table, in the dash's groups and columns: state, name,
// what, because, mem, address. Every word is Go's; a click opens the
// instance, and the two buttons send the verbs the row allows.
export function Table({
  groups,
  onOpen,
  onVerb,
}: {
  groups: view.Group[];
  onOpen: (name: string) => void;
  onVerb: (verb: string, name: string) => void;
}) {
  return (
    <div className="table" role="table" aria-label="Stack">
      {groups.map((g) => (
        <section key={g.title} className="group">
          <header className="group-head" role="rowgroup">
            <span className="group-title">{g.title}</span>
            <span className="group-what muted">{g.what}</span>
          </header>
          {(g.rows ?? []).map((r) => (
            <Row key={r.name} row={r} onOpen={onOpen} onVerb={onVerb} />
          ))}
        </section>
      ))}
    </div>
  );
}

function Row({ row: r, onOpen, onVerb }: { row: view.Row; onOpen: (n: string) => void; onVerb: (v: string, n: string) => void }) {
  const task = r.kind === "task";
  return (
    <div className={"row tone-" + r.look.tone} role="row" tabIndex={0} onClick={() => onOpen(r.name)} onKeyDown={(e) => e.key === "Enter" && onOpen(r.name)}>
      <span className="glyph" aria-hidden>
        {r.look.glyph}
      </span>
      <span className="name bright">{r.name}</span>
      <span className="word">{r.look.word}</span>
      <span className={"because " + (r.trouble ? "bad" : r.look.word === "dozing" || r.look.word === "waking" ? "warn" : "muted")}>{r.because}</span>
      <span className="runs muted">{r.runs}</span>
      <span className="mem">{r.mem}</span>
      <span className="addr muted">
        {r.addr}
        {r.port && <span className="port">:{r.port}</span>}
      </span>
      <span className="verbs" onClick={(e) => e.stopPropagation()}>
        {task ? (
          <button onClick={() => onVerb("task", r.name)}>run</button>
        ) : r.awake ? (
          <button onClick={() => onVerb("sleep", r.name)}>sleep</button>
        ) : (
          <button onClick={() => onVerb("wake", r.name)}>wake</button>
        )}
      </span>
    </div>
  );
}
