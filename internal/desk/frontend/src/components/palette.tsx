import { useEffect, useRef, useState, type ReactNode } from "react";
import { transport, type view } from "../transport";

// The ⌘K palette, as the Search board's Actions pane draws it: every action
// with its shell command at the right, the matched runes marked, ↑↓ to
// move, ⏎ to run, tab to copy the command. Rows come from Go, ranked the
// dash's way.
export function Palette({ stackKey, selected, onPick, onClose }: { stackKey: string; selected: string; onPick: (row: view.PaletteRow) => void; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [rows, setRows] = useState<view.PaletteRow[]>([]);
  const [at, setAt] = useState(0);
  // fresh is false while the rows shown are for an older query, so Enter
  // never runs a row the person has typed past.
  const [fresh, setFresh] = useState(false);
  const box = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let gone = false;
    setFresh(false);
    transport().then(async (t) => {
      const p = await t.palette(stackKey, selected, query);
      if (!gone) {
        setRows(p.rows ?? []);
        setAt(0);
        setFresh(true);
      }
    });
    return () => {
      gone = true;
    };
  }, [stackKey, selected, query]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The palette owns the keyboard while open; ⌘K still closes it.
      if (!((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k")) e.stopPropagation();
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          onClose();
          break;
        case "ArrowDown":
          e.preventDefault();
          setAt((i) => Math.min(i + 1, rows.length - 1));
          break;
        case "ArrowUp":
          e.preventDefault();
          setAt((i) => Math.max(i - 1, 0));
          break;
        case "Enter":
          e.preventDefault();
          if (fresh && rows[at]) onPick(rows[at]);
          break;
        case "Tab": {
          e.preventDefault();
          const r = rows[at];
          if (fresh && r?.cli) {
            void navigator.clipboard?.writeText(r.cli);
            transport().then((t) => t.say(`copied the command ${r.cli}`, false));
            onClose();
          }
          break;
        }
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [rows, at, fresh, onPick, onClose]);

  return (
    <div className="scrim top" onClick={onClose}>
      <section className="pal" role="dialog" aria-label="Command palette" onClick={(e) => e.stopPropagation()}>
        <div className="in">
          <span className="dim">⌘K</span>
          <input ref={box} autoFocus aria-label="Do what?" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="do what?" />
        </div>
        {rows.length > 0 && <div className="grp">Actions</div>}
        {rows.length === 0 && <div className="none dim">nothing does that</div>}
        <div className="list">
          {rows.map((r, i) => (
            <div key={r.label} className={"sr" + (i === at ? " on" : "")} onMouseEnter={() => setAt(i)} onClick={() => onPick(r)} role="option" aria-selected={i === at}>
              <span />
              <span>
                <Marked text={r.label} at={r.at ?? []} />
                {r.note && (
                  <small className={r.bad ? " warn" : ""}>
                    {" "}
                    · {r.note}
                  </small>
                )}
              </span>
              <span>
                <span className="cmd">{r.cli}</span>
              </span>
            </div>
          ))}
        </div>
        <div className="ft">
          <span className="w">Every row is a shell command. The right column shows it.</span>
          <span className="grow" />
          <span>
            <kbd>⏎</kbd>run <kbd>tab</kbd>copy command
          </span>
        </div>
      </section>
    </div>
  );
}

function Marked({ text, at }: { text: string; at: number[] }) {
  if (at.length === 0) return <>{text}</>;
  const set = new Set(at);
  const runes = Array.from(text);
  const out: ReactNode[] = [];
  let run = "";
  let marked = false;
  let k = 0;
  const flush = () => {
    if (!run) return;
    out.push(marked ? <span key={k++} className="hl2">{run}</span> : run);
    run = "";
  };
  runes.forEach((ch, i) => {
    const m = set.has(i);
    if (m !== marked) {
      flush();
      marked = m;
    }
    run += ch;
  });
  flush();
  return <>{out}</>;
}
