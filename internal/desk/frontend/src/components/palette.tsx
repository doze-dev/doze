import { useEffect, useRef, useState, type ReactNode } from "react";
import { transport, type view } from "../transport";

// ⌘K, as the Palette boards draw it. Nothing typed: Needs you, Recent, Do.
// A name finds services and stacks everywhere; a verb then a name finds
// what the verb applies to; nothing found says so with the closest name.
// ↑↓ move, ⏎ does the row, tab shows its actions, ⌘⏎ runs the first,
// esc clears then closes. Every row and word comes from Go.
export function Palette({ stackKey, onPick, onClose }: { stackKey: string; onPick: (d: view.Doing) => void; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [p, setP] = useState<view.Palette | null>(null);
  const [at, setAt] = useState(0);
  const [open, setOpen] = useState(false); // the row's actions shown
  // fresh is false while the rows shown are for an older query, so Enter
  // never runs a row the person has typed past.
  const [fresh, setFresh] = useState(false);
  const box = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let gone = false;
    setFresh(false);
    transport().then(async (t) => {
      const got = await t.palette(stackKey, "", query);
      if (!gone) {
        setP(got);
        setAt(0);
        setOpen(false);
        setFresh(true);
      }
    });
    return () => {
      gone = true;
    };
  }, [stackKey, query]);

  const rows = (p?.sections ?? []).flatMap((s) => s.rows ?? []);
  const row = rows[at];

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The palette owns the keyboard while open; ⌘K still closes it.
      if (!((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k")) e.stopPropagation();
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          if (open) setOpen(false);
          else if (query) setQuery("");
          else onClose();
          break;
        case "ArrowDown":
          e.preventDefault();
          setAt((i) => Math.min(i + 1, rows.length - 1));
          setOpen(false);
          break;
        case "ArrowUp":
          e.preventDefault();
          setAt((i) => Math.max(i - 1, 0));
          setOpen(false);
          break;
        case "Enter":
          e.preventDefault();
          if (!fresh) break;
          if ((e.metaKey || e.ctrlKey) && row?.acts?.[0]) onPick(row.acts[0].do);
          else if (row) onPick(row.do);
          else if (p?.nothing?.fill) setQuery(p.nothing.fill);
          else if (p?.nothing?.do) onPick(p.nothing.do);
          break;
        case "Tab":
          e.preventDefault();
          if (row?.acts?.length) setOpen((o) => !o);
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [rows, row, at, fresh, query, open, p, onPick, onClose]);

  let i = -1;
  return (
    <div className="scrim top" onClick={onClose}>
      <section className="pal" role="dialog" aria-modal="true" aria-label={query ? `Search or do: ${query}` : "Search or do"} onClick={(e) => e.stopPropagation()}>
        <div className="in">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" />
            <path d="M10.5 10.5 14 14" />
          </svg>
          <input ref={box} autoFocus aria-label="A service, a stack, or what to do" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="A service, a stack, or what to do…" />
        </div>
        {(p?.sections ?? []).map((s) => (
          <div key={s.title} role="group" aria-label={s.title}>
            <div className="ph">{s.title}</div>
            {(s.rows ?? []).map((r) => {
              i++;
              const here = i;
              const on = here === at;
              return (
                <button key={s.title + r.label + r.note} className={"it" + (on ? " on" : "")} onMouseEnter={() => setAt(here)} onClick={() => onPick(r.do)} role="option" aria-selected={on}>
                  <Glyph r={r} />
                  <b>
                    <Marked text={r.label} at={r.at ?? []} />
                  </b>
                  {r.note && <span className={"m" + (r.bad ? " warn" : "")}>{r.note}</span>}
                  {on && open && (r.acts ?? []).length > 0 ? (
                    <span className="acts">
                      {(r.acts ?? []).map((a) => (
                        <kbd
                          key={a.label}
                          onClick={(e) => {
                            e.stopPropagation();
                            onPick(a.do);
                          }}
                        >
                          {a.label}
                        </kbd>
                      ))}
                    </span>
                  ) : (
                    <span className="k">{r.key}</span>
                  )}
                </button>
              );
            })}
          </div>
        ))}
        {p?.nothing && fresh && (
          <button className="it" onClick={() => (p.nothing?.fill ? setQuery(p.nothing.fill) : p.nothing?.do && onPick(p.nothing.do))}>
            <span className="dim">?</span>
            <span className="m">
              Nothing called <b>{p.nothing.name}</b> in {p.nothing.where}.
              {p.nothing.closest && (
                <>
                  {" "}
                  Closest: <b>{p.nothing.closest}</b> ({p.nothing.why}).
                </>
              )}
            </span>
            {p.nothing.key && <span className="k">{p.nothing.key}</span>}
          </button>
        )}
        <div className="foot">
          {p?.nothing ? (
            <span>a verb alone lists what it can apply to; a name alone opens it with its actions</span>
          ) : query ? (
            <>
              <span>
                <kbd>⏎</kbd>do it
              </span>
              <span>
                <kbd>esc</kbd>clear
              </span>
              <span className="grow" />
              <span>verbs: up · down · sleep · wake · restart · pin · logs · copy · open · run</span>
            </>
          ) : (
            <>
              <span>
                <kbd>↑↓</kbd>move
              </span>
              <span>
                <kbd>⏎</kbd>open
              </span>
              <span>
                <kbd>⌘⏎</kbd>first action
              </span>
              <span>
                <kbd>tab</kbd>actions of a row
              </span>
              <span className="grow" />
              <span>type a name, or a verb then a name: sleep db · logs api · copy web</span>
            </>
          )}
        </div>
      </section>
    </div>
  );
}

function Glyph({ r }: { r: view.PaletteRow }) {
  if (r.glyph === "dot") return <span className={"dot " + (r.tone ?? "")} aria-hidden />;
  return (
    <span className={r.tone ?? "dim"} aria-hidden>
      {r.glyph}
    </span>
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
    out.push(
      marked ? (
        <span key={k++} className="hl2">
          {run}
        </span>
      ) : (
        run
      ),
    );
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
