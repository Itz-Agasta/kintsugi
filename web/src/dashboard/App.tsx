import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Footer } from "@/components/dashboard/Footer";
import { Graph as GraphView } from "@/components/dashboard/Graph";
import { ChromeStrip, RunHeader, type Times } from "@/components/dashboard/Header";
import { LogStrip } from "@/components/dashboard/LogStrip";
import { Inspector } from "@/components/dashboard/Inspector";
import { SpanTree } from "@/components/dashboard/SpanTree";
import { CypherBlock } from "@/components/CypherBlock";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { ApiError, getJSON, health, openStream, post, sessionId } from "@/lib/api";
import {
  SOURCE,
  type Diff,
  type Graph,
  type Path,
  type RepairEvent,
  type Report,
  type RunEvent,
} from "@/lib/types";
import { busy, clock, initial, reduce } from "./state";

const NO_TIMES: Times = { flagged: null, started: null, ended: null };

function message(e: unknown): string {
  return e instanceof ApiError || e instanceof Error ? e.message : String(e);
}

export default function App() {
  const [s, dispatch] = useReducer(reduce, initial);
  const [up, setUp] = useState<boolean | null>(null);
  const [cypher, setCypher] = useState<Record<string, string>>({});
  const [times, setTimes] = useState<Times>(NO_TIMES);
  const [now, setNow] = useState(() => Date.now());
  const stream = useRef<(() => void) | null>(null);
  const graphReq = useRef({ sent: 0, applied: 0 });

  // refetchGraph drops responses older than one already applied.
  const refetchGraph = useCallback(async () => {
    const id = ++graphReq.current.sent;
    const graph = await getJSON<Graph>("/api/graph");
    if (id < graphReq.current.applied) return;
    graphReq.current.applied = id;
    dispatch({ type: "graph", graph });
  }, []);

  useEffect(() => {
    void health().then(setUp);
    getJSON<Record<string, string>>("/api/cypher").then(setCypher, () => {});
    getJSON<Graph>("/api/graph").then(
      (graph) => dispatch({ type: "restore", graph }),
      (e: unknown) => dispatch({ type: "fail", message: message(e) }),
    );
    return () => stream.current?.();
  }, []);

  const isBusy = busy(s);
  useEffect(() => {
    if (!isBusy) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [isBusy]);

  const endStream = () => {
    stream.current = null;
  };

  const run = () => {
    if (stream.current || busy(s)) return;
    setTimes(NO_TIMES);
    dispatch({ type: "run/start" });
    let last = 0;
    stream.current = openStream(
      "/api/run/stream",
      {},
      {
        action: (d) => {
          dispatch({ type: "run/action", ev: d as RunEvent });
          if (Date.now() - last > 1000) {
            last = Date.now();
            refetchGraph().catch(() => {});
          }
        },
        done: () => {
          endStream();
          refetchGraph().then(
            () => dispatch({ type: "run/done" }),
            (e: unknown) => dispatch({ type: "fail", message: message(e) }),
          );
        },
        error: (m) => {
          endStream();
          dispatch({ type: "fail", message: String(m) });
        },
      },
      ["done"],
    );
  };

  const flag = () => {
    getJSON<string[]>("/api/blast", { source: SOURCE }).then(
      (ids) => {
        setTimes({ ...NO_TIMES, flagged: clock.now() });
        dispatch({ type: "blast", ids });
      },
      (e: unknown) => dispatch({ type: "fail", message: message(e) }),
    );
  };

  const repair = () => {
    if (stream.current || busy(s)) return;
    setTimes((t) => ({ ...t, started: Date.now(), ended: null }));
    setNow(Date.now());
    dispatch({ type: "repair/start" });
    stream.current = openStream(
      "/api/repair/stream",
      { source: SOURCE },
      {
        action: (d) => dispatch({ type: "repair/action", ev: d as RepairEvent }),
        report: (d) => dispatch({ type: "repair/report", report: d as Report }),
        diff: (d) => {
          endStream();
          setTimes((t) => ({ ...t, ended: Date.now() }));
          dispatch({ type: "repair/diff", diff: d as Diff });
          refetchGraph().catch(() => {});
        },
        error: (m) => {
          endStream();
          setTimes((t) => ({ ...t, started: null }));
          dispatch({ type: "fail", message: String(m) });
        },
      },
      ["diff"],
    );
  };

  const reset = () => {
    stream.current?.();
    endStream();
    post("/api/reset").then(
      () => {
        setTimes(NO_TIMES);
        dispatch({ type: "reset" });
      },
      (e: unknown) => dispatch({ type: "fail", message: message(e) }),
    );
  };

  const primary = () => {
    if (s.phase === "idle") run();
    else if (s.phase === "ran") flag();
    else if (s.phase === "flagged") repair();
  };

  // select also fetches the causal path once a source is flagged. A slower
  // answer for an older selection is dropped.
  const selected = useRef<string | null>(null);
  const select = (id: string | null) => {
    selected.current = id;
    dispatch({ type: "select", id });
    if (!id || s.blast.length === 0) return;
    getJSON<Path>("/api/path", { source: SOURCE, action: id }).then(
      (path) => selected.current === id && dispatch({ type: "path", path }),
      () => selected.current === id && dispatch({ type: "path", path: null }),
    );
  };
  const [showCypher, setShowCypher] = useState(false);

  const resetRef = useRef(reset);
  useEffect(() => {
    resetRef.current = reset;
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "r" && e.key !== "R") return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return;
      resetRef.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <TooltipProvider>
      <div className="grid h-svh grid-rows-[auto_auto_minmax(0,1fr)_auto] gap-2.5 p-2.5 max-[1000px]:h-auto">
        <ChromeStrip
          graphId={`demo-${sessionId()}`}
          repairId={s.report?.repair_id ?? null}
          up={up}
          onCypher={() => setShowCypher(true)}
          onReset={reset}
          resetDisabled={false}
        />
        <RunHeader
          s={s}
          times={times}
          now={now}
          onPrimary={primary}
          onTab={(tab) => dispatch({ type: "tab", tab })}
        />
        <div className="grid min-h-0 grid-cols-[340px_minmax(0,1fr)_360px] gap-2.5 max-[1000px]:grid-cols-1">
          <SpanTree s={s} onSelect={select} />
          <section className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-rule bg-panel">
            <div className="flex h-11 items-center justify-between border-b border-rule px-3.5 font-sans font-medium">
              Provenance
              <small className="text-[12.5px] font-normal text-faint">
                what each action read, and who it handed off to
              </small>
            </div>
            <GraphView s={s} onSelect={select} />
            <LogStrip log={s.log} />
          </section>
          <Inspector s={s} cypher={cypher} />
        </div>
        <Footer />
        <Dialog open={showCypher} onOpenChange={setShowCypher}>
          <DialogContent title="Cypher kintsugi runs">
            <p className="mb-2 font-sans text-sm leading-[22px] text-muted">
              Every query the repair depends on, as the server sends it.
            </p>
            {Object.entries(cypher).map(([name, q]) => (
              <CypherBlock key={name} title={name} query={q} />
            ))}
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  );
}
