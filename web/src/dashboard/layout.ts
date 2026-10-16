// Graph positions: lanes top to bottom by kind and agent, ticket columns left
// to right in seq order. Pure, so it can be tested without a canvas.

import type { Graph, GraphNode } from "@/lib/types";

export const LANES = [
  "sources",
  "research",
  "facts",
  "triage",
  "refund",
  "notifier",
  "effects",
] as const;
export const LANE_H = 104;
export const COL_W = 110;
export const SLOT = 70;
// Research sits above the tickets, spread across their columns, at least this
// far apart: their sources carry full ids.
export const RESEARCH_W = 200;
// Effects of one ticket stack in its column, this far apart (not stretched).
export const EFFECT_GAP = 24;

function laneOf(n: GraphNode): number | null {
  switch (n.label) {
    case "Source":
      return 0;
    case "Fact":
      return 2;
    case "Effect":
      return 6;
    case "Action": {
      const i = LANES.indexOf(n.props.tool as (typeof LANES)[number]);
      return i < 0 ? null : i;
    }
    default:
      return null;
  }
}

type Pos = { x: number; y: number };
const seq = (n: GraphNode) => Number(n.props.seq ?? 0);

// focus: the blast radius once a source is flagged, null before. Tickets with
// no focus action fold into one kept column on the right and get no position.
export function layout(
  g: Graph,
  focus: string[] | null,
): { pos: Map<string, Pos>; kept: { x: number; count: number; tickets: string[] } | null } {
  const pos = new Map<string, Pos>();
  const acts = g.nodes.filter((n) => n.label === "Action").sort((a, b) => seq(a) - seq(b));
  const research = acts.filter((n) => !n.props.ticket_id);

  // Tickets in order of their first action.
  const tickets: string[] = [];
  for (const a of acts) {
    const t = a.props.ticket_id as string;
    if (t && !tickets.includes(t)) tickets.push(t);
  }
  const inFocus = focus ? new Set(focus) : null;
  const shown = inFocus
    ? tickets.filter((t) => acts.some((a) => a.props.ticket_id === t && inFocus.has(a.id)))
    : tickets;

  const colX = new Map<string, number>();
  shown.forEach((t, i) => colX.set(t, i * COL_W));
  const gap = Math.max(RESEARCH_W, ((shown.length - 1) * COL_W) / Math.max(research.length - 1, 1));
  const r0 = ((shown.length - 1) * COL_W - (research.length - 1) * gap) / 2;
  research.forEach((a, i) => pos.set(a.id, { x: r0 + i * gap, y: LANE_H }));

  // Ticket actions, splitting a lane slot shared by an original and its replacement.
  const slots = new Map<string, GraphNode[]>();
  for (const a of acts) {
    const t = a.props.ticket_id as string;
    if (!t || !colX.has(t)) continue;
    const key = `${t}/${String(a.props.tool)}`;
    slots.set(key, [...(slots.get(key) ?? []), a]);
  }
  for (const group of slots.values()) {
    const x = colX.get(group[0].props.ticket_id as string)!;
    const ordered = [...group].sort(
      (a, b) => Number(a.props.status === "new") - Number(b.props.status === "new"),
    );
    ordered.forEach((a, i) => {
      const dx = ordered.length === 1 ? 0 : (i - (ordered.length - 1) / 2) * SLOT;
      pos.set(a.id, { x: x + dx, y: laneOf(a)! * LANE_H });
    });
  }

  // Sources, facts and effects hang off the action linked to them.
  const facts = new Map<string, number>();
  const effects = new Map<number, { id: string; seq: number }[]>();
  const seqById = new Map(acts.map((a) => [a.id, seq(a)]));
  for (const e of g.edges) {
    const from = pos.get(e.from);
    if (!from) continue;
    if (e.type === "INGESTED") pos.set(e.to, { x: from.x, y: 0 });
    if (e.type === "WROTE") {
      const i = facts.get(e.from) ?? 0;
      facts.set(e.from, i + 1);
      pos.set(e.to, { x: from.x + i * SLOT, y: 2 * LANE_H });
    }
    if (e.type === "CAUSED") {
      const x = colX.get(acts.find((a) => a.id === e.from)?.props.ticket_id as string) ?? from.x;
      effects.set(x, [...(effects.get(x) ?? []), { id: e.to, seq: seqById.get(e.from) ?? 0 }]);
    }
  }
  for (const [x, list] of effects) {
    list.sort((a, b) => a.seq - b.seq);
    list.forEach(({ id }, i) =>
      pos.set(id, { x, y: 6 * LANE_H + (i - (list.length - 1) / 2) * EFFECT_GAP }),
    );
  }

  const folded = tickets.filter((t) => !shown.includes(t));
  const kept = inFocus ? { x: shown.length * COL_W, count: folded.length, tickets: folded } : null;
  return { pos, kept };
}
