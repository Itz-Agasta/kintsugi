import { cn } from "cn";
import { Suspense, lazy, useCallback, useEffect, useReducer, useRef, useState } from "react";
import { SidebarSimpleIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Graph as GraphView } from "@/components/dashboard/Graph";
import { ChromeStrip, ProgressStrip, RunHeader, type Times } from "@/components/dashboard/Header";
import { Dock } from "@/components/dashboard/Dock";
import { ViewSwitch } from "@/components/dashboard/ViewSwitch";
import { DiffView } from "@/components/dashboard/DiffView";
import { DraftsView } from "@/components/dashboard/DraftsView";
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

// Three.js is large; it loads the first time the 3D view is opened.
const Atlas3D = lazy(() =>
  import("@/components/dashboard/Atlas3D").then((m) => ({ default: m.Atlas3D })),
);

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
    setDismissed(null);
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
        setDismissed(null);
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
  const tab = s.phase === "repaired" ? s.tab : "overview";
  const [rail, setRail] = useState(false);
  const [view, setView] = useState<"2d" | "3d">("2d");
  // The drawer opens on a selection, and on its own when flagged or repaired
  // until the viewer closes it for that phase.
  const [dismissed, setDismissed] = useState<string | null>(null);
  const drawer =
    s.selected !== null ||
    ((s.phase === "flagged" || s.phase === "repaired") && dismissed !== s.phase);

  // Keys: R reset, [ ticket rail, 2 and 3 the graph view, Esc closes the drawer.
  const keys = useRef<Record<string, () => void>>({});
  useEffect(() => {
    keys.current = {
      r: reset,
      "[": () => setRail((r) => !r),
      "2": () => setView("2d"),
      "3": () => setView("3d"),
      Escape: () => {
        select(null);
        setDismissed(s.phase);
      },
    };
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return;
      keys.current[e.key.length === 1 ? e.key.toLowerCase() : e.key]?.();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <TooltipProvider>
      <div className="grid h-svh grid-rows-[auto_minmax(0,1fr)] gap-2.5 p-2.5 max-[1000px]:h-auto">
        <ChromeStrip
          graphId={`demo-${sessionId()}`}
          repairId={s.report?.repair_id ?? null}
          up={up}
          onCypher={() => setShowCypher(true)}
          onReset={reset}
          resetDisabled={false}
        />
        {/* The ticket list runs the full height on the left; the run header
            and the stage share the column to its right. */}
        <div className="flex min-h-0 gap-2.5 max-[1000px]:flex-col">
          <SpanTree s={s} onSelect={select} rail={rail} onToggle={() => setRail((r) => !r)} />
          {/* One instrument: the run header, the stage bar and the stage share a panel. */}
          <section className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[10px] border border-rule bg-panel max-[1000px]:min-h-[720px]">
            <RunHeader
              s={s}
              times={times}
              now={now}
              onPrimary={primary}
              onTab={(tab) => dispatch({ type: "tab", tab })}
            />
            {tab === "diff" && <DiffView s={s} cypher={cypher} />}
            {tab === "drafts" && <DraftsView s={s} />}
            {/* Hidden rather than unmounted on the other tabs, so the graph keeps its canvas. */}
            <div className={cn("flex min-h-0 flex-1 flex-col", tab !== "overview" && "hidden")}>
              <div className="flex h-11 shrink-0 items-center justify-between border-b border-rule px-3.5 font-sans font-medium">
                Provenance
                <div className="flex items-center gap-5">
                  <ProgressStrip s={s} />
                  {!drawer && (s.phase === "flagged" || s.phase === "repaired") && (
                    // Reopens the drawer's phase summary after it was closed.
                    <Button size="sm" onClick={() => setDismissed(null)}>
                      <SidebarSimpleIcon size={14} className="-scale-x-100" />
                      {s.phase === "repaired" ? "Report" : "Blast radius"}
                    </Button>
                  )}
                  <ViewSwitch view={view} onView={setView} />
                </div>
              </div>
              <div className="relative min-h-0 flex-1">
                {view === "3d" ? (
                  <Suspense fallback={<div className="absolute inset-0 bg-canvas" />}>
                    <Atlas3D s={s} onSelect={select} inset={drawer ? 396 : 0} />
                  </Suspense>
                ) : (
                  <GraphView s={s} onSelect={select} inset={drawer ? 396 : 0} />
                )}
                <Inspector
                  s={s}
                  cypher={cypher}
                  open={drawer}
                  onClose={() => {
                    select(null);
                    setDismissed(s.phase);
                  }}
                />
              </div>
              <Dock s={s} onSelect={select} />
            </div>
          </section>
        </div>
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
