// The ticket view of the graph: actions grouped by ticket, for the list on the left.

import { SOURCE, type GraphNode } from "@/lib/types";
import { actions, statusOf, type State, type UiStatus } from "./state";

export type TicketGroup = {
  id: string;
  actions: GraphNode[];
  outcome: UiStatus | undefined;
  decision: string;
};

// The ticket chip: the most telling status among its actions.
const ORDER: UiStatus[] = [
  "running",
  "rerun_changed",
  "poisoned",
  "pending",
  "rerun_same",
  "skipped",
  "superseded",
];

function decision(triage: GraphNode | undefined): string {
  try {
    const d = JSON.parse(String(triage?.props.output)) as { choice?: string; level?: string };
    if (d.choice === "refund") return d.level === "partial" ? "Partial refund" : "Full refund";
    if (d.choice === "reply") return "Reply";
    return d.choice ?? "";
  } catch {
    return "";
  }
}

// groups lists research (id "") then tickets in seq order. Once a source is
// flagged, only groups with an action in the blast radius are listed.
export function groups(s: State): { shown: TicketGroup[]; total: number; kept: number } {
  const by = new Map<string, GraphNode[]>();
  for (const a of actions(s)) {
    const t = (a.props.ticket_id as string) || "";
    by.set(t, [...(by.get(t) ?? []), a]);
  }
  const blast = s.blast.length ? new Set(s.blast) : null;
  const all: TicketGroup[] = [];
  for (const [id, everything] of by) {
    // Once flagged, sources list only the research that read the flagged page.
    const acts = blast && id === "" ? everything.filter((a) => blast.has(a.id)) : everything;
    const sts: UiStatus[] = acts
      .map((a) => statusOf(s, a.id) ?? "ok")
      .map((x) => (x === "new" ? "rerun_changed" : x));
    const triage = acts
      .filter((a) => a.props.tool === "triage" && statusOf(s, a.id) !== "superseded")
      .at(-1);
    all.push({
      id,
      actions: acts,
      outcome: ORDER.find((o) => sts.includes(o)),
      decision: decision(triage),
    });
  }
  const tickets = all.filter((g) => g.id !== "");
  const shown = blast ? all.filter((g) => g.actions.some((a) => blast.has(a.id))) : all;
  return {
    shown,
    total: tickets.length,
    kept: blast ? tickets.length - shown.filter((g) => g.id).length : 0,
  };
}

// poisonedKey is the fact key the flagged source wrote, as the graph records it.
export function poisonedKey(s: State): string | undefined {
  const reader = s.graph.edges.find((e) => e.type === "INGESTED" && e.to === SOURCE)?.from;
  const fact = s.graph.edges.find((e) => e.type === "WROTE" && e.from === reader)?.to;
  return s.graph.nodes.find((n) => n.id === fact)?.props.key as string | undefined;
}
