import type { State } from "@/dashboard/state";
import { Missing } from "./DiffView";
import { UndoCard } from "./Parts";

// DraftsView lists every effect that already left the system, with the
// follow-up the repair drafted for it. Nothing here is sent.
export function DraftsView({ s }: { s: State }) {
  const report = s.report;
  if (!report) return <Missing />;
  return (
    <section className="min-h-0 flex-1 overflow-auto p-6">
      <h2 className="font-sans text-xl leading-[26px] font-medium">
        {report.compensation.length} effects can't be undone
      </h2>
      <p className="mt-2 max-w-[68ch] font-sans text-sm leading-[22px] text-muted">
        These were sent before the source was flagged. The repair drafted a follow-up for each one.
        None of them were sent: a person reviews and sends them.
      </p>
      <div className="mt-5 grid grid-cols-[repeat(auto-fill,minmax(360px,1fr))] gap-2.5">
        {report.compensation.map((c) => (
          <UndoCard
            key={c.effect_id}
            item={c}
            sentence={
              c.kind === "refund"
                ? `Refund of $${c.amount} for ${c.ticket_id}`
                : `Email to ${c.ticket_id}`
            }
          />
        ))}
      </div>
    </section>
  );
}
