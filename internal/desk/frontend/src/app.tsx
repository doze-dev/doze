import { useEffect } from "react";
import { ask, choose, copy, go, run, set, setTheme, useBoot, useSlice, type Theme } from "./store";
import { Titlebar } from "./components/titlebar";
import { Sidebar } from "./components/sidebar";
import { Dialog } from "./components/dialog";
import { Palette } from "./components/palette";
import { Toasts } from "./components/toasts";
import { HomePage } from "./pages/home";
import { InstancePage } from "./pages/instance";
import { LogsPage } from "./pages/logs";
import { RecentPage } from "./pages/recent";
import { transport, type view } from "./transport";

// The window, as every board lays it out: the title bar with a tab per
// stack, the sidebar, and the page. Pages, tabs and views come whole from
// Go; this decides only which page is mounted, and which overlay.
export function App() {
  useBoot();
  const { stacks, where, home, instance, error, version, dialog, palette, selected } = useSlice((s) => ({
    stacks: s.stacks,
    where: s.where,
    home: s.home,
    instance: s.instance,
    error: s.error,
    version: s.version,
    dialog: s.dialog,
    palette: s.palette,
    selected: s.selected,
  }));

  useEffect(() => {
    // The first paint, for the desk bench: when the home page is on screen.
    if (home && document.documentElement.dataset.painted === undefined) {
      requestAnimationFrame(() => {
        const ms = Math.round(performance.now());
        document.documentElement.dataset.painted = String(ms);
        transport().then((t) => {
          t.log(`home painted ${ms}ms after the page loaded`);
          t.painted(ms);
        });
      });
    }
  }, [home]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!where) return;
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key >= "1" && e.key <= "9") {
        const s = stacks[Number(e.key) - 1];
        if (s) {
          e.preventDefault();
          void go({ key: s.key, page: "home" });
        }
      } else if (mod && e.key.toLowerCase() === "l") {
        e.preventDefault();
        void go({ key: where.key, page: "logs" });
      } else if (mod && e.key.toLowerCase() === "k") {
        e.preventDefault();
        set((s) => ({ palette: !s.palette, dialog: null }));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [where, stacks]);

  const banner = where?.page === "home" ? home?.banner : where?.page === "instance" ? instance?.banner : undefined;
  const side = where?.page === "home" ? home?.side : where?.page === "instance" ? instance?.side : undefined;
  const picked = where?.page === "instance" ? (where.instance ?? "") : selected;

  const pick = (row: view.PaletteRow) => {
    if (!where) return;
    set({ palette: false });
    const d = row.do;
    switch (d.kind) {
      case "verb":
        if (d.ask && d.target) void ask(where.key, d.target, d.verb ?? "");
        else void run(where.key, d.verb ?? "", d.target ?? "");
        break;
      case "page":
        void go({ key: where.key, page: (d.page as "logs") || "logs", instance: d.target });
        break;
      case "copy":
        void copy(d.text ?? "", `the connect line of ${d.target}`);
        break;
      case "recent":
        void go({ key: where.key, page: "recent" });
        break;
      case "theme":
        void setTheme((d.text as Theme) || "system");
        break;
    }
  };

  return (
    <div className="win">
      <Titlebar stacks={stacks} current={where?.key ?? ""} onPick={(key) => void go({ key, page: "home" })} onCommand={() => set({ palette: true, dialog: null })} />
      <div className="body">
        <Sidebar where={where} side={side ?? []} version={version} stacks={stacks.length} />
        <main className="main" data-page={where?.page ?? "none"}>
          {banner && !banner.ok && (
            <div className="banner" role="status">
              {banner.text}
            </div>
          )}
          {error && (
            <p className="notice bad" role="alert">
              {error}
            </p>
          )}
          {!where && <Empty count={stacks.length} />}
          {where?.page === "home" && home && <HomePage home={home} stackKey={where.key} />}
          {where?.page === "instance" && instance && <InstancePage inst={instance} stackKey={where.key} />}
          {where?.page === "logs" && <LogsPage stackKey={where.key} instance={where.instance ?? ""} />}
          {where?.page === "recent" && <RecentPage stackKey={where.key} stack={stacks.find((s) => s.key === where.key)?.name ?? ""} />}
        </main>
      </div>
      {dialog && <Dialog d={dialog.d} onChoose={(b, text) => void choose(b, text)} onCancel={() => set({ dialog: null })} />}
      {palette && where && <Palette stackKey={where.key} selected={picked} onPick={pick} onClose={() => set({ palette: false })} />}
      <Toasts />
    </div>
  );
}

function Empty({ count }: { count: number }) {
  return (
    <div className="empty">
      <p>{count === 0 ? "No stack is running." : "Loading…"}</p>
      {count === 0 && (
        <p className="small">
          <code>doze up</code> in a project, and it appears here.
        </p>
      )}
    </div>
  );
}
