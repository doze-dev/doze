import { useEffect, useRef, useState } from "react";
import { askAction, setConsole, useSlice } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "./keys";

// A module's console tab, as the ConsoleTab board frames it: one frame
// with a top line saying what is shown, and inside it the tree of rows,
// columns and blocks the module published, each block resolved by Go for
// the rows selected here. ↑↓ move in the focused table, tab moves between
// tables, an action's letter runs it on the selected row.
export function ConsoleTab({ stackKey, inst, tab, title }: { stackKey: string; inst: string; tab: string; title: string }) {
  const { sel } = useSlice((s) => ({ sel: s.sel }));
  const [con, setCon] = useState<view.Console | null>(null);
  const [focus, setFocus] = useState("");

  useEffect(() => {
    void setConsole(tab, {});
    return () => {
      void setConsole("", {});
    };
  }, [stackKey, inst, tab]);

  // Go pushes the tab whenever the focus or the console changes, so a
  // selection needs no fetch: one is made only for the first paint, and
  // dropped if a push got there first, since it may be older than the push.
  useEffect(() => {
    let gone = false;
    let pushed = false;
    let off = () => {};
    transport().then(async (t) => {
      if (gone) return;
      off = t.on<view.Console>("console", (c) => {
        if (c.key === stackKey && c.name === inst && c.tab === tab) {
          pushed = true;
          setCon(c);
        }
      });
      const c = await t.console(stackKey, inst, tab, {});
      if (!gone && !pushed) setCon(c);
    });
    return () => {
      gone = true;
      off();
    };
  }, [stackKey, inst, tab]);

  const tables = con ? collect(con.body, "table") : [];
  const actions = con ? collect(con.body, "actions").flatMap((n) => n.actions ?? []) : [];
  const focused = tables.find((t) => t.id === focus) ?? tables[0];

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;
      if (!focused) return;
      const n = focused.rows?.length ?? 0;
      const at = sel[focused.id] ?? focused.selected ?? 0;
      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          if (at < n - 1) void setConsole(tab, { ...sel, [focused.id]: at + 1 });
          return;
        case "ArrowUp":
          e.preventDefault();
          if (at > 0) void setConsole(tab, { ...sel, [focused.id]: at - 1 });
          return;
        case "]":
        case "[":
          // The next or the previous table; Tab stays the browser's, for
          // moving between controls.
          if (tables.length > 1) {
            e.preventDefault();
            e.stopPropagation();
            const i = tables.findIndex((t) => t.id === focused.id);
            setFocus(tables[(i + (e.key === "]" ? 1 : tables.length - 1)) % tables.length].id);
          }
          return;
      }
      if (e.key.length === 1) {
        const a = actions.find((a) => a.key === e.key.toUpperCase() && e.key === e.key.toUpperCase());
        if (a) {
          e.preventDefault();
          void askAction(stackKey, inst, a);
        }
      }
    };
    // Before the page's own handler, so tab moves between tables first.
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [focused, tables, actions, sel, tab, stackKey, inst]);

  if (!con) {
    return (
      <div className="empty">
        <p className="dim">reading the console…</p>
      </div>
    );
  }
  return (
    <>
      {con.err && (
        <div className="banner bad" role="alert">
          <span className="dot r" aria-hidden />
          <div className="t">
            <b>{con.err}</b>
          </div>
        </div>
      )}
      <div className="frame" data-tab={tab}>
        <div className="ctop">
          <span>{inst}</span>
          <span className="crumb">{title && title !== "_" ? "· " + title : "· console"}</span>
          {con.state && !con.awake && !con.missing && <span className="dim">· {con.state}: the console never wakes it; what it last showed is kept</span>}
          <span className="grow" />
          <span className="dim">{con.note}</span>
        </div>
        {con.missing ? (
          <div className="empty">
            <p className="dim">the module has no such tab</p>
          </div>
        ) : (
          <div className="con">
            <Block n={con.body} focused={focused?.id ?? ""} sel={sel} onPick={(id, i) => void setConsole(tab, { ...sel, [id]: i })} onFocus={setFocus} onAction={(a) => void askAction(stackKey, inst, a)} />
          </div>
        )}
      </div>
      <Keys keys={(con.keys ?? []) as Array<[string, string]>} note="from the Overview tab: Console, or ⏎ on an asleep module" />
    </>
  );
}

