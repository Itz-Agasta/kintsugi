// Dashboard state. Every stream event and fetch result is one message into
// `reduce`; components render from the State it returns.

import type {
  Diff,
  Graph,
  GraphNode,
  Path,
  RepairEvent,
  Report,
  RunEvent,
  Status,
} from "@/lib/types";

export type Phase = "idle" | "running" | "ran" | "flagged" | "repairing" | "repaired";
export type UiStatus = Status | "pending" | "running";
export type Tab = "overview" | "diff" | "drafts";
export type LogLine = { t: string; text: string; gold?: boolean; error?: boolean };
export type State = {
  phase: Phase;
  before: Phase;
  graph: Graph;
  overrides: Record<string, UiStatus>;
  blast: string[];
  front: number | null;
  report: Report | null;
  diff: Diff | null;
  log: LogLine[];
  selected: string | null;
  path: Path | null;
  tab: Tab;
};
export type Msg =
  | { type: "run/start" }
  | { type: "run/action"; ev: RunEvent }
  | { type: "run/done" }
  | { type: "graph"; graph: Graph }
  | { type: "restore"; graph: Graph }
  | { type: "blast"; ids: string[] }
  | { type: "repair/start" }
  | { type: "repair/action"; ev: RepairEvent }
  | { type: "repair/report"; report: Report }
  | { type: "repair/diff"; diff: Diff }
  | { type: "fail"; message: string }
  | { type: "select"; id: string | null }
  | { type: "path"; path: Path | null }
  | { type: "tab"; tab: Tab }
  | { type: "reset" };

const empty: Graph = { nodes: [], edges: [] };

export const initial: State = {
  phase: "idle",
  before: "idle",
  graph: empty,
  overrides: {},
  blast: [],
  front: null,
  report: null,
  diff: null,
  log: [],
  selected: null,
  path: null,
  tab: "overview",
};

export const clock = { now: () => new Date().toTimeString().slice(0, 8) };

const LOG_MAX = 50;

function log(s: State, text: string, mark: Pick<LogLine, "gold" | "error"> = {}): LogLine[] {
  return [{ t: clock.now(), text, ...mark }, ...s.log].slice(0, LOG_MAX);
}

// Plain words for each status, as the log and chips say them.
export const WORDS: Record<UiStatus, string> = {
  ok: "same",
  poisoned: "poisoned",
  rerun_changed: "changed",
  rerun_same: "same, stopped",
  skipped: "skipped",
  superseded: "superseded",
  new: "new",
  pending: "pending",
  running: "running",
};

const seqOf = (n: GraphNode) => Number(n.props.seq ?? 0);

export function actions(s: State): GraphNode[] {
  return s.graph.nodes.filter((n) => n.label === "Action").sort((a, b) => seqOf(a) - seqOf(b));
}

export function statusOf(s: State, id: string): UiStatus | undefined {
  if (s.overrides[id]) return s.overrides[id];
  const n = s.graph.nodes.find((x) => x.id === id);
  return n?.label === "Action" ? ((n.props.status as Status | undefined) ?? "ok") : undefined;
}

// next marks as running the first pending action after the repair front,
// or the first pending one when none is after it.
function next(
  s: State,
  overrides: Record<string, UiStatus>,
  front: number | null,
): Record<string, UiStatus> {
  const out = { ...overrides };
  for (const id of Object.keys(out)) if (out[id] === "running") out[id] = "pending";
  const pending = actions({ ...s, overrides: out }).filter((n) => out[n.id] === "pending");
  const pick = pending.find((n) => front === null || Number(n.props.seq) > front) ?? pending[0];
  if (pick) out[pick.id] = "running";
  return out;
}

