import { useEffect, useState } from "react";
import { set } from "../store";
import { transport, type view } from "../transport";
import { pageKey } from "../keys";
import { Keys } from "../components/keys";

// A folder with no doze.hcl yet, as the OpenFolder board draws it: what
// doze found there, one line each, and the three ways to start, the same
// three as a bare doze in an empty folder. Nothing starts until one is
// chosen.
export function EmptyPage({ e }: { e: view.Empty }) {
  const choices = e.choices ?? [];
  const items = e.items ?? [];
  const [at, setAt] = useState(0);

  const choose = (c: view.Choice) => {
    if (c.action === "add") {
      set({ dialog: { key: "", d: e.add } });
      return;
    }
    transport().then((t) => t.setup(e.dir, c.action, "").catch((err) => t.log(`setup ${c.action} failed: ${String(err)}`)));
  };

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (!pageKey(ev)) return;
      switch (ev.key) {
        case "ArrowDown":
        case "ArrowRight":
          ev.preventDefault();
          setAt((i) => Math.min(i + 1, choices.length - 1));
          break;
        case "ArrowUp":
        case "ArrowLeft":
          ev.preventDefault();
          setAt((i) => Math.max(i - 1, 0));
          break;
        case "Enter":
          ev.preventDefault();
          if (choices[at]) choose(choices[at]);
          break;
        case "1":
        case "2":
        case "3": {
          ev.preventDefault();
          const c = choices[Number(ev.key) - 1];
          if (c) {
            setAt(Number(ev.key) - 1);
            choose(c);
          }
          break;
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [choices, at, e]);

  return (
    <>
      <div className="head">
        <h1>{e.name}</h1>
        <span className="path">{e.dir} · no doze.hcl yet</span>
      </div>
      <div className="banner cool" role="status">
        <span className="dot c" aria-hidden />
        <div className="t">
          <b>{items.length > 0 ? "Nothing has started. doze read the folder and found this." : "Nothing has started. doze read the folder and found nothing it recognises."}</b>
          <span>Pick one of the three ways below; each writes a doze.hcl you can open in your editor. Nothing runs until you bring the stack up.</span>
        </div>
      </div>
      {items.length > 0 && (
        <div className="lib" aria-label="Found">
          <div className="lrow h" style={{ gridTemplateColumns: "140px minmax(0,1fr) 180px" }}>
            <span>Found</span>
            <span>Where</span>
            <span>Becomes</span>
          </div>
          {items.map((it) => (
            <div key={it.name + it.becomes} className="lrow" style={{ gridTemplateColumns: "140px minmax(0,1fr) 180px", padding: "8px 16px 8px 14px" }}>
              <b className="mono bright">{it.name}</b>
              <span className="dim ell" title={it.where}>
                {it.where}
              </span>
              <span className="dim mono ell">{it.becomes}</span>
            </div>
          ))}
        </div>
      )}
      <div className="tiles" style={{ gridTemplateColumns: "repeat(3, minmax(0, 1fr))", marginTop: 14 }}>
        {choices.map((c, i) => (
          <button key={c.n} className={"tile" + (i === at ? " sel" : "")} style={{ minHeight: 96 }} onClick={() => choose(c)} onMouseEnter={() => setAt(i)}>
            <div className="top">
              <span className="step-num">{c.n}</span>
              <b>{c.title}</b>
            </div>
            <div className="dim" style={{ fontSize: 12, lineHeight: "17px", whiteSpace: "normal" }}>
              {c.what}
            </div>
          </button>
        ))}
      </div>
      <p className="model">
        The same three choices as <span className="mono">doze</span> run in a terminal in this folder. doze never edits <span className="mono">doze.hcl</span> behind your back: after this, the file is yours.
      </p>
      <Keys
        keys={[
          ["1-3", "pick"],
          ["⏎", "choose"],
          ["esc ⌘W", "close without writing anything"],
        ]}
        note="from Library: Open folder, a drop, or doze open in a terminal"
      />
    </>
  );
}
