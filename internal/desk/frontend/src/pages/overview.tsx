import { useEffect, useMemo, useState } from "react";
import { ask, copy, go, repair, run, select, setView, stackAction, useStore } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";
import { StackMenu } from "./library";

// The stack's overview, as the Tiles, List and Map boards lay it out: the
// head with the view switch and the stack's switch, one quiet strip of
// figures, then the stack in the view remembered for it. Every word is
// Go's; this keeps only the selection, the filter and which menu is open.
export function OverviewPage({ home, stackKey }: { home: view.Home; stackKey: string }) {
  const narrow = useNarrow();
  const selected = useStore((s) => s.selected);
  const [filter, setFilter] = useState("");
  const [menu, setMenu] = useState(false);
  const rows = useMemo(() => (home.groups ?? []).flatMap((g) => g.rows ?? []), [home.groups]);
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => r.name.toLowerCase().includes(q) || r.because.toLowerCase().includes(q) || (r.address ?? "").toLowerCase().includes(q));
  }, [rows, filter]);
  const row = rows.find((r) => r.name === selected);
  const viewName = home.view || "tiles";

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") {
        if (e.key === "Escape") (e.target as HTMLElement).blur();
        return;
      }
      const list = shown;
      const i = list.findIndex((r) => r.name === selected);
      const move = (d: number) => {
        e.preventDefault();
        const next = list[i + d];
        if (next) void select(next.name);
      };
      switch (e.key) {
        case "ArrowDown":
        case "j":
          move(1);
          break;
        case "ArrowUp":
        case "k":
          move(-1);
          break;
        case "ArrowRight":
          if (viewName === "tiles") move(1);
          break;
        case "ArrowLeft":
          if (viewName === "tiles") move(-1);
          break;
        case " ":
          e.preventDefault();
          if (row) toggle(stackKey, row);
          break;
        case "Enter":
          if (row) void go({ key: stackKey, page: "instance", instance: row.name });
          break;
        case "p":
          if (row && row.kind !== "task") void run(stackKey, "pin", row.name);
          break;
        case "w":
          if (row && !row.awake) void run(stackKey, row.kind === "task" ? "task" : "wake", row.name);
          break;
        case "s":
          if (row && row.awake) void ask(stackKey, row.name, "sleep");
          break;
        case "r":
          if (row) row.kind === "task" ? void run(stackKey, "task", row.name) : void ask(stackKey, row.name, "restart");
          break;
        case "l":
          if (row) void go({ key: stackKey, page: "logs", instance: row.name });
          break;
        case "L":
          void go({ key: stackKey, page: "logs" });
          break;
        case "y":
          if (row?.address) void copy(row.url || row.address, `the address of ${row.name}`);
          break;
        case "/":
          e.preventDefault();
          (document.querySelector(".filter input") as HTMLInputElement | null)?.focus();
          break;
        case "1":
        case "2":
        case "3":
          void setView(stackKey, ["tiles", "list", "map"][Number(e.key) - 1]);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [shown, row, selected, stackKey, viewName]);

  const stackSummary = useStore((s) => s.stacks.find((x) => x.key === stackKey));
  const libRow: view.LibStack | undefined = useStore((s) => s.library?.stacks?.find((x) => x.key === stackKey));

  const [inst, setInst] = useState<view.Instance | null>(null);
  useEffect(() => {
    // The List view's pane: the selected one in full, from Go.
    let live = true;
    if (viewName !== "list" || !selected) return;
    transport().then(async (t) => {
      try {
        const i = await t.instance(stackKey, selected);
        if (live) setInst(i);
      } catch {
        if (live) setInst(null);
      }
    });
    return () => {
      live = false;
    };
  }, [stackKey, selected, viewName, home]);

  const page = (
    <>
      <div className="head" style={{ position: "relative" }}>
        <h1>{home.stack}</h1>
        <span className="path">
          {home.dir}
          {home.up ? ` · up ${home.up}` : ""}
        </span>
        <span className="grow" />
        <div className="seg" role="group" aria-label="View">
          {(["tiles", "list", "map"] as const).map((v) => (
            <button key={v} className={viewName === v ? "on" : ""} aria-pressed={viewName === v} onClick={() => void setView(stackKey, v)}>
              {v === "tiles" ? "Tiles" : v === "list" ? "List" : "Map"}
            </button>
          ))}
        </div>
        <button className="btn quiet" onClick={() => void go({ key: stackKey, page: "logs" })} title="Every service's logs, merged (L)">
          All logs <kbd>L</kbd>
        </button>
        <span className="swl">
          <b>Up</b>
          <button
            className={"sw big" + (home.running ? " on" : "")}
            role="switch"
            aria-checked={home.running}
            aria-label={home.running ? `${home.stack} is up` : `${home.stack} is down`}
            onClick={() => void stackAction(stackKey, home.running ? "down" : "up-last")}
          />
          <button className={"ib" + (menu ? " on" : "")} aria-label={`More ways to start ${home.stack}`} onClick={() => setMenu((m) => !m)}>
            ▾
          </button>
        </span>
        {menu && libRow && <StackMenu s={libRow} onClose={() => setMenu(false)} />}
        {menu && !libRow && stackSummary && (
          <StackMenu
            s={{ key: stackKey, name: home.stack, dir: home.dir, running: home.running, live: true, dot: "", declared: home.total, fig: [], sub: "", addresses: [], when: "" }}
            onClose={() => setMenu(false)}
          />
        )}
      </div>
      {home.setup && (
        <div className="banner cool" role="status">
          <span className="dot c" aria-hidden />
          <div className="t">
            <b>{home.setup.text}</b>
            <span>{home.setup.detail}</span>
          </div>
          <button className="btn pri" onClick={() => void repair()} title="The same as doze trust, behind the system's password prompt">
            Set it up <span className="dim">(one password)</span>
          </button>
          <button className="btn quiet" onClick={() => void copy(home.setup?.command ?? "", "the command")} title="For a terminal instead">
            Copy <span className="mono">{home.setup.command}</span>
          </button>
        </div>
      )}
      <Strip v={home.vitals} narrow={narrow} tight={viewName === "list"} />
    </>
  );
  // The filter row; in the list view it sits inside the left column, so
  // the details pane can start at the strip's line.
  const filterRow = (
    <div className="toolbar" style={{ paddingBottom: 14 }}>
      <label className="filter">
        <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter the services" />
        <kbd>/</kbd>
      </label>
    </div>
  );

  // The head, the banner, the strip and the filter span the window in
  // every view, so the Tiles · List · Map switch never moves; only the
  // rows share their room with the details pane. The map scrolls like the
  // tiles, so the keys stay in view under it.
  return (
    <>
      {page}
      {viewName !== "list" && filterRow}
      {viewName === "tiles" && (
        <div className="scrollarea">
          <Tiles groups={home.groups ?? []} shown={shown} selected={selected} stackKey={stackKey} />
        </div>
      )}
      {viewName === "list" && (
        <div className="body" style={{ flex: 1, minHeight: 0 }}>
          <div className="main" style={{ paddingTop: 28 }}>
            {filterRow}
            <div className="scrollarea" style={{ paddingBottom: 12 }}>
              <List groups={home.groups ?? []} shown={shown} selected={selected} stackKey={stackKey} />
            </div>
          </div>
          <Pane inst={inst} row={row} stackKey={stackKey} />
        </div>
      )}
      {viewName === "map" && (
        <div className="scrollarea">
          <MapView m={home.map} rows={rows} selected={selected} stackKey={stackKey} />
        </div>
      )}
      <Keys
        keys={[
          ["↑↓", "move"],
          ["space", "wake / sleep"],
          ["p", "pin"],
          ["⏎", "open"],
          ["l", "logs"],
          ["y", "copy address"],
          ["/", "filter"],
          ["esc", "library"],
        ]}
        note="from Library: click a stack"
      />
    </>
  );
}

