import { useEffect, useRef } from "react";
import { closeAlert, fixIt, notNow, set, showItem, useStore } from "../store";
import type { view } from "../transport";

// Needs you (D19), as the NeedsYouBell, NeedsYouPanel and BellEmpty boards
// draw it: a bell in the title bar on every page, a red count for what is
// broken, a yellow dot when only things worth doing wait, and a panel with
// each item's fix. Go builds the list; this only draws it and presses.

const BellIcon = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
    <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
    <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
  </svg>
);

export function Bell() {
  const a = useStore((s) => s.attention);
  const open = useStore((s) => s.bell);
  const red = a?.red ?? 0;
  const yellow = a?.yellow ?? 0;
  const label = red > 0 ? `Needs you: ${red} broken now${yellow ? `, ${yellow} worth doing` : ""}` : yellow > 0 ? `Needs you: ${yellow} worth doing` : "Needs you: nothing";
  return (
    <span className="bellw">
      <button
        className={"bell" + (red > 0 ? " hot" : "") + (open ? " open" : "")}
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={`${label}  ⌘⇧N`}
        onClick={() => set((s) => ({ bell: !s.bell, alerts: [], palette: false, away: null }))}
      >
        <BellIcon />
        {red > 0 ? <span className="badge">{red}</span> : yellow > 0 ? <span className="yd" aria-hidden /> : null}
      </button>
      {open && <Panel a={a} />}
    </span>
  );
}