function hasStream(n: view.Node): boolean {
  return n.kind === "stream" || (n.children ?? []).some(hasStream);
}

function collect(n: view.Node, kind: string): view.Node[] {
  const out: view.Node[] = [];
  const walk = (x: view.Node) => {
    if (x.kind === kind) out.push(x);
    for (const c of x.children ?? []) walk(c);
  };
  walk(n);
  return out;
}

interface BlockProps {
  n: view.Node;
  /** dir is the parent's direction: a table in a column is as tall as its rows. */
  dir?: "row" | "col";
  focused: string;
  sel: Record<string, number>;
  onPick: (id: string, i: number) => void;
  onFocus: (id: string) => void;
  onAction: (a: view.Action) => void;
}

function Block(p: BlockProps) {
  const { n } = p;
  const style = { flex: n.flexible ? `${n.weight} 1 0` : "0 0 auto", minWidth: 0, minHeight: 0 } as const;
  if (n.missing) {
    return (
      <section className="card dim" style={style}>
        {n.missing}
      </section>
    );
  }
  switch (n.kind) {
    case "row":
    case "col": {
      // A row of tables in a column is as tall as its tallest table; only a
      // row with a stream in it takes the room left.
      const rowStyle = n.kind === "row" && p.dir === "col" && !hasStream(n) ? { ...style, flex: "0 0 auto" } : style;
      return (
        <div className={"con-" + n.kind} style={rowStyle}>
          {(n.children ?? []).map((c, i) => (
            <Block key={c.id || i} {...p} n={c} dir={n.kind as "row" | "col"} />
          ))}
        </div>
      );
    }
    case "table":
      return <TableBlock {...p} style={p.dir === "col" ? { ...style, flex: "0 1 auto" } : style} />;
    case "stat":
      return (
        <div className="cstats" style={style} aria-label={n.title || "counters"}>
          {(n.stats ?? []).map((s) => (
            <div key={s.label} className="cstat">
              <span>{s.label}</span>
              <b className={"tone-" + (s.tone || "bright")}>{s.value}</b>
            </div>
          ))}
        </div>
      );
    case "detail": {
      const rows = (n.detail ?? []) as string[][];
      const last = rows[rows.length - 1];
      return (
        <section className="card" style={style}>
          <h3>{n.title}</h3>
          {rows.length === 0 && <div className="dim">nothing selected</div>}
          {rows.length > 0 && last && (
            <>
              <div className="mono bright" style={{ whiteSpace: "pre-wrap", lineHeight: "19px" }}>
                {last[1]}
              </div>
              <div className="dim" style={{ fontSize: "var(--text-sm)" }}>
                {rows
                  .slice(0, -1)
                  .map(([k, v]) => (k ? `${k.toLowerCase()} ${v}` : v))
                  .join(" · ")}
              </div>
            </>
          )}
        </section>
      );
    }
    case "stream":
      return <StreamBlock n={n} style={p.dir === "col" && !n.flexible ? { ...style, flex: "0 1 auto" } : style} />;
    case "actions":
      return (
        <section className="card" style={style}>
          <h3>{n.title}</h3>
          {(n.actions ?? []).map((a) => (
            <button key={a.id} className={"cact" + (a.destructive ? " d" : "")} onClick={() => p.onAction(a)} title={a.resource ? `on ${a.resource}` : undefined}>
              <kbd>{a.key}</kbd>
              {a.label}
            </button>
          ))}
          {(n.actions ?? []).length === 0 && <div className="dim">none</div>}
        </section>
      );
    case "note":
      return (
        <section className="card" style={style}>
          <h3>{n.title}</h3>
          <div className="dim">{n.text}</div>
        </section>
      );
    case "chart":
      return (
        <section className="card" style={style}>
          <h3>{n.title}</h3>
          <div className="big">{n.last ?? "—"}</div>
          <Spark values={n.values ?? []} />
        </section>
      );
  }
  return null;
}