/** toggle flips a row's switch: part of this run, or not. */
function toggle(stackKey: string, r: view.Row) {
  if (r.kind === "task") {
    void run(stackKey, "task", r.name);
    return;
  }
  // The switch is awake or asleep. Sleeping asks when something depends on
  // it; a service doze off took out of the run comes back in first.
  if (r.off) void run(stackKey, "on", r.name).then(() => run(stackKey, "wake", r.name));
  else if (r.on) void ask(stackKey, r.name, "sleep");
  else void run(stackKey, "wake", r.name);
}

function Strip({ v, narrow, tight }: { v: view.Vitals4; narrow?: boolean; tight?: boolean }) {
  const s = v.state;
  const counts: Array<[number, string, string]> = [
    [s.dozing, "dozing", "warn"],
    [s.waking, "waking", "cool"],
    [s.asleep, "asleep", ""],
    [s.off, "off", ""],
    [s.crashing, "crashing", "bad"],
  ];
  return (
    <div className={"strip" + (narrow ? " narrow" : "") + (tight ? " tight" : "")}>
      <div className="stat wide">
        <span className="v">
          {v.mem.total}
          <small>resident</small>
        </span>
        <div className="bar" role="img" aria-label={`Memory ${v.mem.total}`}>
          {(v.mem.shares ?? []).map((sh, i) => (
            <i key={sh.name} className={"c" + (i % 6)} style={{ width: `${Math.max(sh.part * 100, 1)}%` }} title={`${sh.name} ${sh.label}`} />
          ))}
        </div>
        <span className="s">{v.mem.key}</span>
      </div>
      <div className="stat">
        <span className="v">
          {v.cpu.total.split(" ")[0]}
          <small>cores</small>
        </span>
        <span className="s">{v.cpu.key}</span>
      </div>
      <div className="stat">
        <span className="v">
          {s.awake}
          <small>awake</small>
        </span>
        <span className="s">
          {counts
            .filter(([n]) => n > 0)
            .map(([n, word, tone], i) => (
              <span key={word}>
                {i > 0 && " · "}
                <span className={tone}>
                  {n} {word}
                </span>
              </span>
            ))}
          {counts.every(([n]) => n === 0) && "everything awake"}
        </span>
      </div>
      <div className="stat">
        <span className="v">
          {v.conns.open}
          <small>{v.conns.open === 1 ? "connection" : "connections"}</small>
        </span>
        <span className="s">{v.conns.tasks || "through doze"}</span>
      </div>
    </div>
  );
}

function PinGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
      <path d="M6 1.5h4l-.8 4.2L12 8.5V10H8.6V15H7.4V10H4V8.5l2.8-2.8z" fill="currentColor" />
    </svg>
  );
}

function Switch({ r, stackKey }: { r: view.Row; stackKey: string }) {
  if (r.kind === "task") {
    return (
      <button
        className="runbtn"
        onClick={(e) => {
          e.stopPropagation();
          void run(stackKey, "task", r.name);
        }}
      >
        {r.look.word === "done" || r.look.word === "failed" ? "Run again" : "Run"}
      </button>
    );
  }
  return (
    <button
      className={"sw" + (r.on ? " on" : "") + (r.off ? " dis" : "")}
      role="switch"
      aria-checked={r.on}
      aria-label={r.on ? `${r.name} is awake` : r.off ? `${r.name} is off` : `${r.name} is asleep`}
      title={r.off ? "doze off took it out of this run; the switch puts it back" : undefined}
      onClick={(e) => {
        e.stopPropagation();
        toggle(stackKey, r);
      }}
    />
  );
}

function Dot({ r }: { r: view.Row }) {
  if (r.kind === "task") {
    return (
      <span className={"glyph tone-" + r.look.tone} aria-hidden>
        {r.look.glyph}
      </span>
    );
  }
  if (r.trouble) return <span className="dot r" aria-hidden />;
  switch (r.look.word) {
    case "awake":
      return <span className="dot g" aria-hidden />;
    case "dozing":
      return <span className="dot y" aria-hidden />;
    case "waking":
      return <span className="ring" aria-hidden />;
  }
  return <span className="dot" aria-hidden />;
}

