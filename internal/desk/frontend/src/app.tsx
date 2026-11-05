import { useEffect } from "react";
import { ask, back, chooseFolder, choose, copy, forward, go, run, say, set, stackAction, up, useBoot, useSlice } from "./store";
import { Titlebar } from "./components/titlebar";
import { Sidebar } from "./components/sidebar";
import { Dialog } from "./components/dialog";
import { Palette } from "./components/palette";
import { Toasts } from "./components/toasts";
import { Help } from "./components/help";
import { LibraryPage } from "./pages/library";
import { OverviewPage } from "./pages/overview";
import { StoppedPage } from "./pages/stopped";
import { InstancePage } from "./pages/instance";
import { LogsPage } from "./pages/logs";
import { RecentPage } from "./pages/recent";
import { TimelinePage } from "./pages/timeline";
import { EmptyPage } from "./pages/empty";
import { SettingsPage } from "./pages/settings";
import { transport, type view } from "./transport";

// The window, as every board lays it out: the title bar with the Library
// tab and a tab per open stack, the page, and the sidebar only inside a
// service. Pages, tabs and views come whole from Go; this decides only
// which page is mounted, and which overlay.
export function App() {
  useBoot();
  const { stacks, where, library, home, instance, empty, error, version, dialog, palette, help } = useSlice((s) => ({
    stacks: s.stacks,
    empty: s.empty,
    where: s.where,
    library: s.library,
    home: s.home,
    instance: s.instance,
    error: s.error,
    version: s.version,
    dialog: s.dialog,
    palette: s.palette,
    help: s.help,
  }));

  useEffect(() => {
    // The first paint, for the desk bench: when the first page is on screen.
    const painted = (where?.page === "library" && library) || (where?.page === "home" && home);
    if (painted && document.documentElement.dataset.painted === undefined) {
      requestAnimationFrame(() => {
        const ms = Math.round(performance.now());
        document.documentElement.dataset.painted = String(ms);
        transport().then((t) => {
          t.log(`${where?.page} painted ${ms}ms after the page loaded`);
          t.painted(ms);
        });
      });
    }
  }, [where, library, home]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (mod && e.key === "0") {
        e.preventDefault();
        void go({ key: "", page: "library" });
      } else if (mod && e.key >= "1" && e.key <= "9") {
        const s = stacks[Number(e.key) - 1];
        if (s) {
          e.preventDefault();
          void go({ key: s.key, page: "home" });
        }
      } else if (mod && e.key.toLowerCase() === "o") {
        e.preventDefault();
        void chooseFolder();
      } else if (mod && e.key.toLowerCase() === "l" && where?.key) {
        e.preventDefault();
        void go({ key: where.key, page: "logs" });
      } else if (mod && e.key === ",") {
        e.preventDefault();
        void go({ key: "", page: "settings" });
      } else if (mod && e.key.toLowerCase() === "k") {
        e.preventDefault();
        set((s) => ({ palette: !s.palette, dialog: null, help: false }));
      } else if (e.key === "?" && !(e.target as HTMLElement).closest("input, textarea, select")) {
        e.preventDefault();
        set((s) => ({ help: !s.help, palette: false }));
      } else if (mod && e.key === "[") {
        e.preventDefault();
        void back();
      } else if (mod && e.key === "]") {
        e.preventDefault();
        void forward();
      } else if ((e.key === "Escape" || (mod && e.key.toLowerCase() === "w")) && !(e.target as HTMLElement).closest("input, textarea, select") && !dialog && !palette && !help) {
        // The same move on every page: up a level. ⌘W on a pending folder
        // closes it the same way, without writing anything.
        e.preventDefault();
        void up();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [where, stacks, dialog, palette, help]);

  const banner = where?.page === "home" ? home?.banner : where?.page === "instance" ? instance?.banner : undefined;
  const side = where?.page === "home" ? home?.side : where?.page === "instance" ? instance?.side : undefined;
  const onLibrary = where?.page === "library";
  const withSidebar = where?.page === "instance" || where?.page === "logs";

  // What a palette row does. Every row names its stack, since ⌘K reaches
  // across all of them.
  const pick = (d: view.Doing) => {
    set({ palette: false });
    const key = d.stackKey || where?.key || "";
    switch (d.kind) {
      case "verb":
        if (d.ask && d.target) void ask(key, d.target, d.verb ?? "");
        else void run(key, d.verb ?? "", d.target ?? "");
        break;
      case "page":
        if (d.page === "home") void go({ key, page: "home" });
        else void go({ key, page: (d.page as "logs" | "instance") || "instance", instance: d.target });
        break;
      case "copy":
        void copy(d.text ?? "", `the connect line of ${d.target}`);
        break;
      case "open":
        transport().then((t) => {
          t.open(d.text ?? "");
          void say(`opened ${d.text}`);
        });
        break;
      case "stack":
        void stackAction(key, d.verb ?? "up-last");
        break;
      case "folder":
        void chooseFolder();
        break;
    }
  };

  return (
    <div className="win">
      <Titlebar
        stacks={stacks}
        current={where?.key ?? ""}
        library={onLibrary || where?.page === "settings"}
        onLibrary={() => void go({ key: "", page: "library" })}
        pending={where?.page === "empty" && empty ? { name: empty.name, dir: empty.dir } : undefined}
        onPick={(key) => void go({ key, page: "home" })}
        onCommand={() => set({ palette: true, dialog: null })}
      />
      <div className="body">
        {withSidebar && <Sidebar where={where} side={side ?? []} version={version} stacks={stacks.length} />}
        <main className="main" data-page={where?.page ?? "none"}>
          {banner && !banner.ok && (
            <div className="banner warn" role="status">
              <span className="ring" aria-hidden />
              <div className="t">
                <b>{banner.text}</b>
              </div>
            </div>
          )}
          {error && (
            <div className="banner bad" role="alert">
              <span className="dot r" aria-hidden />
              <div className="t">
                <b>{error}</b>
              </div>
            </div>
          )}
          {!where && <Loading />}
          {where?.page === "library" && library && <LibraryPage lib={library} />}
          {where?.page === "home" && home && (home.stopped ? <StoppedPage home={home} stackKey={where.key} /> : <OverviewPage home={home} stackKey={where.key} />)}
          {where?.page === "instance" && instance && <InstancePage inst={instance} stackKey={where.key} />}
          {where?.page === "logs" && <LogsPage stackKey={where.key} instance={where.instance ?? ""} />}
          {where?.page === "empty" && empty && <EmptyPage e={empty} />}
          {where?.page === "timeline" && <TimelinePage stackKey={where.key} />}
          {where?.page === "recent" && <RecentPage stackKey={where.key} stack={stacks.find((s) => s.key === where.key)?.name ?? ""} />}
          {where?.page === "settings" && <SettingsPage library={library} version={version} />}
        </main>
      </div>
      {dialog && <Dialog d={dialog.d} onChoose={(b, text) => void choose(b, text)} onCancel={() => set({ dialog: null })} />}
      {palette && where && <Palette stackKey={where.key} onPick={pick} onClose={() => set({ palette: false })} />}
      {help && <Help version={version} onClose={() => set({ help: false })} />}
      <Toasts />
    </div>
  );
}

function Loading() {
  return (
    <div className="empty">
      <p className="dim">Loading…</p>
    </div>
  );
}
