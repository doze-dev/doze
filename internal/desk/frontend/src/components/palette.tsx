import { useEffect, useRef, useState, type ReactNode } from "react";
import { transport, type view } from "../transport";

// ⌘K, rethought (2026-11-06): commands first, for where you are. Nothing
// typed: what is broken, three commands for the page on screen, what you
// did lately, and every stack. Tab on a stack or a service narrows to its
// commands (the breadcrumb says where; ⌫ on an empty box steps out). A
// name then words is a command for it ("api restart") or its logs
// searched ("api timeout"); a verb then a name still works. A copy says
// so in place and stays open. Every row and word comes from Go.
export function Palette({ stackKey, instance, onPick, onClose }: { stackKey: string; instance: string; onPick: (d: view.Doing) => void; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState("");
  const [p, setP] = useState<view.Palette | null>(null);
  const [at, setAt] = useState(0);
  const [done, setDone] = useState(""); // the row just copied, said in place
  // fresh is false while the rows shown are for an older query, so Enter
  // never runs a row the person has typed past.
  const [fresh, setFresh] = useState(false);
  const box = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let gone = false;
    setFresh(false);
    transport().then(async (t) => {
      const got = await t.palette(stackKey, instance, scope, query);
      if (!gone) {
        setP(got);
        setAt(0);
        setFresh(true);
      }
    });
    return () => {
      gone = true;
    };
  }, [stackKey, instance, scope, query]);

  const rows = (p?.sections ?? []).flatMap((s) => s.rows ?? []);
  const row = rows[at];

  const enter = (r: view.PaletteRow | undefined) => {
    if (!r?.enter) return false;
    setScope(r.enter);
    setQuery("");
    setDone("");
    box.current?.focus();
    return true;
  };
  const out = () => {
    setScope((sc) => (sc.includes("#") ? sc.slice(0, sc.indexOf("#")) : ""));
    setDone("");
  };
  const choose = (r: view.PaletteRow) => {
    if (!r.enter) {
      void transport().then((t) => t.rememberCommand({ glyph: r.glyph, label: r.label, note: r.note ?? "", do: r.do }));
    }
    if (r.do.kind === "copy") {
      // Said in place; the palette stays for the next thing.
      void transport().then((t) => t.copy(r.do.text ?? ""));
      setDone(r.label);
      return;
    }
    onPick(r.do);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The palette owns the keyboard while open; ⌘K still closes it.
      if (!((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k")) e.stopPropagation();
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          if (query) setQuery("");
          else if (scope) out();
          else onClose();
          break;
        case "Backspace":
          if (!query && scope) {
            e.preventDefault();
            out();
          }
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
          if (!fresh) break;
          if (row) choose(row);
          else if (p?.nothing?.fill) setQuery(p.nothing.fill);
          else if (p?.nothing?.do) onPick(p.nothing.do);
          break;
        case "Tab":
          e.preventDefault();
          if (e.shiftKey) out();
          else enter(row);
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [rows, row, at, fresh, query, scope, p, onPick, onClose]);

  let i = -1;
  const crumbs = p?.crumbs ?? [];
  return (
    <div className="scrim top" onClick={onClose}>
      <section className="pal" role="dialog" aria-modal="true" aria-label={query ? `Search or do: ${query}` : "Search or do"} onClick={(e) => e.stopPropagation()}>
        <div className="in">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" />
            <path d="M10.5 10.5 14 14" />
          </svg>
          {scope &&
            crumbs.map((c) => (
              <button key={c} className="crumb" onClick={out} title="⌫ steps out">
                {c} ›
              </button>
            ))}
          <input
            ref={box}
            autoFocus
            aria-label="A service, a stack, or what to do"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setDone("");
            }}
            placeholder={scope ? `What to do with ${crumbs[crumbs.length - 1] ?? "it"}…` : "A service, a stack, or what to do…"}
          />
        </div>
        <div className="pbody">
          {(p?.sections ?? []).map((s) => (
            <div key={s.title} role="group" aria-label={s.title}>
              <div className="ph">{s.title}</div>
              {(s.rows ?? []).map((r) => {
                i++;
                const here = i;
                const on = here === at;
                const copied = done === r.label;
                return (
                  <button key={s.title + r.label + r.note} className={"it" + (on ? " on" : "")} onMouseEnter={() => setAt(here)} onClick={() => choose(r)} role="option" aria-selected={on}>
                    <Glyph r={r} />
                    <b>
                      <Marked text={r.label} at={r.at ?? []} />
                    </b>
                    {r.note && <span className={"m" + (r.bad ? " warn" : "")}>{r.note}</span>}
                    {copied ? (
                      <span className="k ok">Copied ✓</span>
                    ) : on && r.enter ? (
                      <span className="k">
                        <kbd>tab</kbd> its commands
                      </span>
                    ) : on && r.cli && !r.do.ask ? (
                      <span className="k mono" title="the same from a terminal">
                        {r.cli}
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
        </div>
        <div className="foot">
          <span>
            <kbd>↑↓</kbd>move
          </span>
          <span>
            <kbd>⏎</kbd>do it
          </span>
          {scope ? (
            <span>
              <kbd>⌫</kbd>step out
            </span>
          ) : (
            <span>
              <kbd>tab</kbd>into a stack or service
            </span>
          )}
          <span className="grow" />
          <span>{scope ? "the commands for " + (crumbs[crumbs.length - 1] ?? "it") : "try: api restart · db psql · api timeout · theme teal"}</span>
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
