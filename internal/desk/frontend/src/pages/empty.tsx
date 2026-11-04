import { useEffect, useState } from "react";
import { set } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";

// A folder with no doze.hcl yet, as the NewTab board draws it: what doze
// found there, and the three ways to start, the same three as a bare doze
// in an empty folder. Nothing starts until one is chosen.
export function EmptyPage({ e }: { e: view.Empty }) {
  const choices = e.choices ?? [];
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
      if (ev.metaKey || ev.ctrlKey || ev.altKey) return;
      const tag = (ev.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      switch (ev.key) {
        case "ArrowDown":
          ev.preventDefault();
          setAt((i) => Math.min(i + 1, choices.length - 1));
          break;
        case "ArrowUp":
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
          // Not typed into the dialog the key opens.
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
      <div className="head" style={{ paddingBottom: 4 }}>
        <h1>{e.title}</h1>
      </div>
      {e.found && (
        <div className="dim" style={{ padding: "0 20px 14px" }}>
          {e.found}
        </div>
      )}
      <div className="startgrid">
        <section style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <h3 className="cap">Start here</h3>
          {choices.map((c, i) => (
            <button key={c.n} className={"choice" + (i === at ? " on" : "")} onClick={() => choose(c)} onMouseEnter={() => setAt(i)}>
              <span className="k">{c.n}</span>
              <div>
                <b className="bright">{c.title}</b>
                <div className="dim" style={{ fontSize: 12 }}>
                  {c.what}
                </div>
              </div>
            </button>
          ))}
          <div className="dim" style={{ fontSize: 12, marginTop: "auto" }}>
            The same three choices as a bare <b className="bright">doze</b> in an empty folder.
          </div>
        </section>
      </div>
      <Keys
        keys={[
          ["↑↓", "move"],
          ["⏎", "choose"],
          ["1-3", "pick"],
        ]}
        note="nothing starts until you choose"
      />
    </>
  );
}
