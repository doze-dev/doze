import { go, run } from "../store";
import type { view } from "../transport";
import { Meters } from "../components/meters";
import { Table } from "../components/table";

// Home: the meters, then the stack as the dash's table.
export function HomePage({ home, stackKey }: { home: view.Home; stackKey: string }) {
  return (
    <div className={"home" + (home.banner.ok ? "" : " faded")}>
      <header className="page-head">
        <h1>{home.stack}</h1>
        <span className="muted">{home.dir}</span>
        <span className="muted">
          · {home.awake} of {home.total} awake
        </span>
        <span className="verbs">
          <button onClick={() => void go({ key: stackKey, page: "logs" })} title="⌘L">
            all logs
          </button>
        </span>
      </header>
      <Meters meters={home.meters} />
      <Table
        groups={home.groups ?? []}
        onOpen={(name) => void go({ key: stackKey, page: "instance", instance: name })}
        onVerb={(verb, name) => void run(stackKey, verb, name)}
      />
    </div>
  );
}
