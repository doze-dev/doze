import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { askStack, askStop, chooseFolder, chooseStart, dismissHints, go, repair, say, stackAction, useStore } from "../store";
import { transport, type view } from "../transport";
import { pageKey } from "../keys";
import { Keys } from "../components/keys";

// The Library, as the Library boards lay it out: the help strip while it
// is useful, every stack with a switch, the machine line. Every word and
// figure is Go's; this decides only what is selected and which menu is
// open.
export function LibraryPage({ lib }: { lib: view.Library }) {
  const [selected, setSelected] = useState("");
  const [menu, setMenu] = useState<string | null>(null);
  const stacks = lib.stacks ?? [];
  const row = stacks.find((s) => s.key === selected) ?? stacks[0];

  useEffect(() => {
    if (!stacks.some((s) => s.key === selected)) setSelected(stacks[0]?.key ?? "");
  }, [stacks, selected]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && menu) {
        e.preventDefault();
        setMenu(null);
        return;
      }
      if (!pageKey(e)) return;
      const i = stacks.findIndex((s) => s.key === row?.key);
      switch (e.key) {
        case "ArrowDown":
        case "j":
          e.preventDefault();
          if (stacks[i + 1]) setSelected(stacks[i + 1].key);
          break;
        case "ArrowUp":
        case "k":
          e.preventDefault();
          if (stacks[i - 1]) setSelected(stacks[i - 1].key);
          break;
        case " ":
          e.preventDefault();
          if (row) void (row.running ? askStop(row.key) : stackAction(row.key, "up-last"));
          break;
        case "Enter":
          if (row) void go({ key: row.key, page: "home" });
          break;
        case "Backspace":
          // Claimed, or WebKit takes it as "go back" and leaves the page.
          e.preventDefault();
          if (row && !row.running) void askStack(row.key, "forget");
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [stacks, row, menu]);

  return (
    <>
      <div className="head">
        <h1>Library</h1>
        <span className="path">{lib.counts}</span>
        <span className="grow" />
        <button className="btn" onClick={() => void chooseFolder()} title="Open a project folder (⌘O)">
          Open folder… <kbd>⌘O</kbd>
        </button>
        <button className="btn quiet" onClick={() => void go({ key: "", page: "settings" })} title="Themes, the machine, about (⌘,)">
          Settings <kbd>⌘,</kbd>
        </button>
      </div>
      <div className="scrollarea" style={{ paddingBottom: 16 }}>
      {lib.steps && lib.steps.length > 0 && <Steps steps={lib.steps} />}
      {stacks.length === 0 ? (
        <EmptyLibrary />
      ) : (
        <div className="lib" role="table" aria-label="Stacks">
          <div className="lrow h" role="row">
            <span />
            <span>Stack</span>
            <span>Running</span>
            <span>Addresses</span>
            <span className="r">Last</span>
            <span />
          </div>
          {stacks.map((s) => (
            <StackRow key={s.key} s={s} selected={s.key === row?.key} menuOpen={menu === s.key} onSelect={() => setSelected(s.key)} onMenu={() => setMenu(menu === s.key ? null : s.key)} onCloseMenu={() => setMenu(null)} />
          ))}
        </div>
      )}
      {stacks.length > 0 && (
        <p className="model" style={{ marginTop: 12 }}>
          Drop a project folder anywhere in this window, or press ⌘O, to add it. It appears here, stopped, until you bring it up, and stays until you forget it.
        </p>
      )}
      {(lib.others ?? []).length > 0 && <Others others={lib.others ?? []} />}
      </div>
      <Machine checks={lib.machine ?? []} repairable={!!lib.repairable} repairing={!!lib.repairing} />
      <Keys
        keys={[
          ["↑↓", "move"],
          ["space", "up / down"],
          ["⏎", "open"],
          ["⌘O", "open folder"],
          ["⌫", "forget"],
        ]}
        note="the first tab; it never closes"
      />
    </>
  );
}

function Steps({ steps }: { steps: view.Step[] }) {
  return (
    <div className="steps" aria-label="Getting started">
      {steps.map((st, i) => (
        <div key={st.title} className={"step" + (st.done ? " done" : st.now ? " now" : "")}>
          <span className="num">{st.done ? "✓" : i + 1}</span>
          <div className="t">
            <b>{st.title}</b>
            <span>{st.detail}</span>
          </div>
        </div>
      ))}
      <button className="dismiss" onClick={() => void dismissHints()} title="Hide these for good">
        Got it ×
      </button>
    </div>
  );
}

