import { describe, expect, it } from "vite-plus/test";
import type { Graph, GraphNode, Report } from "@/lib/types";
import {
  actions,
  busy,
  initial,
  reduce,
  stats,
  statusOf,
  step,
  strip,
  type Msg,
  type State,
} from "./state";

const act = (
  id: string,
  seq: number,
  status = "ok",
  extra: Record<string, unknown> = {},
): GraphNode => ({
  id,
  label: "Action",
  props: { id, seq, status, tool: "triage", ticket_id: "t01", ...extra },
});

const graph: Graph = {
  nodes: [
    { id: "s1", label: "Source", props: { id: "s1" } },
    act("a1", 1),
    act("a2", 2),
    act("a3", 3),
    act("a4", 4),
    { id: "f1", label: "Fact", props: { id: "f1", key: "vip_rules" } },
    { id: "e1", label: "Effect", props: { id: "e1" } },
  ],
  edges: [],
};

const run = (s: State, ...ms: Msg[]) => ms.reduce(reduce, s);
const flagged = () =>
  run(initial, { type: "restore", graph }, { type: "blast", ids: ["a3", "a1", "a2"] });

const report: Report = {
  repair_id: "r1",
  counts: { rerun_changed: 2, rerun_same: 1, new: 1, skipped: 3, poisoned: 1 },
  compensation: [
    {
      effect_id: "e1",
      kind: "refund",
      ticket_id: "t05",
      action_status: "superseded",
      amount: 40,
      draft: "d",
    },
  ],
};

describe("reduce", () => {
  it("run/start from idle", () => {
    const s = reduce({ ...initial, graph }, { type: "run/start" });
    expect(s.phase).toBe("running");
    expect(s.before).toBe("idle");
    expect(s.graph.nodes).toEqual([]);
    expect(s.log).toHaveLength(1);
  });

  it("run/action adds an ok Action once", () => {
    const ev = { id: "a9", tool: "refund", ticket_id: "t02", seq: 9 };
    const s = run(
      initial,
      { type: "run/start" },
      { type: "run/action", ev },
      { type: "run/action", ev },
    );
    const a = s.graph.nodes.filter((n) => n.id === "a9");
    expect(a).toHaveLength(1);
    expect(a[0].label).toBe("Action");
    expect(a[0].props).toMatchObject({ tool: "refund", ticket_id: "t02", seq: 9, status: "ok" });
  });

  it("run/done", () => {
    expect(run(initial, { type: "run/start" }, { type: "run/done" }).phase).toBe("ran");
  });

  it("graph keeps overrides, except once repaired", () => {
    const s = run(flagged(), { type: "repair/start" });
    expect(reduce(s, { type: "graph", graph }).overrides).toEqual(s.overrides);
    const done = { ...s, phase: "repaired" as const };
    expect(reduce(done, { type: "graph", graph }).overrides).toEqual({});
  });

  it("blast flags", () => {
    const s = flagged();
    expect(s.phase).toBe("flagged");
    expect(s.blast).toEqual(["a3", "a1", "a2"]);
  });

  it("repair/start marks pending and runs the lowest seq", () => {
    const s = run(flagged(), { type: "repair/start" });
    expect(s.phase).toBe("repairing");
    expect(s.before).toBe("flagged");
    expect(s.overrides).toEqual({ a1: "running", a2: "pending", a3: "pending" });
  });

  it("repair/action sets status, front, and moves running", () => {
    const ev = {
      action_id: "a1",
      tool: "triage",
      ticket_id: "t01",
      status: "rerun_changed" as const,
      seq: 1,
    };
    const s = run(flagged(), { type: "repair/start" }, { type: "repair/action", ev });
    expect(s.overrides).toEqual({ a1: "rerun_changed", a2: "running", a3: "pending" });
    expect(s.front).toBe(1);
  });

  it("superseded and new events do not move the front", () => {
    const g2: Graph = { nodes: [...graph.nodes, act("a9", 9)], edges: [] };
    let s = run(
      initial,
      { type: "restore", graph: g2 },
      { type: "blast", ids: ["a1", "a2", "a9"] },
    );
    s = run(
      s,
      { type: "repair/start" },
      {
        type: "repair/action",
        ev: { action_id: "a1", tool: "triage", ticket_id: "t01", status: "rerun_changed", seq: 1 },
      },
      {
        type: "repair/action",
        ev: { action_id: "a9", tool: "refund", ticket_id: "t01", status: "superseded", seq: 9 },
      },
    );
    expect(s.front).toBe(1);
    expect(s.overrides.a2).toBe("running");
  });

  it("repair/action adds an unknown (new) action", () => {
    const ev = {
      action_id: "n1",
      tool: "notifier",
      ticket_id: "t05",
      status: "new" as const,
      seq: 3.5,
    };
    const s = run(flagged(), { type: "repair/start" }, { type: "repair/action", ev });
    const n = s.graph.nodes.find((x) => x.id === "n1");
    expect(n?.label).toBe("Action");
    expect(statusOf(s, "n1")).toBe("new");
  });

  it("repair/report then repair/diff", () => {
    const s = run(
      flagged(),
      { type: "repair/start" },
      { type: "repair/report", report },
      { type: "repair/diff", diff: { match: true, lines: [] } },
    );
    expect(s.phase).toBe("repaired");
    expect(s.front).toBeNull();
    expect(s.report).toBe(report);
    expect(s.log[0]).toMatchObject({ text: "diff against clean run: match", gold: true });
  });

  it("fail during repairing goes back", () => {
    const ev = {
      action_id: "a1",
      tool: "triage",
      ticket_id: "t01",
      status: "rerun_same" as const,
      seq: 1,
    };
    const s = run(
      flagged(),
      { type: "repair/start" },
      { type: "repair/action", ev },
      { type: "fail", message: "boom" },
    );
    expect(s.phase).toBe("flagged");
    expect(s.log[0]).toMatchObject({ error: true });
    expect(s.log[0].text).toContain("boom");
    expect(s.overrides).toEqual({ a1: "rerun_same" });
    expect(busy(s)).toBe(false);
  });

  it("restore picks the phase from statuses", () => {
    const empty = { nodes: [graph.nodes[0]], edges: [] };
    expect(reduce(initial, { type: "restore", graph: empty })).toEqual(initial);
    expect(reduce(initial, { type: "restore", graph }).phase).toBe("ran");
    const fixed = { ...graph, nodes: [...graph.nodes, act("a5", 5, "rerun_changed")] };
    const s = reduce(initial, { type: "restore", graph: fixed });
    expect(s.phase).toBe("repaired");
    expect(s.report).toBeNull();
  });

  it("ignores a late restore once a run has started", () => {
    const s = reduce(initial, { type: "run/start" });
    expect(reduce(s, { type: "restore", graph })).toBe(s);
  });

  it("ignores a late graph fetch after a reset", () => {
    expect(reduce(initial, { type: "graph", graph })).toBe(initial);
  });

  it("caps the log at 200, newest first", () => {
    let s = initial;
    for (let i = 0; i < 210; i++) s = reduce(s, { type: "fail", message: `m${i}` });
    expect(s.log).toHaveLength(200);
    expect(s.log[0].text).toContain("m209");
  });

  it("keeps the repair event on its log line", () => {
    const ev = {
      action_id: "a1",
      tool: "triage",
      ticket_id: "t01",
      status: "rerun_changed" as const,
      seq: 1,
      must_rerun: true,
      query_ms: 0.4,
      start_ms: 3,
      dur_ms: 9,
    };
    const s = run(flagged(), { type: "repair/start" }, { type: "repair/action", ev });
    expect(s.log[0].ev).toEqual(ev);
  });

  it("reset", () => {
    expect(reduce(flagged(), { type: "reset" })).toEqual(initial);
  });

  it("select clears the old path", () => {
    let s = reduce(flagged(), { type: "path", path: { nodes: ["a1"], edges: [] } });
    s = reduce(s, { type: "select", id: "a2" });
    expect(s.selected).toBe("a2");
    expect(s.path).toBeNull();
  });
});

