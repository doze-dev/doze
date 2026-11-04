import type { view } from "../transport";
import { Dot } from "./sidebar";

// The stack as a table (the Main board's .tbl): a header row, a group row
// per heading, and the columns Name, Engine, State, Memory, CPU%, Conn,
// Up, Because. Every word is Go's. One row is selected; its logs fill the
// dock under the table. Enter opens it.
const COLS = "172px 92px 88px 76px 52px 52px 52px minmax(0,1fr)";

export function Table({
  groups,
  selected,
  onSelect,
  onOpen,
}: {
  groups: view.Group[];
  selected: string;
  onSelect: (name: string) => void;
  onOpen: (name: string) => void;
}) {
  return (
    <div className="tbl compact" role="table" aria-label="Stack" style={{ ["--c" as string]: COLS }}>
      <div className="tr h" role="row">
        <span>Name</span>
        <span>Engine</span>
        <span>State</span>
        <span className="r">Memory</span>
        <span className="r">CPU%</span>
        <span className="r">Conn</span>
        <span className="r">Up</span>
        <span>Because</span>
      </div>
      {groups.map((g) => (
        <div key={g.title} role="rowgroup">
          <div className="tr grp" role="row">
            {g.title}
            <span>
              {g.count} · {g.what}
            </span>
          </div>
          {(g.rows ?? []).map((r) => (
            <button
              key={r.name}
              role="row"
              className={"tr row" + (r.name === selected ? " on" : "")}
              aria-selected={r.name === selected}
              onClick={() => onSelect(r.name)}
              onDoubleClick={() => onOpen(r.name)}
              onKeyDown={(e) => e.key === "Enter" && onOpen(r.name)}
            >
              <span className="n">
                <Dot look={r.look} trouble={r.trouble} />
                {r.name}
              </span>
              <span>{r.engine}</span>
              <span className={"tone-" + r.look.tone}>{r.look.word}</span>
              <span className="r">{r.mem}</span>
              <span className="r">{r.cpu}</span>
              <span className="r">{r.conns}</span>
              <span className="r">{r.up}</span>
              <span className={r.becauseTone}>{r.because}</span>
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