// EmptyLibrary is the LibraryEmpty board: two ways in, and the one idea.
function EmptyLibrary() {
  return (
    <>
      <div className="ways">
        <div className="way drop">
          <h3>Drop a project folder here</h3>
          <p>doze reads what is in it: go.mod, package.json, a compose file, a Procfile. It says what it found before anything starts.</p>
          <button className="btn pri" onClick={() => void chooseFolder()}>
            Open folder… <kbd>⌘O</kbd>
          </button>
        </div>
        <div className="way">
          <h3>Or start empty</h3>
          <p>A new folder with a doze.hcl of a few lines. Add one thing at a time: postgres, valkey, kafka, an app of your own.</p>
          <pre className="ex">
            {'stack = "shop"\n\napp "web" {\n  run  = "pnpm dev"\n  port = 3000\n}\n\nmodule postgres "db" { version = 17 }'}
          </pre>
          <button className="btn" onClick={() => void chooseFolder()} title="Pick an empty folder; the next page writes the file">
            New stack… <kbd>⌘N</kbd>
          </button>
        </div>
      </div>
      <p className="model">
        The one idea behind everything here: <b>doze starts a service when something connects to it, and puts it to sleep when nothing does.</b> A row saying <b>dozing</b> is counting down; <b>asleep</b> wakes on the next connection; <b>pinned</b> never sleeps. Data stays through all of it.
      </p>
    </>
  );
}

function StackRow({ s, selected, menuOpen, onSelect, onMenu, onCloseMenu }: { s: view.LibStack; selected: boolean; menuOpen: boolean; onSelect: () => void; onMenu: () => void; onCloseMenu: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <div ref={ref} role="row" className={"lrow" + (selected ? " sel" : "")} onClick={onSelect} onDoubleClick={() => void go({ key: s.key, page: "home" })} style={{ position: "relative" }}>
      <span className={"dot " + s.dot} aria-hidden />
      <div className="name">
        <b className={s.running ? "" : "dim"}>{s.name}</b>
        <span className="dir" title={s.dir}>
          {s.dir ? (
            <>
              <span className="p"><bdi>{s.dir}</bdi></span>
              <span className="n"> · {s.declared} declared</span>
            </>
          ) : (
            <span className="p">run by an older doze</span>
          )}
        </span>
      </div>
      <div className="fig">
        <span>
          {(s.fig ?? []).map((b, i) => (
            <span key={i} className={b.tone}>
              {b.text}
            </span>
          ))}
        </span>
        <span className="dim">{s.sub}</span>
      </div>
      <div className="pills">
        {s.need && (
          <span className="need">
            ● <span className="t">{s.need}</span>
          </span>
        )}
        {(s.addresses ?? []).map((a) => (
          <AddrPill key={a.text} a={a} />
        ))}
        {(s.more ?? 0) > 0 && (
          <span className="pill dim">
            <span className="t">+{s.more}</span>
          </span>
        )}
      </div>
      <span className="when">{s.busy ? <span className="cool">{s.busy}…</span> : s.when}</span>
      <div className="act">
        <button
          className={"sw big" + ((s.busy ? s.busy !== "stopping" : s.running) ? " on" : "") + (s.busy ? " dis" : "")}
          role="switch"
          aria-checked={s.busy ? s.busy !== "stopping" : s.running}
          aria-busy={!!s.busy}
          disabled={!!s.busy}
          title={s.busy ? `${s.busy}…` : undefined}
          aria-label={s.busy ? `${s.name} is ${s.busy}` : s.running ? `${s.name} is up` : `${s.name} is stopped`}
          onDoubleClick={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation();
            void (s.running ? askStop(s.key) : stackAction(s.key, "up-last"));
          }}
        />
        <button
          className={"ib" + (menuOpen ? " on" : "")}
          aria-label={`More ways to start ${s.name}`}
          onClick={(e) => {
            e.stopPropagation();
            onMenu();
          }}
        >
          ▾
        </button>
      </div>
      {menuOpen && <StackMenu s={s} onClose={onCloseMenu} />}
    </div>
  );
}

function AddrPill({ a }: { a: view.Addr }) {
  const open = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!a.url) return;
    transport().then((t) => {
      t.open(a.url ?? "");
      void say(`opened ${a.url}`);
    });
  };
  return (
    <button className={"pill" + (a.up ? "" : " dim")} onClick={open} title={a.unready ? `${a.text} needs a one-time setup on this machine; the stack's page has it` : a.url ? `Open ${a.url}` : a.text}>
      {a.up && <span className="dot g" aria-hidden />}
      <span className={"t" + (a.url && a.up ? " link" : "")}>{a.text}</span>
    </button>
  );
}

