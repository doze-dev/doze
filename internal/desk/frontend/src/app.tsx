import { useEffect } from "react";
import { go, useBoot, useStore } from "./store";
import { Tabs } from "./components/tabs";
import { Banner } from "./components/banner";
import { HomePage } from "./pages/home";
import { InstancePage } from "./pages/instance";
import { LogsPage } from "./pages/logs";
import { transport } from "./transport";

// The shell: the tab strip across the top, one page below it. Pages, tabs
// and views come whole from Go; this decides only which page is mounted.
export function App() {
  useBoot();
  const { stacks, where, home, instance, error } = useStore();

  useEffect(() => {
    // The first paint, for the desk bench: when the home page is on screen.
    if (home && document.documentElement.dataset.painted === undefined) {
      requestAnimationFrame(() => {
        const ms = Math.round(performance.now());
        document.documentElement.dataset.painted = String(ms);
        transport().then((t) => t.painted(ms));
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
      } else if (e.key === "Escape" && where.page !== "home") {
        void go({ key: where.key, page: "home" });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [where, stacks]);

  const banner = where?.page === "home" ? home?.banner : where?.page === "instance" ? instance?.banner : undefined;

  return (
    <main className="shell">
      <Tabs stacks={stacks} current={where?.key ?? ""} onPick={(key) => void go({ key, page: "home" })} />
      <section className="page" data-page={where?.page ?? "none"}>
        {banner && !banner.ok && <Banner text={banner.text ?? ""} />}
        {error && (
          <p className="notice bad" role="alert">
            {error}
          </p>
        )}
        {!where && <Empty count={stacks.length} />}
        {where?.page === "home" && home && <HomePage home={home} stackKey={where.key} />}
        {where?.page === "instance" && instance && <InstancePage inst={instance} stackKey={where.key} />}
        {where?.page === "logs" && <LogsPage stackKey={where.key} instance={where.instance ?? ""} />}
      </section>
    </main>
  );
}

function Empty({ count }: { count: number }) {
  return (
    <div className="empty">
      <p className="muted">{count === 0 ? "No stack is running." : "Loading…"}</p>
      {count === 0 && (
        <p className="muted small">
          <code>doze up</code> in a project, and it appears here.
        </p>
      )}
    </div>
  );
}
