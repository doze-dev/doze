import { useEffect, useMemo, useRef, useState } from "react";
import { ask, askStop, chooseStart, copy, go, repair, run, select, setView, stackAction, useStore } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";
import { pageKey } from "../keys";
import { useTrap } from "../components/trap";
import { StackMenu } from "./library";

// The stack's overview, as the Tiles, List and Map boards lay it out: the
// head with the view switch and the stack's switch, one quiet strip of
// figures, then the stack in the view remembered for it. Every word is
// Go's; this keeps only the selection, the filter and which menu is open.
//
// A stopped stack is the same page in another state: the same rows in the
// same places, each down with its switch off, since nothing runs. Turning
// one on starts the stack with it and what it needs; the strip of figures
// becomes the strip that brings the stack up, as last time or as chosen in
// the start sheet.
export function OverviewPage({ home, stackKey }: { home: view.Home; stackKey: string }) {
  const narrow = useNarrow();
  const selected = useStore((s) => s.selected);
  const [filter, setFilter] = useState("");
  const [menu, setMenu] = useState(false);
  const stopped = home.stopped;
  const starting = useStore((s) => s.choosing === stackKey);
  const groups = home.groups ?? [];
  const rows = useMemo(() => groups.flatMap((g) => g.rows ?? []), [groups]);
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => r.name.toLowerCase().includes(q) || r.because.toLowerCase().includes(q) || (r.address ?? "").toLowerCase().includes(q));
  }, [rows, filter]);
  // The keys act on the selected row only while it is shown: a row the
  // filter hides is not acted on unseen.
  const row = shown.find((r) => r.name === selected);
  const viewName = home.view || "tiles";

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName;
      if ((tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") && e.key === "Escape") {
        (e.target as HTMLElement).blur();
        e.preventDefault();
        return;
      }
      if (!pageKey(e)) return;
      const list = shown;
      const i = list.findIndex((r) => r.name === selected);
      const move = (d: number) => {
        e.preventDefault();
        const next = list[i + d];
        if (next) void select(next.name);
      };
      if (stopped) {
        // Down rows: space starts the stack with the selected one, as its
        // switch does; u is Up as last time, c the start sheet. ⏎ opens, as
        // everywhere, so it is not a way to start a whole stack by surprise.
        // The verbs that need a running stack do nothing.
        if (starting) return;
        switch (e.key) {
          case " ":
            e.preventDefault();
            if (row && startable(row)) void stackAction(stackKey, "up", [row.name]);
            return;
          case "u":
            if (!stopped.problem) void stackAction(stackKey, "up-last");
            return;
          case "Enter":
            return;
          case "c":
            if (!stopped.problem) void chooseStart(stackKey);
            return;
          case "p":
          case "w":
          case "s":
          case "r":
          case "l":
            return;
        }
      }
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
        case "T":
          e.preventDefault();
          void go({ key: stackKey, page: "timeline" });
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown, row, selected, stackKey, viewName, stopped, starting]);

  const stackSummary = useStore((s) => s.stacks.find((x) => x.key === stackKey));
  const libRow: view.LibStack | undefined = useStore((s) => s.library?.stacks?.find((x) => x.key === stackKey));

  const [inst, setInst] = useState<view.Instance | null>(null);
  useEffect(() => {
    // The List view's pane: the selected one in full, from Go.
    let live = true;
    if (viewName !== "list" || !selected || stopped) return;
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
          {stopped ? ` · stopped · ${stopped.when}` : ""}
        </span>
        <span className="grow" />
        <div className="seg" role="group" aria-label="View">
          {(["tiles", "list", "map"] as const).map((v) => (
            <button key={v} className={viewName === v ? "on" : ""} aria-pressed={viewName === v} onClick={() => void setView(stackKey, v)}>
              {v === "tiles" ? "Tiles" : v === "list" ? "List" : "Map"}
            </button>
          ))}
        </div>
        <button className="btn quiet" onClick={() => void go({ key: stackKey, page: "timeline" })} title="What woke, slept and crashed in the last half hour (T)">
          Timeline <kbd>T</kbd>
        </button>
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
            onClick={() => void (home.running ? askStop(stackKey) : stackAction(stackKey, "up-last"))}
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
      {home.config && <ConfigBanner c={home.config} dir={home.dir} />}
      {home.changed && (
        <div className="banner cool" role="status">
          <span className="dot c" aria-hidden />
          <div className="t">
            <b>{home.changed.text}.</b>
            <span>The stack runs the file as it was when it came up. Restarting it brings it down and up as last time, on the new file; data stays.</span>
          </div>
          <button className="btn pri" onClick={() => void stackAction(stackKey, "restart")}>
            Restart the stack
          </button>
        </div>
      )}
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
      {stopped ? (
        <DownStrip s={stopped} tight={viewName === "list"} onUp={() => void stackAction(stackKey, "up-last")} onChoose={() => void chooseStart(stackKey)} dir={home.dir} />
      ) : (
        <Strip v={home.vitals} narrow={narrow} tight={viewName === "list"} />
      )}
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
          <Tiles groups={groups} shown={shown} selected={selected} stackKey={stackKey} />
        </div>
      )}
      {viewName === "list" && (
        <div className="body" style={{ flex: 1, minHeight: 0 }}>
          <div className="main" style={{ paddingTop: 28 }}>
            {filterRow}
            <div className="scrollarea" style={{ paddingBottom: 12 }}>
              <List groups={groups} shown={shown} selected={selected} stackKey={stackKey} />
            </div>
          </div>
          {row?.down ? <DownPane row={row} /> : <Pane inst={inst} row={row} stackKey={stackKey} />}
        </div>
      )}
      {viewName === "map" && (
        <div className="scrollarea">
          <MapView m={home.map} rows={rows} selected={selected} stackKey={stackKey} />
        </div>
      )}
      <Keys
        keys={
          stopped
            ? [
                ["↑↓", "move"],
                ["space", "start with this one"],
                ["u", "up as last time"],
                ["c", "choose what starts"],
                ["/", "filter"],
                ["esc", "library"],
              ]
            : [
                ["↑↓", "move"],
                ["space", "wake / sleep"],
                ["p", "pin"],
                ["⏎", "open"],
                ["l", "logs"],
                ["y", "copy address"],
                ["/", "filter"],
                ["esc", "library"],
              ]
        }
        note={stopped ? "nothing runs while the stack is down" : "from Library: click a stack"}
      />
      {starting && stopped && <StartSheet stack={home.stack} rows={rows} lastSet={stopped.lastSet ?? []} stackKey={stackKey} />}
    </>
  );
}