function wordTone(r: view.Row): string {
  if (r.trouble) return "bad";
  switch (r.look.word) {
    case "awake":
      return "ok";
    case "dozing":
      return "warn";
    case "waking":
      return "cool";
    case "done":
      return "ok";
    case "ready":
      return "acc";
    case "failed":
      return "bad";
  }
  return "";
}

function Address({ r, stackKey }: { r: view.Row; stackKey: string }) {
  void stackKey;
  if (!r.address) return null;
  const openIt = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (r.url) transport().then((t) => t.open(r.url ?? ""));
  };
  return (
    <>
      <button className={"a" + (r.url && r.awake ? " link" : "")} onClick={openIt} title={r.url ? `Open ${r.url}` : r.address} style={{ font: "inherit", color: "inherit", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: "0 1 auto" }}>
        {r.address}
      </button>
      <button
        className="copy"
        aria-label={`Copy the address of ${r.name}`}
        onClick={(e) => {
          e.stopPropagation();
          void copy(r.url || r.address || "", `the address of ${r.name}`);
        }}
      >
        ⧉
      </button>
    </>
  );
}

function Tiles({ groups, shown, selected, stackKey }: { groups: view.Group[]; shown: view.Row[]; selected: string; stackKey: string }) {
  const visible = new Set(shown.map((r) => r.name));
  return (
    <>
      {groups.map((g) => {
        const rows = (g.rows ?? []).filter((r) => visible.has(r.name));
        if (rows.length === 0) return null;
        return (
          <div key={g.title}>
            <div className="gl">
              {g.title.charAt(0) + g.title.slice(1).toLowerCase()} <span>{g.what}</span>
            </div>
            <div className="tiles">
              {rows.map((r) => (
                <button
                  key={r.name}
                  className={"tile" + (r.name === selected ? " sel" : "") + (r.trouble ? " bad" : "") + (!r.on && r.kind !== "task" ? " off" : "")}
                  onClick={() => void select(r.name)}
                  onDoubleClick={() => void go({ key: stackKey, page: "instance", instance: r.name })}
                >
                  <div className="top">
                    <Dot r={r} />
                    <b>{r.name}</b>
                    {r.pinned && (
                      <span className="pin" title="pinned: never sleeps">
                        <PinGlyph />
                      </span>
                    )}
                    <Switch r={r} stackKey={stackKey} />
                  </div>
                  <div className="st">
                    <b className={wordTone(r)}>{r.look.word}</b>
                    {r.because && <> · {r.because}</>}
                  </div>
                  {r.err ? (
                    <div className="err">{r.err}</div>
                  ) : (
                    <div className="ft">
                      {r.mem && <span className="m">{r.mem}</span>}
                      {r.kind === "task" ? <span className="a">{r.engine === "task" ? "" : r.engine}</span> : <Address r={r} stackKey={stackKey} />}
                    </div>
                  )}
                </button>
              ))}
            </div>
          </div>
        );
      })}
    </>
  );
}