export function reduce(s: State, m: Msg): State {
  switch (m.type) {
    case "run/start":
      return {
        ...initial,
        phase: "running",
        before: s.phase,
        log: log(s, "run started: recording the poisoned scenario"),
      };
    case "run/action": {
      const { ev } = m;
      if (s.graph.nodes.some((n) => n.id === ev.id)) return s;
      const node: GraphNode = {
        id: ev.id,
        label: "Action",
        props: { id: ev.id, tool: ev.tool, ticket_id: ev.ticket_id, seq: ev.seq, status: "ok" },
      };
      return {
        ...s,
        graph: { ...s.graph, nodes: [...s.graph.nodes, node] },
        log: log(s, `${ev.tool} ${ev.ticket_id || "sources"} recorded`),
      };
    }
    case "run/done":
      return { ...s, phase: "ran", log: log(s, `run done: ${actions(s).length} actions recorded`) };
    case "graph": {
      // While running, keep actions the stream added that the fetch missed.
      const ids = new Set(m.graph.nodes.map((n) => n.id));
      const kept =
        s.phase === "running"
          ? s.graph.nodes.filter((n) => n.label === "Action" && !ids.has(n.id))
          : [];
      return {
        ...s,
        graph: { nodes: [...m.graph.nodes, ...kept], edges: m.graph.edges },
        overrides: s.phase === "repaired" ? {} : s.overrides,
      };
    }
    case "restore": {
      const acts = m.graph.nodes.filter((n) => n.label === "Action");
      if (acts.length === 0) return initial;
      const touched = acts.filter((n) => (n.props.status ?? "ok") !== "ok");
      if (touched.length === 0) return { ...initial, phase: "ran", before: "ran", graph: m.graph };
      return {
        ...initial,
        phase: "repaired",
        before: "repaired",
        graph: m.graph,
        blast: touched.filter((n) => n.props.status !== "new").map((n) => n.id),
      };
    }
    case "blast":
      return {
        ...s,
        phase: "flagged",
        before: s.phase,
        blast: m.ids,
        log: log(s, `flagged: ${m.ids.length} actions depend on the source`),
      };
    case "repair/start": {
      const overrides: Record<string, UiStatus> = {};
      for (const id of s.blast) overrides[id] = "pending";
      return {
        ...s,
        phase: "repairing",
        before: s.phase,
        overrides: next(s, overrides, null),
        log: log(s, "repair started"),
      };
    }
    case "repair/action": {
      const { ev } = m;
      let graph = s.graph;
      if (!graph.nodes.some((n) => n.id === ev.action_id)) {
        const node: GraphNode = {
          id: ev.action_id,
          label: "Action",
          props: {
            id: ev.action_id,
            tool: ev.tool,
            ticket_id: ev.ticket_id,
            seq: ev.seq,
            status: ev.status,
          },
        };
        graph = { ...graph, nodes: [...graph.nodes, node] };
      }
      // Superseded and new actions are side effects of a decision; only
      // decided actions move the front, which walks in seq order.
      const decided = ev.status !== "superseded" && ev.status !== "new";
      const front = decided ? ev.seq : s.front;
      return {
        ...s,
        graph,
        overrides: next({ ...s, graph }, { ...s.overrides, [ev.action_id]: ev.status }, front),
        front,
        log: log(s, `${ev.tool} ${ev.ticket_id} ${WORDS[ev.status]}`, {
          gold: ev.status === "rerun_changed" || ev.status === "new",
        }),
      };
    }
    case "repair/report":
      return {
        ...s,
        report: m.report,
        log: log(s, `report: ${m.report.compensation.length} effects can't be undone`),
      };
    case "repair/diff":
      return {
        ...s,
        phase: "repaired",
        before: "repaired",
        front: null,
        diff: m.diff,
        log: m.diff.match
          ? log(s, "diff against clean run: match", { gold: true })
          : log(s, `diff against clean run: ${m.diff.lines.length} lines differ`),
      };
    case "fail": {
      const overrides: Record<string, UiStatus> = {};
      for (const [id, st] of Object.entries(s.overrides)) {
        if (st !== "pending" && st !== "running") overrides[id] = st;
      }
      return {
        ...s,
        phase: busy(s) ? s.before : s.phase,
        overrides,
        front: null,
        log: log(s, `error: ${m.message}`, { error: true }),
      };
    }
    case "select":
      return { ...s, selected: m.id, path: null };
    case "path":
      return { ...s, path: m.path };
    case "tab":
      return { ...s, tab: m.tab };
    case "reset":
      return initial;
  }
}

export function busy(s: State): boolean {
  return s.phase === "running" || s.phase === "repairing";
}

const count = (s: State, kind: GraphNode["label"]) =>
  s.graph.nodes.filter((n) => n.label === kind).length;

export type Stat = { label: string; value: number; gold?: boolean };

export function stats(s: State): Stat[] {
  const of = (sts: UiStatus[]) =>
    actions(s).filter((n) => sts.includes(statusOf(s, n.id) ?? "ok")).length;
  switch (s.phase) {
    case "idle":
      return [];
    case "running":
    case "ran":
      return [
        { label: "Actions", value: count(s, "Action") },
        { label: "Facts", value: count(s, "Fact") },
        { label: "Effects", value: count(s, "Effect") },
      ];
    case "flagged":
      return [
        { label: "Blast radius", value: s.blast.length },
        { label: "Outside", value: count(s, "Action") - s.blast.length },
      ];
    case "repairing":
      return [
        { label: "Blast radius", value: s.blast.length },
        { label: "Re-run", value: of(["rerun_changed", "rerun_same", "new"]), gold: true },
        { label: "Skipped", value: of(["skipped"]) },
        { label: "Pending", value: of(["pending", "running"]) },
      ];
    case "repaired": {
      const c = s.report?.counts;
      const out: Stat[] = [
        { label: "Blast radius", value: s.blast.length },
        {
          label: "Re-run",
          value: c
            ? (c.rerun_changed ?? 0) + (c.rerun_same ?? 0) + (c.new ?? 0)
            : of(["rerun_changed", "rerun_same", "new"]),
          gold: true,
        },
        { label: "Skipped", value: c ? (c.skipped ?? 0) : of(["skipped"]) },
      ];
      if (s.report) out.push({ label: "Can't undo", value: s.report.compensation.length });
      return out;
    }
  }
}

export function step(s: State): 1 | 2 | 3 | 4 {
  switch (s.phase) {
    case "idle":
    case "running":
      return 1;
    case "ran":
      return 2;
    case "flagged":
    case "repairing":
      return 3;
    case "repaired":
      return 4;
  }
}

export function strip(s: State): { id: string; status: UiStatus }[] {
  if (s.blast.length === 0) return [];
  const blast = new Set(s.blast);
  return actions(s)
    .filter((n) => blast.has(n.id))
    .map((n) => ({ id: n.id, status: statusOf(s, n.id) ?? "ok" }));
}
