import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { scaleLinear } from "d3-scale";
import {
  CaretDownIcon,
  CaretUpIcon,
  ChartBarHorizontalIcon,
  TerminalIcon,
} from "@phosphor-icons/react";
import { cn } from "cn";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { LogLine, State } from "@/dashboard/state";
import type { RepairEvent } from "@/lib/types";

// The query each repair event came from, as named in the server's queries.
const QUERY: Record<string, string> = {
  rerun_changed: "MustRerun",
  rerun_same: "MustRerun",
  skipped: "MustRerun",
  poisoned: "PoisonedActions",
  superseded: "Supersede",
  new: "Created",
};

const OUTCOME: Record<string, string> = {
  rerun_changed: "re-ran, changed",
  rerun_same: "re-ran, same",
  skipped: "skipped",
  poisoned: "poisoned",
  superseded: "superseded",
  new: "created",
};

function Row({
  l,
  onSelect,
  selected,
}: {
  l: LogLine;
  onSelect: (id: string) => void;
  selected: boolean;
}) {
  const ev = l.ev;
  if (!ev) {
    return (
      <div
        className={cn(
          "grid grid-cols-[76px_1fr] px-3",
          l.error ? "text-text" : l.gold ? "text-gold" : "text-muted",
        )}
      >
        <span className="text-faint">{l.t}</span>
        <span className="truncate">{l.text}</span>
      </div>
    );
  }
  const repaired = ev.status === "rerun_changed" || ev.status === "new";
  return (
    <button
      type="button"
      onClick={() => onSelect(ev.action_id)}
      className={cn(
        "grid w-full grid-cols-[76px_128px_150px_70px_76px_1fr] px-3 text-left hover:bg-raised",
        selected && "bg-raised",
      )}
    >
      <span className="text-faint">{l.t}</span>
      <span className="text-muted">{QUERY[ev.status]}</span>
      <span className="truncate text-text">
        {ev.tool} {ev.ticket_id || ""}
      </span>
      <span className={ev.must_rerun === undefined ? "text-faint" : "text-text"}>
        {ev.must_rerun === undefined ? "" : String(ev.must_rerun)}
      </span>
      <span className="text-faint">
        {ev.query_ms !== undefined ? `${ev.query_ms.toFixed(2)}ms` : ""}
      </span>
      <span
        className={
          repaired ? "text-gold" : ev.status === "superseded" ? "text-faint" : "text-muted"
        }
      >
        {OUTCOME[ev.status]}
      </span>
    </button>
  );
}

function Console({ s, onSelect }: { s: State; onSelect: (id: string) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const lines = [...s.log].reverse();
  // Stick to the newest line, as a terminal does.
  useLayoutEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [s.log]);
  return (
    <div ref={box} className="h-full overflow-auto py-1.5 font-mono text-xs leading-[19px]">
      {lines.length === 0 && (
        <div className="px-3 text-faint">Queries and results appear here as the graph changes.</div>
      )}
      {lines.map((l, i) => (
        <Row key={i} l={l} onSelect={onSelect} selected={!!l.ev && l.ev.action_id === s.selected} />
      ))}
    </div>
  );
}

const BAR: Record<string, string> = {
  rerun_changed: "fill-gold",
  new: "fill-transparent stroke-gold",
  rerun_same: "fill-muted",
  skipped: "fill-transparent stroke-muted",
  superseded: "fill-transparent stroke-faint [stroke-dasharray:3_2]",
  poisoned: "fill-text",
};

// Trace is the repair as a span waterfall: one row per decided action,
// placed at when its decision began and as long as it took.
function Trace({ s, onSelect }: { s: State; onSelect: (id: string) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(600);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setW(el.clientWidth));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const evs = s.log
    .map((l) => l.ev)
    .filter((e): e is RepairEvent => !!e && e.start_ms !== undefined)
    .reverse();
  // One wrapper holds the ref in both states, so the width is observed from
  // the first render on.
  return (
    <div ref={box} className="h-full overflow-auto">
      {evs.length === 0 ? (
        <div className="px-3 py-2 font-mono text-xs text-faint">
          The repair trace appears here while it runs.
        </div>
      ) : (
        <Waterfall evs={evs} w={w} s={s} onSelect={onSelect} />
      )}
    </div>
  );
}

