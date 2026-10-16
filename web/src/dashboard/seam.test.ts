import { describe, expect, it } from "vite-plus/test";
import type { Graph } from "@/lib/types";
import { initial } from "./state";
import { seam } from "./seam";

const graph: Graph = {
  nodes: [],
  edges: [
    { from: "r1", to: "src", type: "INGESTED" },
    { from: "r1", to: "f1", type: "WROTE" },
    { from: "t1", to: "f1", type: "READ" },
    { from: "t1", to: "n1", type: "HANDED_OFF" },
  ],
};

describe("seam", () => {
  it("draws a FEEDS hop as the WROTE and READ pair through the fact", () => {
    const s = {
      ...initial,
      graph,
      selected: "t1",
      path: {
        nodes: ["src", "r1", "t1"],
        edges: [
          { from: "r1", to: "src", type: "INGESTED" },
          { from: "r1", to: "t1", type: "FEEDS" },
        ],
      },
    };
    const { edges, nodes } = seam(s);
    expect([...edges].sort()).toEqual([
      "r1|INGESTED|src",
      "r1|WROTE|f1",
      "t1|HANDED_OFF|n1",
      "t1|READ|f1",
    ]);
    expect(nodes.has("f1")).toBe(true);
    expect(nodes.has("n1")).toBe(true);
  });
});
