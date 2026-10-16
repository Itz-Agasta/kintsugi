// The gold seam: the causal path from the flagged page to the selected
// action, as the canvas draws it.

import type { GraphEdge } from "@/lib/types";
import { statusOf, type State } from "./state";

// seam returns the edge ids on the gold causal path. The path API walks FEEDS
// edges, which the canvas draws as the WROTE and READ pair through a fact.
export function seam(s: State): { edges: Set<string>; nodes: Set<string> } {
  const edges = new Set<string>();
  const nodes = new Set<string>(s.path?.nodes ?? []);
  const id = (e: GraphEdge) => `${e.from}|${e.type}|${e.to}`;
  for (const e of s.path?.edges ?? []) {
    if (e.type !== "FEEDS") {
      edges.add(id(e));
      continue;
    }
    for (const w of s.graph.edges) {
      if (w.type !== "WROTE" || w.from !== e.from) continue;
      const r = s.graph.edges.find((x) => x.type === "READ" && x.from === e.to && x.to === w.to);
      if (r) {
        edges.add(id(w));
        edges.add(id(r));
        nodes.add(w.to);
      }
    }
  }
  // On from the selected action to what it handed off to, now.
  if (s.path && s.selected) {
    for (const e of s.graph.edges) {
      if (e.type === "HANDED_OFF" && e.from === s.selected && statusOf(s, e.to) !== "superseded") {
        edges.add(id(e));
        nodes.add(e.to);
      }
    }
  }
  return { edges, nodes };
}
