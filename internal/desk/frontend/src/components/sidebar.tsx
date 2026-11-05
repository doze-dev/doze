import { go, set, useStore, type Where } from "../store";
import type { view } from "../transport";
import { Bat } from "./bat";

// The sidebar inside a service, as the inside boards draw it: Overview,
// then the stack's instances under their groups, each with its dot and a
// word or figure; on the logs page, the Show checkboxes instead.
const GROUPS: Array<[string, string]> = [
  ["app", "Apps"],
  ["module", "Modules"],
  ["service", "Outside"],
  ["task", "Tasks"],
];

export function Sidebar({ where, side, version, stacks }: { where: Where | null; side: view.Side[]; version: string; stacks: number }) {
  const show = useStore((s) => s.show);
  const key = where?.key ?? "";
  return (
    <nav className="side" aria-label={where?.page === "logs" ? "Show" : "Services"}>
      <button className={"si" + (where?.page === "home" ? " on" : "")} onClick={() => key && void go({ key, page: "home" })} title="Back to the stack (esc)">
        <span className="glyph dim" aria-hidden>
          ‹
        </span>
        <span className="t">Overview</span>
      </button>
      {where?.page === "logs" && (
        <button className="si on">
          <span className="t">All logs</span>
        </button>
      )}
      {where?.page === "instance" &&
        GROUPS.map(([kind, title]) => {
          const rows = side.filter((s) => (s.kind || "module") === kind || (kind === "module" && !s.kind));
          if (rows.length === 0) return null;
          return (
            <div key={kind}>
              <h4>{title}</h4>
              {rows.map((s) => (
                <button key={s.name} className={"si" + (s.name === where.instance ? " on" : "")} onClick={() => void go({ key, page: "instance", instance: s.name })} title={s.name}>
                  <Dot look={s.look} trouble={s.trouble} />
                  <span className={"t" + (s.look.word === "disabled" ? " dim" : "")}>{s.name}</span>
                  <small className={s.trouble ? "bad" : ""}>{s.trouble ? s.look.word : s.note}</small>
                </button>
              ))}
            </div>
          );
        })}
      {where?.page === "logs" && <ShowList show={show} />}
      <div className="foot">
        <Bat size={18} />
        doze {version || ""} · {stacks} {stacks === 1 ? "stack" : "stacks"}
      </div>
    </nav>
  );
}

/** Dot is an instance's state as the boards draw it: a filled dot, a ring while waking, a glyph for a task. */
export function Dot({ look, trouble }: { look: view.Look; trouble?: boolean }) {
  if (trouble) return <span className="dot r" aria-hidden />;
  switch (look.word) {
    case "awake":
      return <span className="dot g" aria-hidden />;
    case "dozing":
      return <span className="dot y" aria-hidden />;
    case "waking":
    case "running":
      return <span className="ring" aria-hidden />;
    case "asleep":
    case "disabled":
      return <span className="dot" aria-hidden />;
  }
  return (
    <span className={"glyph tone-" + look.tone} aria-hidden>
      {look.glyph}
    </span>
  );
}

function ShowList({ show }: { show: string[] | null }) {
  const home = useStore((s) => s.home);
  const names = (home?.side ?? []).map((s) => s.name);
  const all = [...names, "doze"];
  const on = (n: string) => show === null || show.includes(n);
  const toggle = (n: string) => {
    const next = all.filter((x) => (x === n ? !on(x) : on(x)));
    set({ show: next.length === all.length ? null : next });
  };
  return (
    <>
      <h4>Show</h4>
      {(home?.side ?? []).map((s) => (
        <label key={s.name} className="si">
          <input type="checkbox" checked={on(s.name)} onChange={() => toggle(s.name)} />
          <Dot look={s.look} trouble={s.trouble} />
          <span className="t">{s.name}</span>
        </label>
      ))}
      <label className="si">
        <input type="checkbox" checked={on("doze")} onChange={() => toggle("doze")} />
        <span className="acc glyph" aria-hidden>
          z
        </span>
        <span className="t">doze itself</span>
      </label>
    </>
  );
}