function cols(n: view.Node): string {
  const cs = n.cols ?? [];
  // Each column at least a few characters wide, so a right-aligned number
  // has somewhere to align to, as the board's fixed widths give it; the
  // last takes the rest.
  return cs.map((_, i) => (i === cs.length - 1 ? "minmax(0,1fr)" : "minmax(5ch, max-content)")).join(" ");
}

function Header({ n }: { n: view.Node }) {
  return (
    <div className="ctr h" style={{ "--c": cols(n) } as React.CSSProperties}>
      {(n.cols ?? []).map((c) => (
        <span key={c.key} className={c.align === "right" ? "r" : ""}>
          {c.title}
        </span>
      ))}
    </div>
  );
}

function Cells({ n, r }: { n: view.Node; r: view.CRow }) {
  return (
    <>
      {(r.cells ?? []).map((c, j) => (
        <span key={j} className={(n.cols?.[j]?.align === "right" ? "r " : "") + "tone-" + (c.tone || "text")}>
          {c.text}
        </span>
      ))}
    </>
  );
}

function TableBlock(p: BlockProps & { style: React.CSSProperties }) {
  const { n, style } = p;
  const selected = p.sel[n.id] ?? n.selected ?? 0;
  return (
    <div className={"ctable" + (p.focused === n.id ? " focused" : "")} style={style} role="grid" aria-label={n.title || n.id}>
      <Header n={n} />
      <div className="rows">
        {(n.rows ?? []).map((r, i) => (
          <button
            key={r.key + i}
            className={"ctr row" + (i === selected ? " on" : "") + (r.tone === "dim" ? " faded" : "")}
            style={{ "--c": cols(n) } as React.CSSProperties}
            onClick={() => {
              p.onFocus(n.id);
              p.onPick(n.id, i);
            }}
            role="row"
            aria-selected={i === selected}
          >
            <Cells n={n} r={r} />
          </button>
        ))}
        {(n.rows ?? []).length === 0 && (
          <div className="ctr dim" style={{ "--c": "1fr" } as React.CSSProperties}>
            <span>nothing yet</span>
          </div>
        )}
      </div>
    </div>
  );
}

function StreamBlock({ n, style }: { n: view.Node; style: React.CSSProperties }) {
  const rows = n.rows ?? [];
  // The newest line stays in view, as the dash drew the tail.
  const list = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = list.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [rows.length, rows[rows.length - 1]?.key]);
  return (
    <div className="ctable" style={style} aria-label={n.title || n.id}>
      <div className="ctr h" style={{ "--c": "1fr" } as React.CSSProperties}>
        <span>{n.title}</span>
      </div>
      <Header n={n} />
      <div ref={list} className="rows">
        {rows.map((r, i) => (
          <div key={r.key + i} className="ctr mono" style={{ "--c": cols(n) } as React.CSSProperties}>
            <Cells n={n} r={r} />
          </div>
        ))}
        {rows.length === 0 && (
          <div className="ctr dim" style={{ "--c": "1fr" } as React.CSSProperties}>
            <span>waiting for the first line</span>
          </div>
        )}
      </div>
    </div>
  );
}

function Spark({ values }: { values: number[] }) {
  const w = 240;
  const h = 40;
  if (values.length < 2) return <svg width={w} height={h} className="sparkline" />;
  const max = Math.max(...values, 1);
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * w},${h - (v / max) * (h - 2) - 1}`).join(" ");
  return (
    <svg width={w} height={h} className="sparkline" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none">
      <polyline points={pts} fill="none" stroke="var(--accent)" strokeWidth="1.5" />
    </svg>
  );
}

/** Extends draws the module's overview blocks that go after one card. */
export function Extends({ after, inst, stackKey }: { after: string; inst: view.Instance; stackKey: string }) {
  const blocks = (inst.extend ?? []).filter((e) => e.after === after);
  if (blocks.length === 0) return null;
  return (
    <>
      {blocks.map((e, i) => (
        <section key={i} className="card" style={{ padding: 0, overflow: "hidden" }}>
          {e.node.title && <h3 style={{ padding: "12px 14px 0" }}>{e.node.title}</h3>}
          <Block n={{ ...e.node, title: "", flexible: false }} focused="" sel={{}} onPick={() => {}} onFocus={() => {}} onAction={(a) => void askAction(stackKey, inst.name, a)} />
        </section>
      ))}
    </>
  );
}
