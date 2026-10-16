import { describe, expect, it } from "vite-plus/test";
import type { Graph, GraphNode } from "@/lib/types";
import { COL_W, LANE_H, SLOT, laneIndex, layout } from "./layout";

const act = (id: string, tool: string, ticket: string, seq: number, status = "ok"): GraphNode => ({
  id,
  label: "Action",
  props: { id, tool, ticket_id: ticket, seq, status },
});

const g: Graph = {
  nodes: [
    { id: "s1", label: "Source", props: { id: "s1" } },
    act("a1", "research", "", 1),
    { id: "f1", label: "Fact", props: { id: "f1", key: "k1", seq: 1 } },
    { id: "f2", label: "Fact", props: { id: "f2", key: "k2", seq: 1 } },
    act("a2", "triage", "t01", 2),
    act("a3", "refund", "t01", 3),
    { id: "e1", label: "Effect", props: { id: "e1", kind: "refund" } },
    { id: "e2", label: "Effect", props: { id: "e2", kind: "email" } },
    act("a4", "triage", "t02", 4),
    act("a6", "refund", "t02", 5, "superseded"),
    act("a7", "refund", "t02", 4.5, "new"),
    act("a8", "triage", "t03", 6),
    { id: "g1", label: "Agent", props: { id: "triage" } },
  ],
  edges: [
    { from: "a1", to: "s1", type: "INGESTED" },
    { from: "a1", to: "f1", type: "WROTE" },
    { from: "a1", to: "f2", type: "WROTE" },
    { from: "a2", to: "a3", type: "HANDED_OFF" },
    { from: "a3", to: "e1", type: "CAUSED" },
    { from: "a2", to: "e2", type: "CAUSED" },
    { from: "g1", to: "a2", type: "RAN" },
  ],
};

describe("layout", () => {
  const { pos, kept } = layout(g, null);
  const p = (id: string) => pos.get(id)!;

  it("puts research above the ticket columns, centred on them", () => {
    expect(p("a1").x).toBe((p("a2").x + p("a8").x) / 2);
  });
  it("puts a ticket's actions in one column, by lane", () => {
    expect(p("a2").x).toBe(p("a3").x);
    expect(p("a3").y).toBe(4 * LANE_H);
    expect(p("a2").y).toBe(3 * LANE_H);
    expect(p("s1").y).toBe(0);
    expect(p("a1").y).toBe(LANE_H);
    expect(p("f1").y).toBe(2 * LANE_H);
  });
  it("spaces tickets by COL_W in seq order", () => {
    expect(p("a4").x - p("a2").x).toBe(COL_W);
    expect(p("a8").x - p("a4").x).toBe(COL_W);
  });
  it("splits a superseded action and its new replacement", () => {
    expect(p("a6").x).toBe(p("a4").x - SLOT / 2);
    expect(p("a7").x).toBe(p("a4").x + SLOT / 2);
  });
  it("places sources, facts and effects by their action", () => {
    expect(p("s1").x).toBe(p("a1").x);
    expect(p("f2").x - p("f1").x).toBe(SLOT);
    expect(p("e1").x).toBe(p("a3").x);
    expect(p("e2").x).toBe(p("a2").x);
  });
  it("stacks effects that share a slot", () => {
    expect(p("e1").y).not.toBe(p("e2").y);
    expect((p("e1").y + p("e2").y) / 2).toBe(6 * LANE_H);
  });
  it("skips agents and has no kept column before a flag", () => {
    expect(pos.has("g1")).toBe(false);
    expect(kept).toBeNull();
  });
  it("folds tickets outside the focus into the kept column", () => {
    const f = layout(g, ["a1", "a2", "a3"]);
    expect(f.kept?.count).toBe(2);
    expect(f.pos.has("a4")).toBe(false);
    expect(f.pos.has("a8")).toBe(false);
    expect(f.pos.has("a2")).toBe(true);
    expect(f.kept!.x).toBeGreaterThan(f.pos.get("a2")!.x);
  });
});

describe("laneIndex", () => {
  it("keeps a deep effects stack in the effects lane", () => {
    expect(laneIndex(0)).toBe(0);
    expect(laneIndex(3 * LANE_H + 10)).toBe(3);
    expect(laneIndex(6 * LANE_H + 60)).toBe(6);
    expect(laneIndex(6 * LANE_H - 12)).toBe(6);
  });
});