function Waterfall({
  evs,
  w,
  s,
  onSelect,
}: {
  evs: RepairEvent[];
  w: number;
  s: State;
  onSelect: (id: string) => void;
}) {
  const LABEL = 150;
  const ROW = 14;
  const end = Math.max(...evs.map((e) => (e.start_ms ?? 0) + (e.dur_ms ?? 0)), 1);
  const x = scaleLinear()
    .domain([0, end])
    .range([LABEL, w - 16]);
  const h = 22 + evs.length * ROW;
  return (
    <svg width={w} height={h} className="block font-mono text-[10.5px]">
      {x.ticks(8).map((t) => (
        <g key={t}>
          <line x1={x(t)} x2={x(t)} y1={16} y2={h} className="stroke-rule" />
          <text x={x(t)} y={11} textAnchor="middle" className="fill-faint">
            {t >= 1000 ? `${(t / 1000).toFixed(1)}s` : `${t}ms`}
          </text>
        </g>
      ))}
      {evs.map((e, i) => {
        const y = 20 + i * ROW;
        const sel = e.action_id === s.selected;
        return (
          <g
            key={e.action_id + e.status}
            className="cursor-pointer"
            onClick={() => onSelect(e.action_id)}
          >
            <rect
              x={0}
              y={y - 1}
              width={w}
              height={ROW}
              className={sel ? "fill-raised" : "fill-transparent"}
            />
            <text x={12} y={y + 9} className={sel ? "fill-text" : "fill-muted"}>
              {e.tool} {e.ticket_id}
            </text>
            <rect
              x={x(e.start_ms ?? 0)}
              y={y + 2}
              width={Math.max(2, x((e.start_ms ?? 0) + (e.dur_ms ?? 0)) - x(e.start_ms ?? 0))}
              height={ROW - 5}
              rx={2}
              className={cn(BAR[e.status], "stroke-1")}
            />
            {e.query_ms !== undefined && (
              // The MustRerun query's share of the decision.
              <rect
                x={x(e.start_ms ?? 0)}
                y={y + ROW - 4}
                width={Math.max(1.5, x((e.start_ms ?? 0) + e.query_ms) - x(e.start_ms ?? 0))}
                height={1.5}
                className="fill-text"
              />
            )}
          </g>
        );
      })}
    </svg>
  );
}

export function Dock({ s, onSelect }: { s: State; onSelect: (id: string) => void }) {
  const [tab, setTab] = useState<"console" | "trace">("console");
  const [open, setOpen] = useState(true);
  // Switch to the trace once the repair is done, where it has the most to show.
  useEffect(() => {
    if (s.phase === "repaired" && s.log.some((l) => l.ev)) setTab("trace");
  }, [s.phase]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <div
      className={cn(
        "flex shrink-0 flex-col border-t border-rule bg-panel",
        open ? "h-[210px]" : "h-9",
      )}
    >
      <div className="flex h-9 shrink-0 items-stretch justify-between border-b border-rule px-3">
        <Tabs value={tab} onValueChange={(v) => setTab(v as "console" | "trace")}>
          <TabsList className="h-full">
            <TabsTrigger value="console">
              <TerminalIcon size={14} />
              Console
            </TabsTrigger>
            <TabsTrigger value="trace">
              <ChartBarHorizontalIcon size={14} />
              Trace
            </TabsTrigger>
          </TabsList>
        </Tabs>
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-label={open ? "Collapse" : "Expand"}
          className="grid w-8 place-items-center text-faint hover:text-text"
        >
          {open ? <CaretDownIcon size={14} /> : <CaretUpIcon size={14} />}
        </button>
      </div>
      {open && (
        <div className="min-h-0 flex-1">
          {tab === "console" ? (
            <Console s={s} onSelect={onSelect} />
          ) : (
            <Trace s={s} onSelect={onSelect} />
          )}
        </div>
      )}
    </div>
  );
}
