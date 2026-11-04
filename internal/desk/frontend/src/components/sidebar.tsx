import { go, set, useStore, type Where } from "../store";
import type { view } from "../transport";

// The sidebar (the boards' .side): the View list, then the stack's
// instances on an instance page, or the Show checkboxes on the logs page.
export function Sidebar({ where, side, version, stacks }: { where: Where | null; side: view.Side[]; version: string; stacks: number }) {
  const show = useStore((s) => s.show);
  const key = where?.key ?? "";
  return (
    <nav className="side" aria-label="Views">
      <h4>View</h4>
      <button className={"space" + (where?.page !== "logs" ? " on" : "")} onClick={() => key && void go({ key, page: "home" })}>
        {where?.page !== "logs" ? <b>Services</b> : "Services"}
      </button>
      <button className="space" disabled title="Arrives after parity">
        Map <span className="new">new</span>
      </button>
      <button className="space" disabled title="Arrives after parity">
        Timeline <span className="new">new</span>
      </button>
      {where?.page === "instance" && side.length > 0 && (
        <>
          <h4>Instances</h4>
          {side.map((s) => (
            <button
              key={s.name}
              className={"space compact" + (s.name === where.instance ? " on" : "")}
              onClick={() => void go({ key, page: "instance", instance: s.name })}
            >
              <Dot look={s.look} trouble={s.trouble} />
              {s.name === where.instance ? <b>{s.name}</b> : s.name}
              {s.trouble ? <span className="badge">!</span> : <small>{s.note}</small>}
            </button>
          ))}
        </>
      )}
      {where?.page === "logs" && <ShowList show={show} />}
      <div className="foot">
        <span className="acc" aria-hidden>
          z
        </span>
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
        <label key={s.name} className="space compact">
          <input type="checkbox" checked={on(s.name)} onChange={() => toggle(s.name)} />
          <Dot look={s.look} trouble={s.trouble} />
          {s.name}
        </label>
      ))}
      <label className="space compact">
        <input type="checkbox" checked={on("doze")} onChange={() => toggle("doze")} />
        <span className="acc glyph" aria-hidden>
          z
        </span>
        doze itself
      </label>
    </>
  );
}