export function StackMenu({ s, onClose }: { s: view.LibStack; onClose: () => void }) {
  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!(e.target as HTMLElement).closest(".pop")) onClose();
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [onClose]);
  const ref = useRef<HTMLDivElement>(null);
  const [up, setUp] = useState(false);
  useLayoutEffect(() => {
    // Near the window's bottom the menu opens upward, so all of it shows.
    const r = ref.current?.getBoundingClientRect();
    if (r && r.bottom > window.innerHeight - 8) setUp(true);
  }, []);
  useEffect(() => {
    // A menu owns the keyboard while it is open: focus starts on its first
    // item, the arrows move, esc closes it and nothing else.
    const items = () => Array.from(ref.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    items()[0]?.focus();
    const onKey = (e: KeyboardEvent) => {
      const all = items();
      const i = all.indexOf(document.activeElement as HTMLButtonElement);
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          e.stopPropagation();
          onClose();
          break;
        case "ArrowDown":
          e.preventDefault();
          e.stopPropagation();
          all[(i + 1) % all.length]?.focus();
          break;
        case "ArrowUp":
          e.preventDefault();
          e.stopPropagation();
          all[(i - 1 + all.length) % all.length]?.focus();
          break;
        case "Tab":
          e.preventDefault();
          all[(i + (e.shiftKey ? all.length - 1 : 1)) % all.length]?.focus();
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);
  const act = (action: string, names: string[] = []) => {
    onClose();
    void stackAction(s.key, action, names);
  };
  return (
    <div ref={ref} className="pop" role="menu" aria-label={`Ways to start ${s.name}`} style={up ? { bottom: "calc(100% - 8px)", right: 16 } : { top: "calc(100% - 8px)", right: 16 }} onClick={(e) => e.stopPropagation()}>
      <div className="ph">Up</div>
      <button className="pi" role="menuitem" onClick={() => act("up-last")}>
        <div className="t">
          <b>As last time</b>
          <span>{s.lastSet && s.lastSet.length > 0 ? s.lastSet.join(", ") + ", and what they need" : "everything declared"}</span>
        </div>
        <kbd>space</kbd>
      </button>
      <button className="pi" role="menuitem" onClick={() => act("up-all")}>
        <div className="t">
          <b>Everything</b>
          <span>all {s.declared}</span>
        </div>
      </button>
      {!s.running && (
        <button className="pi" role="menuitem" onClick={() => {
          onClose();
          void chooseStart(s.key);
        }}>
          <div className="t">
            <b>Choose what starts…</b>
            <span>tick the apps; what they need comes too</span>
          </div>
          <kbd>c</kbd>
        </button>
      )}
      <div className="msep" />
      {s.running ? (
        <button className="pi" role="menuitem" onClick={() => {
          onClose();
          void askStop(s.key);
        }}>
          <div className="t">
            <b>Down</b>
            <span>stops everything; this set is remembered</span>
          </div>
        </button>
      ) : (
        <button className="pi" role="menuitem" onClick={() => act("forget")}>
          <div className="t">
            <b className="dim">Forget this stack</b>
            <span>leaves every file where it is</span>
          </div>
          <kbd>⌫</kbd>
        </button>
      )}
    </div>
  );
}

function Machine({ checks, repairable, repairing }: { checks: view.Check[]; repairable: boolean; repairing: boolean }) {
  return (
    <div className="machine">
      <b>Machine</b>
      {checks.length === 0 && <span className="dim">checking…</span>}
      {checks.map((c) => (
        <span key={c.name} className="it" title={c.text}>
          <span className={"dot " + (c.ok ? "g" : "y")} aria-hidden />
          {c.name}
          {!c.ok && c.text && <span className="dim"> · {c.text}</span>}
        </span>
      ))}
      <span className="grow" />
      {(repairable || repairing) && (
        <button className="btn pri" style={{ height: 26 }} disabled={repairing} onClick={() => void repair()} title="The same setup as doze doctor --fix, behind the system's password prompt">
          {repairing ? "Setting up…" : "Set up (one password)"}
        </button>
      )}
      <button className="btn" style={{ height: 26 }} onClick={() => void go({ key: "", page: "settings", section: "machine" })} title="What doze set up here, part by part">
        Machine…
      </button>
    </div>
  );
}

export const useLibrary = () => useStore((s) => s.library);

// Others is what other doze programs run here: a standalone doze-aws or
// doze-kafka is not a stack, but its consoles are a click away all the same.
function Others({ others }: { others: view.Other[] }) {
  return (
    <section className="others" aria-label="Also on this machine">
      <h4>
        Also on this machine <span className="dim">· run by other doze programs, not by a stack</span>
      </h4>
      {others.map((o) => (
        <div key={o.name} className="other">
          <span className="mono bright">{o.name}</span>
          <span className="dim">
            {o.owner} · pid {o.pid}
          </span>
          <span className="grow" />
          {o.url && (
            <button className="btn quiet" onClick={() => transport().then((t) => t.open(o.url ?? ""))} title={o.url}>
              Open
            </button>
          )}
        </div>
      ))}
    </section>
  );
}