describe("derived", () => {
  it("actions sorts by seq", () => {
    const g = { nodes: [act("b", 2), act("a", 1), graph.nodes[0]], edges: [] };
    expect(actions({ ...initial, graph: g }).map((n) => n.id)).toEqual(["a", "b"]);
  });

  it("statusOf prefers overrides", () => {
    const s = { ...initial, graph, overrides: { a1: "pending" as const } };
    expect(statusOf(s, "a1")).toBe("pending");
    expect(statusOf(s, "a2")).toBe("ok");
    expect(statusOf(s, "zz")).toBeUndefined();
  });

  it("stats per phase", () => {
    expect(stats(initial)).toEqual([]);
    const ran = run(initial, { type: "restore", graph });
    expect(stats(ran)).toEqual([
      { label: "Actions", value: 4 },
      { label: "Facts", value: 1 },
      { label: "Effects", value: 1 },
    ]);
    expect(stats(flagged())).toEqual([
      { label: "Blast radius", value: 3 },
      { label: "Outside", value: 1 },
    ]);
    const ev = {
      action_id: "a1",
      tool: "triage",
      ticket_id: "t01",
      status: "rerun_changed" as const,
      seq: 1,
    };
    const rep = run(flagged(), { type: "repair/start" }, { type: "repair/action", ev });
    expect(stats(rep)).toEqual([
      { label: "Blast radius", value: 3 },
      { label: "Re-run", value: 1, gold: true },
      { label: "Skipped", value: 0 },
      { label: "Pending", value: 2 },
    ]);
    const done = run(
      rep,
      { type: "repair/report", report },
      { type: "repair/diff", diff: { match: true, lines: [] } },
    );
    expect(stats(done)).toEqual([
      { label: "Blast radius", value: 3 },
      { label: "Re-run", value: 4, gold: true },
      { label: "Skipped", value: 3 },
      { label: "Can't undo", value: 1 },
    ]);
  });

  it("step", () => {
    expect(step(initial)).toBe(1);
    expect(step({ ...initial, phase: "running" })).toBe(1);
    expect(step({ ...initial, phase: "ran" })).toBe(2);
    expect(step({ ...initial, phase: "flagged" })).toBe(3);
    expect(step({ ...initial, phase: "repairing" })).toBe(3);
    expect(step({ ...initial, phase: "repaired" })).toBe(4);
  });

  it("strip is the blast in seq order, empty before a flag", () => {
    expect(strip({ ...initial, graph })).toEqual([]);
    expect(strip(flagged())).toEqual([
      { id: "a1", status: "ok" },
      { id: "a2", status: "ok" },
      { id: "a3", status: "ok" },
    ]);
  });
});
