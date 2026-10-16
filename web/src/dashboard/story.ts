// Plain sentences for the inspector, from what the graph recorded.

import type { ActionDetail } from "@/lib/types";

type Out = { choice?: string; level?: string; amount?: number; template?: string };

// outcome turns an action's JSON output into a short phrase.
export function outcome(tool: string, output: unknown): string {
  let o: Out;
  try {
    o = JSON.parse(String(output)) as Out;
  } catch {
    return String(output);
  }
  if (tool === "triage") {
    if (o.choice === "refund") return o.level === "partial" ? "Partial refund" : "Full refund";
    if (o.choice === "reply") return "Reply";
    if (o.choice === "escalate") return "Escalate";
    return o.choice ?? String(output);
  }
  if (tool === "refund") return `Refund $${o.amount ?? 0}`;
  if (tool === "notifier") {
    if (o.template === "reply") return "Reply email";
    if (o.template === "escalate") return "Escalation email";
    return `Refund email, $${o.amount ?? 0}`;
  }
  return String(output);
}

const tool = (d: ActionDetail) => String(d.action.tool);

export function title(d: ActionDetail): string {
  switch (d.action.status) {
    case "rerun_changed":
      if (d.reran) {
        return `${outcome(tool(d), d.reran.old_output)} became ${outcome(tool(d), d.action.output).toLowerCase()}`;
      }
      return "Changed on re-run";
    case "rerun_same":
      return "Same decision, cascade stopped";
    case "skipped":
      return "Skipped, nothing upstream changed";
    case "superseded":
      return "Replaced by the repair";
    case "new":
      return "Created by the repair";
    case "poisoned":
      return "Read the flagged page";
    default:
      return "Untouched by the repair";
  }
}

export function why(d: ActionDetail): string {
  const retired = d.reads.find((f) => f.active === false);
  const cause = retired
    ? `It read ${String(retired.key)}, which came from the poisoned page.`
    : "An action that handed it work changed.";
  switch (d.action.status) {
    case "rerun_changed":
      return `${cause} Its result changed, so the cascade continued to its handoffs.`;
    case "rerun_same":
      return `${cause} It ran again and decided the same, so the cascade stopped here.`;
    case "skipped":
      return "Nothing it depends on changed, so it was not run again.";
    case "superseded":
      return "The action that handed it work decided differently, so this one no longer applies.";
    case "new":
      return "A re-run handed off differently, so the repair created this action.";
    case "poisoned":
      return "It ingested the flagged page, so everything it wrote is suspect.";
    default:
      return "It never depended on the flagged page.";
  }
}

export function effectSentence(e: Record<string, unknown>): string {
  return e.kind === "refund" ? `Refund of $${String(e.amount)} was paid out` : "Email was sent";
}