/** DownStrip stands where the figures do while the stack is stopped: that it is down, what Up as last time starts, and the two ways up. */
function DownStrip({ s, tight, onUp, onChoose, dir }: { s: view.Stopped; tight?: boolean; onUp: () => void; onChoose: () => void; dir: string }) {
  if (s.problem) {
    return (
      <div className={"strip" + (tight ? " tight" : "")} role="alert">
        <div className="stat wide">
          <span className="v bad">
            doze.hcl does not load<small>nothing can start until it does</small>
          </span>
          <span className="s" title={s.problem}>
            {s.problem}
          </span>
        </div>
        <button className="btn" onClick={() => transport().then((t) => t.open("file://" + dir + "/doze.hcl"))}>
          Edit doze.hcl <kbd>⌘E</kbd>
        </button>
      </div>
    );
  }
  return (
    <div className={"strip" + (tight ? " tight" : "")} role="status">
      <div className="stat wide">
        <span className="v">
          Stopped<small>{s.when}</small>
        </span>
        <span className="s">{s.lastLine} Modules wake on their own when something connects.</span>
      </div>
      <button className="btn pri" onClick={onUp}>
        Up as last time <kbd>u</kbd>
      </button>
      <button className="btn" onClick={onChoose}>
        Choose what starts… <kbd>c</kbd>
      </button>
    </div>
  );
}

