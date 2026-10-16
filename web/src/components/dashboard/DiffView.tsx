import { CheckIcon } from "@phosphor-icons/react";
import { CypherBlock } from "@/components/CypherBlock";
import type { State } from "@/dashboard/state";
import { DIFF_PARTS } from "./Report";

const QUERY: Record<string, string> = {
  facts: "DiffFacts",
  decisions: "DiffDecisions",
  effects: "DiffEffects",
};

// DiffView compares the repaired graph with a clean run of the same scenario,
// part by part, with the query behind each part.
export function DiffView({ s, cypher }: { s: State; cypher: Record<string, string> }) {
  const diff = s.diff;
  if (!diff) return <Missing />;
  return (
    <section className="min-h-0 flex-1 overflow-auto p-6">
      <h2 className="flex items-center gap-2 font-sans text-xl leading-[26px] font-medium">
        {diff.match && <CheckIcon size={20} className="text-gold" />}
        {diff.match
          ? "The repaired graph matches a clean run"
          : `${diff.lines.length} rows differ from a clean run`}
      </h2>
      <p className="mt-2 max-w-[68ch] font-sans text-sm leading-[22px] text-muted">
        The same scenario was run once against clean memory. These queries read facts, decisions and
        live effects from both graphs and compare them row by row.
      </p>
      <div className="mt-5 grid grid-cols-3 gap-2.5 max-[1000px]:grid-cols-1">
        {DIFF_PARTS.map((p) => {
          const lines = diff.lines.filter((l) => l.startsWith(p.key + " "));
          return (
            <div key={p.key} className="min-w-0 rounded-lg border border-rule bg-canvas p-4">
              <div className="flex items-baseline justify-between font-sans text-sm font-medium">
                {p.label}
                <span className="font-mono text-[13px] font-normal text-muted">
                  {lines.length} rows differ
                </span>
              </div>
              {lines.map((l) => (
                <div key={l} className="mt-1 font-mono text-xs break-all text-text">
                  {l}
                </div>
              ))}
              <CypherBlock title={QUERY[p.key]} query={cypher[QUERY[p.key]]} />
            </div>
          );
        })}
      </div>
    </section>
  );
}

export function Missing() {
  return (
    <section className="p-6 font-sans text-sm text-muted">
      The report is sent live during a repair. Reset and run again to see it.
    </section>
  );
}