function Panel({ a }: { a: view.Attention | null }) {
  const ref = useRef<HTMLElement>(null);
  const items = a?.items ?? [];
  const red = items.filter((i) => i.red);
  const yellow = items.filter((i) => !i.red);
  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      const t = e.target as HTMLElement;
      if (!t.closest(".np, .bell, .scrim, [role=dialog]")) set({ bell: false });
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);
  const live = useRef(items);
  live.current = items;
  useEffect(() => {
    // Focus starts on the first item, once: a push must not move it.
    const first = ref.current?.querySelector<HTMLElement>(".ni:not(.fixed)");
    (first ?? ref.current)?.focus();
  }, []);
  useEffect(() => {
    // The panel owns the keyboard while focus is in it: ↑↓ move between
    // items, ⏎ their first fix, n not now, ⌘⏎ the page that explains it,
    // esc closes. Anything opened over it (⌘K, a dialog) keeps its keys.
    const rows = () => Array.from(ref.current?.querySelectorAll<HTMLElement>(".ni:not(.fixed)") ?? []);
    const onKey = (e: KeyboardEvent) => {
      if (!ref.current?.contains(document.activeElement)) return;
      const all = rows();
      const i = all.indexOf(document.activeElement as HTMLElement);
      const it = i >= 0 ? live.current.filter((x) => !x.fixed)[i] : undefined;
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          e.stopPropagation();
          set({ bell: false });
          break;
        case "ArrowDown":
          e.preventDefault();
          e.stopPropagation();
          all[Math.min(i + 1, all.length - 1)]?.focus();
          break;
        case "ArrowUp":
          e.preventDefault();
          e.stopPropagation();
          all[Math.max(i - 1, 0)]?.focus();
          break;
        case "Enter":
          if (!it || (document.activeElement as HTMLElement)?.tagName === "BUTTON") return;
          e.preventDefault();
          e.stopPropagation();
          if (e.metaKey) void showItem(it);
          else if (it.fixes?.[0]) void fixIt(it, it.fixes[0]);
          break;
        case "n":
          if (!it || e.metaKey || e.ctrlKey || e.altKey) return;
          e.preventDefault();
          e.stopPropagation();
          void notNow(it);
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);
  return (
    <section ref={ref} className="np" role="dialog" aria-label="Needs you" tabIndex={-1}>
      <div className="nh">
        <h2>Needs you</h2>
        {items.length > 0 && (
          <span className="dim">
            {[(a?.red ?? 0) > 0 && `${a?.red} broken now`, (a?.yellow ?? 0) > 0 && `${a?.yellow} worth doing`].filter(Boolean).join(" · ")}
          </span>
        )}
        <span className="grow" />
        {(a?.hidden ?? 0) > 0 && <span className="dim" title="Put aside with Not now; they come back when what they say changes">{a?.hidden} put aside</span>}
      </div>
      <div className="nbody">
        {items.length === 0 ? (
          <Calm hidden={a?.hidden ?? 0} />
        ) : (
          <>
            {red.length > 0 && (
              <div className="sec r">
                Broken now<span>· something you asked for is not happening</span>
              </div>
            )}
            {red.map((it) => (
              <Row key={it.id} it={it} />
            ))}
            {yellow.length > 0 && (
              <div className="sec y">
                Worth doing<span>· nothing broken; waiting on you</span>
              </div>
            )}
            {yellow.map((it) => (
              <Row key={it.id} it={it} />
            ))}
          </>
        )}
      </div>
      <div className="nf">
        {items.length > 0 ? (
          <>
            <span>
              <kbd>↑↓</kbd>move
            </span>
            <span>
              <kbd>⏎</kbd>first fix
            </span>
            <span>
              <kbd>⌘⏎</kbd>show
            </span>
            <span>
              <kbd>n</kbd>not now
            </span>
            <span className="grow" />
            <span>leaves by itself once fixed</span>
          </>
        ) : (
          <>
            <span>Items leave by themselves once fixed</span>
            <span className="grow" />
            <span>⌘⇧N</span>
          </>
        )}
      </div>
    </section>
  );
}

function Row({ it }: { it: view.Item }) {
  const where = it.stack ? ` · ${it.stack}` : "";
  return (
    <div className={"ni" + (it.fixed ? " fixed" : "")} tabIndex={it.fixed ? undefined : -1} aria-label={`${it.title}${where}${it.fixed ? ", fixed" : ""}`}>
      <span className={"dot " + (it.fixed ? "g" : it.red ? "r" : "y")} aria-hidden />
      <div className="t">
        <b>
          {it.title}
          {where && <span className="dim">{where}</span>}
        </b>
        {it.detail && (
          <span className={it.mono ? "mono" : undefined} title={it.detail}>
            {it.detail}
          </span>
        )}
        {!it.fixed && (
          <div className="fx">
            {(it.fixes ?? []).map((f, i) => (
              <button key={i} className={"btn sm" + (f.primary ? " pri" : "")} onClick={() => void fixIt(it, f)}>
                {f.label}
              </button>
            ))}
            {it.show && !(it.fixes ?? []).some((f) => f.kind === "show") && (
              <button className="btn sm quiet" onClick={() => void showItem(it)}>
                Show
              </button>
            )}
            <button className="nn" onClick={() => void notNow(it)} title="Hide it until what it says changes">
              Not now
            </button>
          </div>
        )}
      </div>
      {it.fixed ? <span className="ok2">fixed ✓</span> : <span className="ago">{ago(it.since)}</span>}
    </div>
  );
}

function Calm({ hidden }: { hidden: number }) {
  return (
    <>
      <div className="calm">
        <div className="ring2" aria-hidden>
          ✓
        </div>
        <h3>{hidden > 0 ? "Nothing new needs you." : "Nothing needs you."}</h3>
        <p>{hidden > 0 ? `${hidden} put aside with Not now; each comes back when what it says changes.` : "Every stack is doing what you asked."}</p>
      </div>
      <div className="what">
        <span className="k r">
          <span className="dot r" aria-hidden />
          Broken now
        </span>
        <span className="v">a crash, a failed task, a service that did not wake in time, a stack that failed to start, a daemon not answering. These also light the tab, the bat and a notification.</span>
        <span className="k y">
          <span className="dot y" aria-hidden />
          Worth doing
        </span>
        <span className="v">doze.hcl changed or not loading, a domain or a machine part not set up, things left behind, a stack whose folder is gone. These wait here quietly.</span>
      </div>
    </>
  );
}

/** Alerts are the toasts for red items that appeared while the window was open: the first fix and Show, until acted on or closed. */
export function Alerts() {
  const alerts = useStore((s) => s.alerts);
  const where = useStore((s) => s.where);
  if (alerts.length === 0) return null;
  return (
    <div className="alerts" role="alert">
      {alerts.map((it) => {
        const fix = it.fixes?.find((f) => f.kind !== "show");
        return (
          <div key={it.id} className="toast act">
            <span className="dot r" aria-hidden />
            <div className="t">
              <b>
                {it.title}
                {it.stack ? ` · ${it.stack}` : ""}
              </b>
              {it.detail && <span>{it.detail}</span>}
              <div className="fx">
                {fix && (
                  <button className="btn pri sm" onClick={() => void fixIt(it, fix)}>
                    {fix.label}
                  </button>
                )}
                {it.show && !(where?.page === it.show.page && where.key === it.show.key && (where.instance ?? "") === (it.show.instance ?? "")) && (
                  <button className="btn sm" onClick={() => void showItem(it)}>
                    Show
                  </button>
                )}
              </div>
            </div>
            <button className="ib" onClick={() => closeAlert(it.id)} aria-label="Close; it stays in Needs you">
              ×
            </button>
          </div>
        );
      })}
    </div>
  );
}

function ago(since?: string): string {
  if (!since) return "";
  const t = Date.parse(since);
  if (!t || t < 0) return "";
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 60) return "now";
  if (s < 3600) return `${Math.round(s / 60)}m`;
  if (s < 86400) return `${Math.round(s / 3600)}h`;
  return `${Math.round(s / 86400)}d`;
}