/** startable is a down row that can start the stack: not a task, not paused in doze.hcl. */
function startable(r: view.Row): boolean {
  return r.kind !== "task" && !r.off;
}

/**
 * StartSheet is how a stopped stack is started some other way than last
 * time: tick the apps you will work on; what they need is shown starting
 * with them, and modules nobody ticked still wake when something connects.
 * Up remembers the set, so next time "as last time" is this.
 */
function StartSheet({ stack, rows, lastSet, stackKey }: { stack: string; rows: view.Row[]; lastSet: string[]; stackKey: string }) {
  const pickable = rows.filter((r) => startable(r) && r.kind !== "module");
  const [ticked, setTicked] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(pickable.map((r) => [r.name, lastSet.length === 0 ? true : lastSet.includes(r.name)])),
  );
  const [at, setAt] = useState(0);
  const chosen = pickable.filter((r) => ticked[r.name]).map((r) => r.name);
  // What the chosen ones need, all the way down, that was not chosen itself.
  const byName = new Map(rows.map((r) => [r.name, r]));
  const brought = new Set<string>();
  const walk = (n: string) => {
    for (const d of byName.get(n)?.needs ?? []) {
      if (!brought.has(d) && !chosen.includes(d)) {
        brought.add(d);
        walk(d);
      }
    }
  };
  chosen.forEach(walk);
  const box = useRef<HTMLElement>(null);
  useTrap(box);
  const close = () => void chooseStart("");
  const start = () => {
    if (chosen.length === 0) return;
    close();
    void stackAction(stackKey, "up", chosen);
  };
  const everything = () => {
    close();
    void stackAction(stackKey, "up-all");
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Tab") return;
      e.stopPropagation();
      // Space and ⏎ on a focused button are the button's own.
      const onButton = (e.target as HTMLElement).closest?.("button");
      if ((e.key === " " || e.key === "Enter") && onButton && !onButton.classList.contains("pick")) return;
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          close();
          break;
        case "ArrowDown":
          e.preventDefault();
          setAt((i) => Math.min(pickable.length - 1, i + 1));
          break;
        case "ArrowUp":
          e.preventDefault();
          setAt((i) => Math.max(0, i - 1));
          break;
        case " ":
          e.preventDefault();
          if (pickable[at]) setTicked((t) => ({ ...t, [pickable[at].name]: !t[pickable[at].name] }));
          break;
        case "Enter":
          e.preventDefault();
          start();
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [at, ticked]);
  return (
    <div className="scrim" onClick={close}>
      <section ref={box} className="dialog start" role="dialog" aria-modal="true" aria-label={`Start ${stack}`} onClick={(e) => e.stopPropagation()}>
        <h2>Start {stack}</h2>
        <p className="dim">Tick what you will work on. doze starts those and what they need, in order; the rest stays down until something connects to it.</p>
        <div className="picks" role="group" aria-label="What starts">
          {pickable.map((r, i) => (
            <button
              key={r.name}
              className={"pick" + (i === at ? " at" : "")}
              role="checkbox"
              aria-label={r.name}
              aria-checked={!!ticked[r.name]}
              onMouseEnter={() => setAt(i)}
              onFocus={() => setAt(i)}
              onClick={() => setTicked((t) => ({ ...t, [r.name]: !t[r.name] }))}
            >
              <span className={"box" + (ticked[r.name] ? " on" : "")} aria-hidden>
                {ticked[r.name] ? "✓" : ""}
              </span>
              <b>{r.name}</b>
              <span className="dim ell">{r.needs && r.needs.length > 0 ? "needs " + r.needs.join(", ") : "needs nothing"}</span>
              {r.last && <span className="tag">last time</span>}
            </button>
          ))}
        </div>
        <p className="also">
          {chosen.length === 0 ? (
            <span className="warn">Tick at least one, or start everything.</span>
          ) : brought.size > 0 ? (
            <>
              Also starts, because they are needed: <b className="bright">{Array.from(brought).join(", ")}</b>
            </>
          ) : (
            <span className="dim">Nothing else starts with them.</span>
          )}
        </p>
        <div className="acts">
          <button className="btn quiet" onClick={close}>
            Cancel <kbd>esc</kbd>
          </button>
          <button className="btn" onClick={everything}>
            Everything
          </button>
          <button className="btn pri" disabled={chosen.length === 0} onClick={start}>
            {chosen.length === pickable.length ? `Start all ` : chosen.length === 1 ? `Start ` : `Start these `} <kbd>⏎</kbd>
          </button>
        </div>
      </section>
    </div>
  );
}