function List({ groups, shown, selected, stackKey }: { groups: view.Group[]; shown: view.Row[]; selected: string; stackKey: string }) {
  const visible = new Set(shown.map((r) => r.name));
  return (
    <div className="ql">
          {groups.map((g) => {
            const rows = (g.rows ?? []).filter((r) => visible.has(r.name));
            if (rows.length === 0) return null;
            return (
              <div key={g.title}>
                <div className="qh">{g.title.charAt(0) + g.title.slice(1).toLowerCase()}</div>
                {rows.map((r) => (
                  <button
                    key={r.name}
                    className={"qr narrow" + (r.name === selected ? " sel" : "") + (r.trouble ? " bad" : "") + (!r.on && r.kind !== "task" ? " off" : "")}
                    onClick={() => void select(r.name)}
                    onDoubleClick={() => void go({ key: stackKey, page: "instance", instance: r.name })}
                  >
                    <Dot r={r} />
                    <b>{r.name}</b>
                    <span className="why">
                      <span className={"w " + wordTone(r)}>{r.look.word}</span>
                      {r.because && <> · {r.because}</>}
                      {r.err && <> · {r.err}</>}
                    </span>
                    <span className="m">{r.mem ?? ""}</span>
                    <Switch r={r} stackKey={stackKey} />
                  </button>
                ))}
              </div>
            );
          })}
    </div>
  );
}

function Pane({ inst, row, stackKey }: { inst: view.Instance | null; row?: view.Row; stackKey: string }) {
  if (!inst || !row) return <aside className="insp" aria-label="Details" />;
  const needs = inst.needs ?? [];
  const holders = inst.holders ?? [];
  return (
    <aside className="insp" aria-label={`${inst.name}, in full`}>
      <div className="ih">
        <Dot r={row} />
        <h2>{inst.name}</h2>
        <span className="sub">{inst.path}</span>
      </div>
      {(inst.connect ?? []).length > 0 && (
        <div className="sec">
          <h3>Addresses</h3>
          {(inst.connect ?? []).map((kv) => (
            <div key={kv.k} className="kv">
              <span className="k">{kv.k}</span>
              <span className={"v" + (kv.dim ? " dim" : "") + (kv.mono ? "" : " b")} title={kv.v}>
                {kv.v}
              </span>
              {kv.copy ? (
                <button className="copy" onClick={() => void copy(kv.copy ?? "", kv.k)}>
                  Copy
                </button>
              ) : (
                <span />
              )}
            </div>
          ))}
        </div>
      )}
      <div className="sec">
        <h3>Run</h3>
        {inst.run && (
          <div className="kv">
            <span className="k">command</span>
            <span className="v b" title={inst.run}>
              {inst.run}
            </span>
            <button className="copy" onClick={() => void copy(inst.run ?? "", "the command")}>
              Copy
            </button>
          </div>
        )}
        {inst.up && (
          <div className="kv">
            <span className="k">awake for</span>
            <span className="v">{inst.up}</span>
            <span />
          </div>
        )}
        {inst.pid ? (
          <div className="kv">
            <span className="k">pid</span>
            <span className="v">{inst.pid}</span>
            <span />
          </div>
        ) : null}
        <div className="kv">
          <span className="k">sleep</span>
          <span className="v">{row.pinned ? "never · pinned" : inst.sleeps || "when nothing holds it"}</span>
          {row.kind !== "task" && (
            <button className="copy" onClick={() => void run(stackKey, "pin", row.name)}>
              {row.pinned ? "Unpin" : "Pin"}
            </button>
          )}
        </div>
      </div>
      {(needs.length > 0 || holders.length > 0) && (
        <div className="sec">
          <h3>Needs · held by</h3>
          {needs.length > 0 && (
            <div className="chips">
              {needs.map((n) => (
                <span key={n.name} className="chip">
                  <span className={"glyph tone-" + n.look.tone} aria-hidden>
                    {n.glyph}
                  </span>
                  {n.name}
                </span>
              ))}
            </div>
          )}
          {holders.length > 0 && (
            <div className="chips">
              {holders.map((h) => (
                <span key={h.text} className="chip" title={h.explain}>
                  <span className="dot g" aria-hidden />
                  {h.text}
                </span>
              ))}
            </div>
          )}
          {inst.because && <div className="dim" style={{ fontSize: 12 }}>{inst.because}</div>}
        </div>
      )}
      {inst.crash && (
        <div className="sec">
          <h3>Last error</h3>
          <div className="code" style={{ whiteSpace: "pre-wrap" }}>
            {inst.crash.error}
          </div>
          {inst.crash.tries && (
            <div className="dim" style={{ fontSize: 12 }}>
              {inst.crash.tries}
              {inst.crash.nextIn ? ` · next in ${inst.crash.nextIn}` : ""}
            </div>
          )}
        </div>
      )}
      <div className="acts">
        <button className="btn" onClick={() => void go({ key: stackKey, page: "logs", instance: row.name })}>
          Logs <kbd>l</kbd>
        </button>
        {row.kind !== "task" && (
          <button className="btn" onClick={() => void ask(stackKey, row.name, "restart")}>
            Restart <kbd>r</kbd>
          </button>
        )}
        <span className="grow" />
        <button className="btn pri" onClick={() => void go({ key: stackKey, page: "instance", instance: row.name })}>
          Open <kbd>⏎</kbd>
        </button>
      </div>
    </aside>
  );
}

