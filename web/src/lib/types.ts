export type Status =
  | "ok"
  | "poisoned"
  | "rerun_changed"
  | "rerun_same"
  | "skipped"
  | "superseded"
  | "new";
export type Kind = "Agent" | "Source" | "Action" | "Fact" | "Effect";
export type GraphNode = { id: string; label: Kind; props: Record<string, unknown> };
export type GraphEdge = { from: string; to: string; type: string };
export type Graph = { nodes: GraphNode[]; edges: GraphEdge[] };
export type RunEvent = { id: string; tool: string; ticket_id: string; seq: number };
export type RepairEvent = {
  action_id: string;
  tool: string;
  ticket_id: string;
  status: Status;
  seq: number;
  // Set for actions the MustRerun query decided: its answer and FalkorDB's own
  // execution time for it.
  must_rerun?: boolean;
  query_ms?: number;
  // When the decision began, from the start of the repair, and how long it took.
  start_ms?: number;
  dur_ms?: number;
};
export type CompItem = {
  effect_id: string;
  kind: string;
  ticket_id: string;
  action_status: string;
  amount: number;
  draft: string;
  draft_error?: string;
};
export type Report = {
  repair_id: string;
  counts: Record<string, number>;
  compensation: CompItem[];
};
export type Diff = { match: boolean; lines: string[] };
export type Path = { nodes: string[]; edges: GraphEdge[] };
export type ActionDetail = {
  action: Record<string, unknown>;
  reads: Record<string, unknown>[];
  writes: Record<string, unknown>[];
  handoffs: Record<string, unknown>[];
  effects: Record<string, unknown>[];
  reran: { old_output: string; old_hash: string; new_hash: string } | null;
};

export const SOURCE = "partner-blog/policy-update";