/** DownPane is the List view's pane for a row of a stopped stack: what doze.hcl says about it. */
function DownPane({ row }: { row: view.Row }) {
  return (
    <aside className="insp" aria-label={`${row.name}, from doze.hcl`}>
      <div className="ih">
        <Dot r={row} />
        <h2>{row.name}</h2>
        <span className="sub">{row.engine} · down</span>
      </div>
      <div className="sec">
        <h3>From doze.hcl</h3>
        <div className="kv">
          <span className="k">{row.kind === "module" ? "used by" : "needs"}</span>
          <span className="v" title={row.because}>
            {row.because}
          </span>
          <span />
        </div>
        <div className="kv">
          <span className="k">on Up</span>
          <span className="v">
            {row.off ? "paused in doze.hcl" : row.kind === "task" ? "runs when you say, once the stack is up" : row.last ? "starts, as last time" : row.kind === "module" ? "wakes when something connects" : "stays down; tick it in Choose what starts"}
          </span>
          <span />
        </div>
        {row.address && (
          <div className="kv">
            <span className="k">address</span>
            <span className="v b" title={row.address}>
              {row.address}
            </span>
            <button className="copy" onClick={() => void copy(row.address ?? "", `the address of ${row.name}`)}>
              Copy
            </button>
          </div>
        )}
        {row.run && (
          <div className="kv">
            <span className="k">command</span>
            <span className="v b" title={row.run}>
              {row.run}
            </span>
            <button className="copy" onClick={() => void copy(row.run ?? "", "the command")}>
              Copy
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}

/** toggle flips a row's switch: wake it, or put it to sleep; one turned off comes back and wakes. */
function toggle(stackKey: string, r: view.Row) {
  if (r.kind === "task") {
    void run(stackKey, "task", r.name);
    return;
  }
  if (r.paused) return;
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
  if (r.down) {
    // A stopped stack: nothing runs, so the switch is off. Turning it on
    // starts the stack with this one and what it needs, as waking does.
    if (r.kind === "task") return <span aria-hidden />;
    return (
      <button
        className={"sw" + (r.off ? " dis" : "")}
        role="switch"
        aria-checked={false}
        aria-label={`${r.name} is down`}
        title={r.off ? "paused in doze.hcl (enabled = false)" : `Start the stack with ${r.name} and what it needs`}
        disabled={r.off}
        onClick={(e) => {
          e.stopPropagation();
          void stackAction(stackKey, "up", [r.name]);
        }}
      />
    );
  }
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
  if (r.paused) {
    return (
      <span className="lock" role="img" aria-label={`${r.name} is paused in doze.hcl`} title="enabled = false in doze.hcl: only the file brings it back">
        ⊘
      </span>
    );
  }
  return (
    <button
      className={"sw" + (r.on ? " on" : "") + (r.off ? " dis" : "")}
      role="switch"
      aria-checked={r.on}
      aria-label={`${r.name} is ${r.look.word}`}
      title={r.off ? "Turned off: switch it on to bring it back and wake it" : r.on ? "Switch off to put it to sleep" : "Switch on to wake it"}
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
                <div
                  key={r.name}
                  role="group"
                  aria-label={r.name}
                  className={"tile" + (r.name === selected ? " sel" : "") + (r.trouble ? " bad" : "") + (!r.on && r.kind !== "task" ? " off" : "")}
                  onClick={() => void select(r.name)}
                  onDoubleClick={() => !r.down && void go({ key: stackKey, page: "instance", instance: r.name })}
                >
                  <div className="top">
                    <Dot r={r} />
                    <Name r={r} stackKey={stackKey} />
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
                </div>
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
                  <div
                    key={r.name}
                    role="group"
                    aria-label={r.name}
                    className={"qr narrow" + (r.name === selected ? " sel" : "") + (r.trouble ? " bad" : "") + (!r.on && r.kind !== "task" ? " off" : "")}
                    onClick={() => void select(r.name)}
                    onDoubleClick={() => !r.down && void go({ key: stackKey, page: "instance", instance: r.name })}
                  >
                    <Dot r={r} />
                    <Name r={r} stackKey={stackKey} />
                    <span className="why">
                      <span className={"w " + wordTone(r)}>{r.look.word}</span>
                      {r.because && <> · {r.because}</>}
                      {r.err && <> · {r.err}</>}
                    </span>
                    <span className="m">{r.mem ?? ""}</span>
                    <Switch r={r} stackKey={stackKey} />
                  </div>
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
    <div className="map" role="group" aria-label="The stack as a map: what needs what, and what is keeping what awake" style={{ minHeight: m.h + 60, minWidth: m.w + 60 }}>
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
            aria-label={`${r.name}, ${r.look.word}${r.because ? ", " + r.because : ""}`}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !r.down) {
                e.preventDefault();
                void go({ key: stackKey, page: "instance", instance: r.name });
              }
            }}
            onClick={() => void select(r.name)}
            onDoubleClick={() => !r.down && void go({ key: stackKey, page: "instance", instance: r.name })}
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

/** ConfigBanner is the ConfigError board: the stack keeps running on the file it last loaded, and the page shows what is wrong and where. */
function ConfigBanner({ c, dir }: { c: view.ConfigError; dir: string }) {
  const name = (c.file || "doze.hcl").split("/").pop() || "doze.hcl";
  const edit = () => transport().then((t) => t.open("file://" + (c.file || dir + "/doze.hcl")));
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "e") {
        e.preventDefault();
        void edit();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
  return (
    <>
      <div className="banner bad" role="alert">
        <span className="dot r" aria-hidden />
        <div className="t">
          <b>{c.text}: the stack keeps running as it was.</b>
          <span>Nothing is applied until the file loads. Fix it in your editor; this page reads it again on save.</span>
        </div>
        <button className="btn" onClick={() => void edit()}>
          Edit {name} <kbd>⌘E</kbd>
        </button>
      </div>
      <section className="card srccard">
        <h3>
          {name}{c.line ? <span className="dim"> · line {c.line}</span> : null}
        </h3>
        {(c.excerpt ?? []).length > 0 && (
          <div className="src" role="figure" aria-label={`${name} around line ${c.line}`}>
            {(c.excerpt ?? []).map((l) => (
              <div key={l.n} style={{ display: "contents" }}>
                <span className="n">{l.n}</span>
                <span className={l.bad ? "bad-l" : ""}>{l.text || " "}</span>
              </div>
            ))}
          </div>
        )}
        <div>
          <b className="bright">{c.summary}</b>
          {c.detail && <span className="dim"> · {c.detail}</span>}
        </div>
        <p className="dim" style={{ margin: 0, fontSize: "var(--text-sm)" }}>
          The same message as <span className="mono">doze lint</span>.
        </p>
      </section>
    </>
  );
}

/**
 * Name is a service's name on a tile or row: the control a keyboard reaches
 * it by. Focus or a click selects the service; ⏎ opens it (a double click
 * on the tile does too). It says the state to a screen reader, which the
 * tile's dot and colour cannot.
 */
function Name({ r, stackKey }: { r: view.Row; stackKey: string }) {
  return (
    <button
      className="nm"
      aria-label={`${r.name}, ${r.look.word}${r.because ? ", " + r.because : ""}`}
      onFocus={() => void select(r.name)}
      onClick={(e) => {
        e.stopPropagation();
        void select(r.name);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !r.down) {
          e.preventDefault();
          void go({ key: stackKey, page: "instance", instance: r.name });
        }
      }}
    >
      <b>{r.name}</b>
    </button>
  );
}
