import { useLayoutEffect, useRef, useState } from "react";
import { go, run, select } from "../store";
import type { view } from "../transport";
import { Dot, PinGlyph, toggle } from "./overview";

// The Map and the Grid, as MapFlow, MapFlowSelected and MapGrid draw them.
// Go places every card and every line (view.MapView, view.GridView) and
// says every word; this draws them and keeps what is lit or under the
// pointer.

/** MapFlow is the stack as calls left to right, a lane for what nothing calls, and the shared band. lit is the selected card, "" for none. */
export function MapFlow({ m, rows, lit, onLit, stackKey }: { m: view.MapView; rows: view.Row[]; lit: string; onLit: (name: string) => void; stackKey: string }) {
  const byName = new Map(rows.map((r) => [r.name, r]));
  const nodes = m.nodes ?? [];
  const shared = m.shared ?? [];
  // A map a little wider than its box is drawn a little smaller, so the
  // whole of it shows at 1280 wide; a much wider one scrolls instead.
  const scroller = useRef<HTMLDivElement>(null);
  const [fit, setFit] = useState(1);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => {
      const r = el.clientWidth / m.w;
      setFit(r < 1 && r >= 0.88 ? r : 1);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [m.w]);
  const litNode = nodes.find((n) => n.name === lit);
  const litShared = shared.find((s) => s.name === lit);
  // What a selection keeps lit: a card's callers, what it calls and what it
  // uses; a band card's users.
  const on = new Set<string>();
  if (litNode) [litNode.name, ...(litNode.calls ?? []), ...(litNode.callers ?? []), ...(litNode.uses ?? [])].forEach((x) => on.add(x));
  if (litShared) [litShared.name, ...(litShared.users ?? [])].forEach((x) => on.add(x));
  const any = !!(litNode || litShared);
  const faded = (name: string) => any && !on.has(name);
  const edgeOn = (e: view.MapEdge) => !any || e.from === lit || e.to === lit;
  const lines = (m.lines ?? []).filter((l) => (litNode && l.from === lit) || (litShared && l.to === lit));
  const open = (name: string) => {
    const r = byName.get(name);
    if (r && !r.down) void go({ key: stackKey, page: "instance", instance: name });
  };
  const pick = (name: string) => {
    onLit(name);
    void select(name);
  };
  const card = (name: string) => ({
    "data-name": name,
    onClick: () => pick(name),
    onDoubleClick: () => open(name),
    onKeyDown: (e: React.KeyboardEvent) => {
      // A card reached by keyboard: ⏎ selects it, and opens it once
      // selected; space wakes or sleeps it, as on every view.
      if (e.key === "Enter") {
        e.preventDefault();
        if (lit === name) open(name);
        else pick(name);
      } else if (e.key === " ") {
        e.preventDefault();
        const r = byName.get(name);
        if (r) toggle(stackKey, r);
      }
    },
  });
  const detail = litNode?.detail ?? litShared?.detail;
  const litRow = byName.get(lit);
  return (
    <div className="map flow" role="group" aria-label="The stack as a map: who calls whom, left to right, with the shared services in a band below">
      <div className="mscroll" ref={scroller}>
        <div className="canvas" style={{ width: m.w, height: m.h, zoom: fit }}>
          {m.band > 0 && <div className="band" style={{ top: m.band }} />}
          <svg className="edges" width={m.w} height={m.h} aria-hidden="true">
            <defs>
              <marker id="flow-ar" viewBox="0 0 12 12" refX="11" refY="6" markerWidth="12" markerHeight="12" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
                <path d="M1.5 1.5L11 6L1.5 10.5L4.2 6Z" style={{ fill: "var(--accent)", stroke: "var(--accent)", strokeWidth: 1, strokeLinejoin: "round" }} />
              </marker>
              <marker id="flow-ard" viewBox="0 0 12 12" refX="11" refY="6" markerWidth="12" markerHeight="12" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
                <path d="M1.5 1.5L11 6L1.5 10.5L4.2 6Z" style={{ fill: "var(--line2)", stroke: "var(--line2)", strokeWidth: 1, strokeLinejoin: "round" }} />
              </marker>
            </defs>
            {(m.edges ?? []).map((e) => (
              <path key={e.from + ">" + e.to} d={e.d} className={"edge" + (edgeOn(e) ? "" : " dim")} data-from={e.from} data-to={e.to} markerEnd={edgeOn(e) ? "url(#flow-ar)" : "url(#flow-ard)"} />
            ))}
            {lines.map((l) => (
              <path key={l.from + ">" + l.to} d={l.d} className="use" data-from={l.from} data-to={l.to} markerEnd="url(#flow-ar)" />
            ))}
          </svg>
          {(m.caps ?? []).map((c) => (
            <div key={c.text} className={"cap" + (m.band > 0 && c.y > m.band ? " inband" : "")} style={{ left: c.x, top: c.y }}>
              {c.text}
              <span>{c.sub}</span>
            </div>
          ))}
          {nodes.map((n) => {
            const r = byName.get(n.name);
            if (!r) return null;
            const sel = n.name === lit;
            return (
              <button
                key={n.name}
                className={"node" + (sel ? " sel" : "") + (n.task ? " task" : "") + (r.trouble ? " bad" : "") + (faded(n.name) ? " faded" : "")}
                style={{ left: n.x, top: n.y, width: n.w, height: n.h }}
                aria-label={`${n.name}, ${r.look.word}${n.lang ? ", " + n.lang : ""}${n.uses?.length ? ", uses " + n.uses.join(", ") : ""}`}
                aria-pressed={sel}
                {...card(n.name)}
              >
                <div className="top">
                  <Dot r={r} />
                  <b>{n.name}</b>
                  {n.lang && <span className="lang">{n.lang}</span>}
                  {r.pinned && (
                    <span className="pin">
                      <PinGlyph />
                    </span>
                  )}
                  {r.mem && <span className="m">{r.mem}</span>}
                </div>
                <div className="chips">
                  {n.quiet && <span className={"chip quiet" + (n.task ? " " + toneOf(r.look.tone) : "")}>{n.quiet}</span>}
                  {(n.chips ?? []).map((c) => (
                    <span key={c} className={"chip" + (sel ? " hot" : "")}>
                      {c}
                    </span>
                  ))}
                  {(n.more ?? 0) > 0 && <span className="chip quiet">+{n.more}</span>}
                  {n.fold && (
                    <span className={"fold " + toneOf(n.fold.tone)} title={n.fold.title} aria-label={n.fold.title}>
                      {n.fold.glyph}
                    </span>
                  )}
                </div>
              </button>
            );
          })}
          {shared.map((s) => {
            const r = byName.get(s.name);
            if (!r) return null;
            const sel = s.name === lit;
            const hot = !!litNode && on.has(s.name);
            return (
              <button
                key={s.name}
                className={"inf" + (sel ? " sel" : "") + (hot ? " hot" : "") + (r.trouble ? " bad" : "") + (faded(s.name) ? " faded" : "")}
                style={{ left: s.x, top: s.y, width: s.w, height: s.h }}
                aria-label={`${s.name}, ${r.look.word}, ${s.what}, ${s.used}`}
                aria-pressed={sel}
                {...card(s.name)}
              >
                <div className="top">
                  <Dot r={r} />
                  <b>{s.name}</b>
                  {r.mem && <span className="m">{r.mem}</span>}
                </div>
                <div className="st">{s.what}</div>
                <div className="st">
                  <b>{s.used}</b>
                  {s.only && <> · {s.only}</>}
                </div>
              </button>
            );
          })}
        </div>
      </div>
      {/* The detail and the legend stay put while the drawing scrolls under them: Go's place for the detail is the top right, or lower where that is taken. */}
      {detail && litRow ? (
        <div className="detail" style={{ right: 12, top: m.detail.y }} role="region" aria-label={`${lit}, selected`}>
          <div className="h">
            <Dot r={litRow} />
            <b>{lit}</b>
            <span className="dim">{detail.head}</span>
            <span className="grow" />
            {!litRow.down && (
              <button className="btn quiet" onClick={() => open(lit)}>
                Open <kbd>⏎</kbd>
              </button>
            )}
          </div>
          {(detail.rows ?? []).map((d) => (
            <div key={d.k} className="dr">
              <span>{d.k}</span>
              <span className={toneOf(d.tone ?? "")}>
                {d.v}
                {d.dim && <span className="dim"> {d.dim}</span>}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="legend">
          <span>
            <i />
            calls, from references in doze.hcl
          </span>
          <span>chips · what it uses below</span>
          <span>
            <b className="ok">✓</b> its setup tasks ran
          </span>
          <span>click a card to draw its lines</span>
        </div>
      )}
    </div>
  );
}

/** toneOf is a Go tone as the class that colours it. */
function toneOf(t: string): string {
  switch (t) {
    case "good":
    case "ok":
      return "ok";
    case "bad":
      return "bad";
    case "warn":
      return "warn";
    case "cool":
      return "cool";
    case "dim":
      return "dim";
  }
  return "";
}

/** nextCard is the card an arrow key moves to from lit: the nearest one that way, the first when none is lit. */
export function nextCard(m: view.MapView, lit: string, key: string): string {
  const all = [...(m.nodes ?? []), ...(m.shared ?? [])];
  if (all.length === 0) return "";
  const from = all.find((c) => c.name === lit);
  if (!from) return all[0].name;
  const cx = from.x + from.w / 2;
  const cy = from.y + from.h / 2;
  let best = "";
  let score = Infinity;
  for (const c of all) {
    if (c.name === lit) continue;
    const dx = c.x + c.w / 2 - cx;
    const dy = c.y + c.h / 2 - cy;
    let along: number, across: number;
    switch (key) {
      case "ArrowRight":
        [along, across] = [dx, dy];
        break;
      case "ArrowLeft":
        [along, across] = [-dx, dy];
        break;
      case "ArrowDown":
        [along, across] = [dy, dx];
        break;
      default:
        [along, across] = [-dy, dx];
    }
    if (along <= 10) continue;
    const s = along + 2 * Math.abs(across);
    if (s < score) {
      score = s;
      best = c.name;
    }
  }
  return best || lit;
}

/** GridView is the stack as a table: a row needs each column it has a dot in. onRow says which row the crosshair is on. */
export function GridView({ g, rows, visible, stackKey, onRow }: { g: view.GridView; rows: view.Row[]; visible: Set<string>; stackKey: string; onRow: (name: string) => void }) {
  const byName = new Map(rows.map((r) => [r.name, r]));
  const cols = g.cols ?? [];
  const body = (g.rows ?? []).filter((r) => visible.has(r.name));
  const [cur, setCur] = useState<{ r: number; c: number } | null>(null);
  const [focused, setFocused] = useState<{ r: number; c: number }>({ r: 0, c: 0 });
  const box = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const [tipAt, setTipAt] = useState<{ left: number; top: number; arrow: number } | null>(null);
  const cell = cur ? body[cur.r]?.cells?.[cur.c] : undefined;
  const tip = cell?.tip;

  const move = (r: number, c: number) => {
    const nr = Math.max(0, Math.min(body.length - 1, r));
    const nc = Math.max(0, Math.min(cols.length - 1, c));
    setFocused({ r: nr, c: nc });
    setCur({ r: nr, c: nc });
    onRow(body[nr]?.name ?? "");
    box.current?.querySelector<HTMLElement>(`td[data-r="${nr}"][data-c="${nc}"]`)?.focus();
  };
  // The tooltip sits under its cell, its arrow on the cell, kept inside the box.
  useLayoutEffect(() => {
    if (!cur || !tip || !box.current) {
      setTipAt(null);
      return;
    }
    const td = box.current.querySelector<HTMLElement>(`td[data-r="${cur.r}"][data-c="${cur.c}"]`);
    if (!td) return;
    const b = box.current.getBoundingClientRect();
    const t = td.getBoundingClientRect();
    const w = tipRef.current?.offsetWidth ?? 320;
    const h = tipRef.current?.offsetHeight ?? 70;
    const mid = t.left - b.left + t.width / 2;
    let left = mid - 28;
    left = Math.max(8, Math.min(left, b.width - w - 8));
    let top = t.bottom - b.top + 8;
    if (top + h > b.height - 4) top = t.top - b.top - h - 8;
    setTipAt({ left, top, arrow: mid - left });
  }, [cur, tip]);

  const onKey = (e: React.KeyboardEvent) => {
    const r = focused.r;
    const c = focused.c;
    const row = body[r];
    switch (e.key) {
      case "ArrowRight":
        e.preventDefault();
        move(r, c + 1);
        break;
      case "ArrowLeft":
        e.preventDefault();
        move(r, c - 1);
        break;
      case "ArrowDown":
        e.preventDefault();
        move(r + 1, c);
        break;
      case "ArrowUp":
        e.preventDefault();
        move(r - 1, c);
        break;
      case "Enter": {
        e.preventDefault();
        const to = row?.cells?.[c]?.tip?.open;
        if (to) void go({ key: stackKey, page: "instance", instance: to });
        break;
      }
      case "o":
        e.preventDefault();
        if (row) void go({ key: stackKey, page: "instance", instance: row.name });
        break;
      case " ": {
        e.preventDefault();
        const rr = row && byName.get(row.name);
        if (rr) toggle(stackKey, rr);
        break;
      }
      case "l":
        e.preventDefault();
        if (row) void go({ key: stackKey, page: "logs", instance: row.name });
        break;
      case "p":
        e.preventDefault();
        if (row) void run(stackKey, "pin", row.name);
        break;
    }
  };
  const width = 300 + cols.reduce((s, c) => s + (c.kind === "app" ? 74 : c.kind === "shared" ? 68 : 136), 0);
  const heads = g.heads ?? [];
  let at = 0;
  const headSep = heads.map((h) => {
    const sep = !!cols[at]?.sep;
    at += h.span;
    return sep;
  });
  const colOn = (ci: number) => (cur && cur.c === ci ? " selc" : "");
  return (
    <div className="gridw" ref={box} role="region" aria-label="The stack as a grid: rows need columns">
      <div className="gscroll">
        <table className="g" role="grid" aria-label="What each service needs" style={{ minWidth: width }} onMouseLeave={() => setCur(null)} onKeyDown={onKey}>
          <colgroup>
            <col style={{ width: 300 }} />
            {cols.map((c) => (
              <col key={c.name} style={{ width: c.kind === "app" ? 74 : c.kind === "shared" ? 68 : 136 }} />
            ))}
            {/* What is left of the width, so the columns keep the board's widths. */}
            <col />
          </colgroup>
          <thead>
            <tr className="grph">
              <th className="rh">
                Service<span>needs →</span>
              </th>
              {heads.map((h, i) => (
                <th key={h.text} colSpan={h.span} className={headSep[i] ? "sep" : ""}>
                  {h.text}
                </th>
              ))}
              <th className="fill" aria-hidden />
            </tr>
            <tr>
              <th className="rh" aria-hidden />
              {cols.map((c, ci) => {
                const r = byName.get(c.name);
                return (
                  <th key={c.name} scope="col" className={(c.sep ? "sep" : "") + colOn(ci)} title={c.name}>
                    {c.kind === "tasks" ? (
                      <div className="n">
                        <b className="tasks">tasks</b>
                        <small>{c.sub}</small>
                      </div>
                    ) : (
                      <div className="n">
                        {r && <Dot r={r} />}
                        <b>{c.name}</b>
                        <small>{r?.mem || " "}</small>
                      </div>
                    )}
                  </th>
                );
              })}
              <th className="fill" aria-hidden />
            </tr>
          </thead>
          <tbody>
            {body.map((row, ri) => {
              const r = byName.get(row.name);
              return (
                <tr key={row.name} className={cur && cur.r === ri ? "selr" : ""}>
                  <th className="rh" scope="row">
                    <div className="in">
                      {r && <Dot r={r} />}
                      <b>{row.name}</b>
                      {row.lang && <span className="lang">{row.lang}</span>}
                      {r?.mem && <span className="m">{r.mem}</span>}
                    </div>
                  </th>
                  {(row.cells ?? []).map((c, ci) => {
                    const cross = !!(cur && cur.r === ri && cur.c === ci && c.dot);
                    return (
                      <td
                        key={ci}
                        role="gridcell"
                        data-r={ri}
                        data-c={ci}
                        tabIndex={focused.r === ri && focused.c === ci ? 0 : -1}
                        className={(cols[ci]?.sep ? "sep" : "") + colOn(ci) + (cross ? " cross" : "") + (c.text ? " t " + toneOf(c.tone ?? "") : "")}
                        aria-label={c.tip ? c.tip.head : c.self ? `${row.name} itself` : `${row.name}, ${cols[ci]?.name}: nothing`}
                        onMouseEnter={() => {
                          setCur({ r: ri, c: ci });
                          onRow(row.name);
                        }}
                        onFocus={() => {
                          setFocused({ r: ri, c: ci });
                          setCur({ r: ri, c: ci });
                          onRow(row.name);
                        }}
                        onClick={() => setFocused({ r: ri, c: ci })}
                        onDoubleClick={() => c.tip?.open && void go({ key: stackKey, page: "instance", instance: c.tip.open })}
                      >
                        {c.dot && <span className="cell" />}
                        {c.self && <span className="self">—</span>}
                        {c.text}
                      </td>
                    );
                  })}
                  <td className="fill" aria-hidden />
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr>
              <td className="rh">needed by</td>
              {(g.foot ?? []).map((f, ci) => (
                <td key={ci} className={(cols[ci]?.sep ? "sep" : "") + colOn(ci)}>
                  {f && <b>{f}</b>}
                </td>
              ))}
              <td className="fill" aria-hidden />
            </tr>
          </tfoot>
        </table>
        {g.later && <p className="later">{g.later}</p>}
      </div>
      {tip && (
        <div ref={tipRef} className="tip" role="tooltip" style={tipAt ? { left: tipAt.left, top: tipAt.top, ["--arrow" as string]: `${tipAt.arrow - 6}px` } : { visibility: "hidden" }}>
          <b>{tip.head}</b>
          {tip.why && <> · {tip.why}</>}
          <br />
          <code>{tip.ref}</code> <span className="in">in doze.hcl</span>
          <br />
          <span className="k">{tip.keys}</span>
        </div>
      )}
    </div>
  );
}
