import { useEffect, useState } from "react";
import { askAction, setConsole, useSlice } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "./keys";

// A module's console tab, as the PgActivity board draws it: the tree of
// rows, columns and blocks the module published, each block resolved by Go
// for the rows selected here. ↑↓ move in the focused table, tab moves
// between tables, an action's letter runs it on the selected row.
export function ConsoleTab({ stackKey, inst, tab }: { stackKey: string; inst: string; tab: string }) {
  const { sel } = useSlice((s) => ({ sel: s.sel }));
  const [con, setCon] = useState<view.Console | null>(null);
  const [focus, setFocus] = useState("");

  useEffect(() => {
    void setConsole(tab, {});
    return () => {
      void setConsole("", {});
    };
  }, [stackKey, inst, tab]);

  useEffect(() => {
    let gone = false;
    let off = () => {};
    transport().then(async (t) => {
      if (gone) return;
      off = t.on<view.Console>("console", (c) => {
        if (c.key === stackKey && c.name === inst && c.tab === tab) setCon(c);
      });
      const c = await t.console(stackKey, inst, tab, sel);
      if (!gone) setCon(c);
    });
    return () => {
      gone = true;
      off();
    };
  }, [stackKey, inst, tab, sel]);

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
        case "Tab":
          if (tables.length > 1) {
            e.preventDefault();
            e.stopPropagation();
            const i = tables.findIndex((t) => t.id === focused.id);
            setFocus(tables[(i + 1) % tables.length].id);
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

  if (!con) return <div className="empty dim">reading the console…</div>;
  return (
    <>
      {con.err && (
        <p className="notice bad" role="alert">
          {con.err}
        </p>
      )}
      {con.missing && <div className="empty dim">the module has no such tab</div>}
      {!con.awake && !con.missing && (
        <div className="dim" style={{ padding: "10px 20px 0" }}>
          {inst} is {con.state || "asleep"} · the console never wakes it; what it last showed is kept
        </div>
      )}
      <div className="con" data-tab={tab}>
        <Block n={con.body} focused={focused?.id ?? ""} sel={sel} onPick={(id, i) => void setConsole(tab, { ...sel, [id]: i })} onFocus={setFocus} onAction={(a) => void askAction(stackKey, inst, a)} />
      </div>
      <Keys keys={(con.keys ?? []) as Array<[string, string]>} note={con.note} />
    </>
  );
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
    case "col":
      return (
        <div className={"con-" + n.kind} style={style}>
          {(n.children ?? []).map((c, i) => (
            <Block key={c.id || i} {...p} n={c} />
          ))}
        </div>
      );
    case "table":
      return <TableBlock {...p} style={style} />;
    case "stat":
      return (
        <div className="ctr" style={style} aria-label={n.title || "counters"}>
          {(n.stats ?? []).map((s) => (
            <div key={s.label}>
              <span>{s.label}</span>
              <b className={"tone-" + (s.tone || "text")}>{s.value}</b>
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
              <div className="dim" style={{ fontSize: 12 }}>
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
      return <StreamBlock n={n} style={style} />;
    case "actions":
      return (
        <section className="card" style={style}>
          <h3>{n.title}</h3>
          {(n.actions ?? []).map((a) => (
            <button key={a.id} className={"act" + (a.destructive ? " d" : "")} onClick={() => p.onAction(a)} title={a.resource ? `on ${a.resource}` : undefined}>
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

function TableBlock(p: BlockProps & { style: React.CSSProperties }) {
  const { n, style } = p;
  const selected = p.sel[n.id] ?? n.selected ?? 0;
  return (
    <div className={"tbl con-tbl" + (p.focused === n.id ? " focused" : "")} style={{ ...style, margin: 0, maxHeight: "100%" }} role="grid" aria-label={n.title || n.id}>
      <div className="tr h" style={{ "--c": cols(n) } as React.CSSProperties}>
        {(n.cols ?? []).map((c) => (
          <span key={c.key} className={c.align === "right" ? "r" : ""}>
            {c.title}
          </span>
        ))}
      </div>
      {(n.rows ?? []).map((r, i) => (
        <button
          key={r.key + i}
          className={"tr row" + (i === selected ? " on" : "") + (r.tone === "dim" ? " faded" : "")}
          style={{ "--c": cols(n) } as React.CSSProperties}
          onClick={() => {
            p.onFocus(n.id);
            p.onPick(n.id, i);
          }}
          role="row"
          aria-selected={i === selected}
        >
          {(r.cells ?? []).map((c, j) => (
            <span key={j} className={(n.cols?.[j]?.align === "right" ? "r " : "") + "tone-" + (c.tone || "text")}>
              {c.text}
            </span>
          ))}
        </button>
      ))}
      {(n.rows ?? []).length === 0 && (
        <div className="tr dim" style={{ "--c": "1fr" } as React.CSSProperties}>
          <span>nothing yet</span>
        </div>
      )}
    </div>
  );
}

function StreamBlock({ n, style }: { n: view.Node; style: React.CSSProperties }) {
  const rows = n.rows ?? [];
  return (
    <section className="card con-stream" style={{ ...style, padding: 0 }} aria-label={n.title || n.id}>
      <h3 style={{ padding: "12px 14px 0" }}>{n.title}</h3>
      <div className="tbl" style={{ margin: 0, border: 0, borderRadius: 0, background: "transparent" }}>
        <div className="tr h" style={{ "--c": cols(n) } as React.CSSProperties}>
          {(n.cols ?? []).map((c) => (
            <span key={c.key} className={c.align === "right" ? "r" : ""}>
              {c.title}
            </span>
          ))}
        </div>
        {rows.map((r, i) => (
          <div key={r.key + i} className="tr mono" style={{ "--c": cols(n) } as React.CSSProperties}>
            {(r.cells ?? []).map((c, j) => (
              <span key={j} className={(n.cols?.[j]?.align === "right" ? "r " : "") + "tone-" + (c.tone || "text")}>
                {c.text}
              </span>
            ))}
          </div>
        ))}
        {rows.length === 0 && (
          <div className="tr dim" style={{ "--c": "1fr" } as React.CSSProperties}>
            <span>waiting for the first line</span>
          </div>
        )}
      </div>
    </section>
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
