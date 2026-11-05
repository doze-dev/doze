import { useEffect, useRef, useState } from "react";
import { chooseFolder, dismissHints, go, repair, say, stackAction, useStore } from "../store";
import { transport, type view } from "../transport";
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
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
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
          if (row) void stackAction(row.key, row.running ? "down" : "up-last");
          break;
        case "Enter":
          if (row) void go({ key: row.key, page: "home" });
          break;
        case "Backspace":
          if (row && !row.running) void stackAction(row.key, "forget");
          break;
        case "Escape":
          setMenu(null);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [stacks, row]);

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
          Drop a project folder anywhere in this window to add it. A stack appears here the first time it runs, and stays until you forget it.
        </p>
      )}
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
          <span className="p"><bdi>{s.dir}</bdi></span>
          <span className="n"> · {s.declared} declared</span>
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
      <span className="when">{s.when}</span>
      <div className="act">
        <button
          className={"sw big" + (s.running ? " on" : "")}
          role="switch"
          aria-checked={s.running}
          aria-label={s.running ? `${s.name} is up` : `${s.name} is down`}
          onClick={(e) => {
            e.stopPropagation();
            void stackAction(s.key, s.running ? "down" : "up-last");
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
    <button className={"pill" + (a.up ? "" : " dim")} onClick={open} title={a.url ? `Open ${a.url}` : a.text}>
      {a.up && <span className="dot g" aria-hidden />}
      <span className={"t" + (a.url && a.up ? " link" : "")}>{a.text}</span>
    </button>
  );
}

export function StackMenu({ s, onClose }: { s: view.LibStack; onClose: () => void }) {
  const [ticked, setTicked] = useState<Record<string, boolean>>(() => Object.fromEntries((s.choices ?? []).map((c) => [c.name, c.on])));
  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!(e.target as HTMLElement).closest(".pop")) onClose();
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [onClose]);
  const act = (action: string, names: string[] = []) => {
    onClose();
    void stackAction(s.key, action, names);
  };
  const chosen = Object.entries(ticked)
    .filter(([, on]) => on)
    .map(([n]) => n);
  return (
    <div className="pop" role="menu" aria-label={`Ways to start ${s.name}`} style={{ top: "calc(100% - 8px)", right: 16 }} onClick={(e) => e.stopPropagation()}>
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
      {(s.choices ?? []).length > 0 && (
        <>
          <div className="ph">Choose what runs</div>
          {(s.choices ?? []).map((c) => (
            <button key={c.name} className="tick" role="menuitemcheckbox" aria-checked={!!ticked[c.name]} onClick={() => setTicked((t) => ({ ...t, [c.name]: !t[c.name] }))}>
              <span className={"box" + (ticked[c.name] ? " on" : "")}>{ticked[c.name] ? "✓" : ""}</span>
              {c.name}
              {c.brings && <span className="dim">{c.brings}</span>}
            </button>
          ))}
          <button className="pi" role="menuitem" disabled={chosen.length === 0} onClick={() => act("up", chosen)}>
            <div className="t">
              <b>Up the ticked</b>
              <span>{chosen.length === 0 ? "tick at least one" : chosen.join(", ")}</span>
            </div>
          </button>
        </>
      )}
      <div className="msep" />
      {s.running ? (
        <button className="pi" role="menuitem" onClick={() => act("down")}>
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
      <button className="btn" style={{ height: 26 }} onClick={() => void go({ key: "", page: "settings" })} title="What doze set up here, part by part">
        Doctor
      </button>
    </div>
  );
}

export const useLibrary = () => useStore((s) => s.library);
