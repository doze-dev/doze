import { useEffect, useState } from "react";
import { stackAction } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";

// A stopped stack, as the StackStopped board lays it out: tick what should
// start, remembered from last time; Up starts the ticked ones in dependency
// order. The rows are read from doze.hcl, since there is no daemon to ask.
export function StoppedPage({ home, stackKey }: { home: view.Home; stackKey: string }) {
  const s = home.stopped!;
  const rows = (s.groups ?? []).flatMap((g) => g.rows ?? []);
  const [ticked, setTicked] = useState<Record<string, boolean>>({});
  const [at, setAt] = useState(rows[0]?.name ?? "");
  useEffect(() => {
    setTicked(Object.fromEntries(rows.filter((r) => r.tick !== "").map((r) => [r.name, r.tick === "on"])));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [home.key, rows.length]);
  const chosen = rows.filter((r) => ticked[r.name]).map((r) => r.name);
  const up = () => void stackAction(stackKey, chosen.length === 0 ? "up-all" : "up", chosen);
  const edit = () => transport().then((t) => t.open("file://" + home.dir + "/doze.hcl"));

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "e") {
        e.preventDefault();
        void edit();
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      const i = rows.findIndex((r) => r.name === at);
      switch (e.key) {
        case "ArrowDown":
        case "j":
          e.preventDefault();
          if (rows[i + 1]) setAt(rows[i + 1].name);
          break;
        case "ArrowUp":
        case "k":
          e.preventDefault();
          if (rows[i - 1]) setAt(rows[i - 1].name);
          break;
        case " ":
          e.preventDefault();
          if (at && rows[i]?.tick !== "") setTicked((t) => ({ ...t, [at]: !t[at] }));
          break;
        case "a":
          setTicked(Object.fromEntries(rows.filter((r) => r.tick !== "" && r.tick !== "off").map((r) => [r.name, true])));
          break;
        case "Enter":
          up();
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, at, ticked]);

  return (
    <>
      <div className="head">
        <h1>{home.stack}</h1>
        <span className="path">
          {home.dir} · stopped · {s.when}
        </span>
        <span className="grow" />
        <button className="btn" onClick={edit} title="Open doze.hcl in your editor (⌘E)">
          Edit doze.hcl
        </button>
        <span className="swl">
          <b>Up</b>
          <button className="sw big" role="switch" aria-checked={false} aria-label={`${home.stack} is down`} onClick={() => void stackAction(stackKey, "up-last")} />
        </span>
      </div>
      {s.problem ? (
        <>
          <div className="banner bad" role="alert">
            <span className="dot r" aria-hidden />
            <div className="t">
              <b>doze.hcl does not load, so nothing can start.</b>
              <span>{s.problem}</span>
            </div>
            <button className="btn" onClick={edit}>
              Edit doze.hcl <kbd>⌘E</kbd>
            </button>
          </div>
          <p className="model">
            The same message as <span className="mono">doze lint</span>. Fix the file in your editor; this page reads it again on save, and Up is back the moment it loads.
          </p>
          <Keys keys={[["⌘0", "library"]]} note="from Library: a stopped stack whose doze.hcl fails to load" />
        </>
      ) : (
        <div className="banner" role="status">
          <span className="dot" aria-hidden />
          <div className="t">
            <b>Nothing is running. Tick what should start; the rest wakes when something connects to it.</b>
            <span>{s.lastLine} Up starts the ticked ones in dependency order and remembers the set.</span>
          </div>
          <button className="btn pri" onClick={up}>
            Up as ticked <kbd>⏎</kbd>
          </button>
          <button className="btn" onClick={() => void stackAction(stackKey, "up-all")}>
            Up everything
          </button>
        </div>
      )}
      {!s.problem && (
      <>
      <div className="scrollarea">
        <div className="lib">
          <div className="lrow h" style={{ gridTemplateColumns: "18px minmax(140px,1fr) 100px minmax(160px,2fr) minmax(140px,1fr)" }}>
            <span />
            <span>Name</span>
            <span>Kind</span>
            <span>What it needs</span>
            <span>Address when up</span>
          </div>
          {(s.groups ?? []).map((g) => (
            <div key={g.title}>
              <div className="grp">
                <span className="car">▾</span>
                {g.title}
                <span className="cnt">
                  {g.count} · {g.what || `${(g.rows ?? []).filter((r) => ticked[r.name]).length} ticked`}
                </span>
              </div>
              {(g.rows ?? []).map((r) => (
                <button
                  key={r.name}
                  className={"lrow" + (r.name === at ? " sel" : "")}
                  style={{ gridTemplateColumns: "18px minmax(140px,1fr) 100px minmax(160px,2fr) minmax(140px,1fr)", padding: "8px 16px 8px 14px" }}
                  onClick={() => {
                    setAt(r.name);
                    if (r.tick !== "") setTicked((t) => ({ ...t, [r.name]: !t[r.name] }));
                  }}
                >
                  {r.tick === "" ? <span /> : <span className={"box" + (ticked[r.name] ? " on" : r.tick === "half" ? " half" : "")}>{ticked[r.name] ? "✓" : ""}</span>}
                  <span className="name" style={{ gap: 0 }}>
                    <b className={ticked[r.name] ? "" : "dim"} style={{ fontSize: 13 }}>
                      {r.name}
                    </b>
                    {r.run && <span className="dim mono ell" style={{ fontSize: 11 }}>{r.run}</span>}
                  </span>
                  <span className="dim">{r.engine}</span>
                  <span className="dim ell">
                    {r.needs} · {r.note}
                  </span>
                  <span className="mono dim ell">{r.address}</span>
                </button>
              ))}
            </div>
          ))}
        </div>
        <p className="model" style={{ marginTop: 12 }}>
          <b>Half-ticked</b> means it is not started on Up but wakes the moment something needs it. That is the normal state for a module.
        </p>
      </div>
      <Keys
        keys={[
          ["space", "tick"],
          ["⏎", "up as ticked"],
          ["a", "tick all"],
            ["⌘E", "edit doze.hcl"],
        ]}
        note="from Library: a stopped stack, or the switch's Choose"
      />
      </>
      )}
    </>
  );
}
