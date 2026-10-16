import { CheckIcon, ReceiptIcon, PaperPlaneTiltIcon } from "@phosphor-icons/react";
import { cn } from "cn";
import type { State } from "@/dashboard/state";
import { Section, UndoCard } from "./Parts";

export const DIFF_PARTS = [
  { key: "facts", label: "Facts" },
  { key: "decisions", label: "Decisions" },
  { key: "effects", label: "Live effects" },
];

// Report is the inspector after a repair with nothing selected: how the
// repaired graph compares with the clean run, and what cannot be undone.
export function Report({ s }: { s: State }) {
  if (!s.report || !s.diff) {
    return (
      <Section last>
        <h2 className="mb-2 font-sans text-xl leading-[26px] font-medium">Repaired</h2>
        <p className="font-sans text-sm leading-[22px] text-muted">
          The report is sent live during a repair. Reset and run again to see it.
        </p>
      </Section>
    );
  }
  const { diff, report } = s;
  const [first, ...rest] = report.compensation;
  return (
    <>
      <Section title="Diff against the clean run">
        <div
          className={cn(
            "mb-2 flex items-center gap-2 font-sans text-[15px]",
            diff.match && "text-gold",
          )}
        >
          {diff.match && <CheckIcon size={16} />}
          {diff.match ? "Match" : `${diff.lines.length} rows differ`}
        </div>
        {DIFF_PARTS.map((p) => (
          <div key={p.key} className="flex justify-between py-1 font-sans text-[13px] text-muted">
            {p.label}
            <span className="font-mono">
              {diff.lines.filter((l) => l.startsWith(p.key + " ")).length} rows differ
            </span>
          </div>
        ))}
      </Section>
      <Section title={`${report.compensation.length} effects can't be undone`} last>
        {first && (
          <UndoCard
            item={first}
            sentence={
              first.kind === "refund"
                ? `Refund of $${first.amount} for ${first.ticket_id}`
                : `Email to ${first.ticket_id}`
            }
          />
        )}
        <div className="mt-2.5">
          {rest.map((c) => {
            const I = c.kind === "refund" ? ReceiptIcon : PaperPlaneTiltIcon;
            return (
              <div
                key={c.effect_id}
                className="grid grid-cols-[20px_1fr_auto] items-center gap-2 py-[5px] font-sans text-[13px]"
              >
                <I size={15} className="text-faint" />
                <span>
                  {c.kind === "refund" ? `Refund $${c.amount}` : "Email"}{" "}
                  <span className="font-mono text-muted">{c.ticket_id}</span>
                </span>
                <span className="font-sans text-xs text-gold">Draft, not sent</span>
              </div>
            );
          })}
        </div>
      </Section>
    </>
  );
}
