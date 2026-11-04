import { useEffect } from "react";
import { go, useBoot, useStore } from "./store";
import { Titlebar } from "./components/titlebar";
import { Sidebar } from "./components/sidebar";
import { HomePage } from "./pages/home";
import { InstancePage } from "./pages/instance";
import { LogsPage } from "./pages/logs";
import { transport } from "./transport";

// The window, as every board lays it out: the title bar with a tab per
// stack, the sidebar, and the page. Pages, tabs and views come whole from
// Go; this decides only which page is mounted.
export function App() {
  useBoot();
  const { stacks, where, home, instance, error, version } = useStore();

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
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [where, stacks]);

  const banner = where?.page === "home" ? home?.banner : where?.page === "instance" ? instance?.banner : undefined;
  const side = where?.page === "home" ? home?.side : where?.page === "instance" ? instance?.side : undefined;

  return (
    <div className="win">
      <Titlebar stacks={stacks} current={where?.key ?? ""} onPick={(key) => void go({ key, page: "home" })} />
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
        </main>
      </div>
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