function MapView({ m, rows, selected, stackKey }: { m: view.MapView; rows: view.Row[]; selected: string; stackKey: string }) {
  const byName = new Map(rows.map((r) => [r.name, r]));
  const path = (e: view.MapEdge) => {
    if (e.same) return `M${e.x1} ${e.y1} C${e.x1} ${(e.y1 + e.y2) / 2}, ${e.x2} ${(e.y1 + e.y2) / 2}, ${e.x2} ${e.y2}`;
    const dx = Math.max(60, Math.abs(e.x2 - e.x1) / 2);
    return `M${e.x1} ${e.y1} C${e.x1 + dx} ${e.y1}, ${e.x2 - dx} ${e.y2}, ${e.x2} ${e.y2}`;
  };
  return (
    <div className="map" role="img" aria-label="The stack as a map: what needs what, and what is keeping what awake" style={{ minHeight: m.h + 60 }}>
      <svg className="edges" width={m.w} height={m.h} aria-hidden="true">
        {(m.edges ?? []).map((e) => (
          <path key={e.from + ">" + e.to} d={path(e)} fill="none" strokeWidth="2" className={"edge " + e.tone} />
        ))}
      </svg>
      {(m.columns ?? []).map((c) => (
        <div key={c.title + c.y} className="col" style={{ left: c.x, top: c.y }}>
          {c.title.charAt(0) + c.title.slice(1).toLowerCase()} <span>{c.what}</span>
        </div>
      ))}
      {(m.nodes ?? []).map((n) => {
        const r = byName.get(n.name);
        if (!r) return null;
        return (
          <button
            key={n.name}
            className={"node" + (n.task ? " task" : "") + (r.name === selected ? " sel" : "") + (r.trouble ? " bad" : "") + (!r.on && !n.task ? " off" : "")}
            style={{ left: n.x, top: n.y, width: n.w }}
            onClick={() => void select(r.name)}
            onDoubleClick={() => void go({ key: stackKey, page: "instance", instance: r.name })}
          >
            <div className="top">
              <Dot r={r} />
              <b>{r.name}</b>
              {r.pinned && (
                <span className="pin">
                  <PinGlyph />
                </span>
              )}
              {r.mem && <span className="m">{r.mem}</span>}
            </div>
            <div className="st">
              <b className={wordTone(r)}>{r.look.word}</b>
              {r.because && <> · {r.because}</>}
            </div>
          </button>
        );
      })}
      <div className="legend">
        <span>
          <i />
          needs, and keeps it awake
        </span>
        <span>
          <i className="dim" />
          needs, not right now
        </span>
        <span>
          <i className="dash" />
          would need, if on
        </span>
      </div>
    </div>
  );
}

/** useNarrow is whether the window is the Narrow board's size: the strip drops its cores figure. */
function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => window.matchMedia("(max-width: 960px)").matches);
  useEffect(() => {
    const q = window.matchMedia("(max-width: 960px)");
    const on = () => setNarrow(q.matches);
    q.addEventListener("change", on);
    return () => q.removeEventListener("change", on);
  }, []);
  return narrow;
}
