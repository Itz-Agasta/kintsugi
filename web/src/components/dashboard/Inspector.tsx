import { useEffect, useState } from "react";
import { ArrowRightIcon, CaretRightIcon, CodeIcon, DatabaseIcon } from "@phosphor-icons/react";
import { cn } from "cn";
import { CypherBlock } from "@/components/CypherBlock";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ICONS } from "@/dashboard/marks";
import type { State, UiStatus } from "@/dashboard/state";
import { effectSentence, outcome, title, why } from "@/dashboard/story";
import { getJSON } from "@/lib/api";
import type { ActionDetail } from "@/lib/types";
import { Chip } from "./Chip";
import { Report } from "./Report";
import { Prose, Section, UndoCard } from "./Parts";

const AGENT_NAME: Record<string, string> = {
  research: "Research agent",
  triage: "Triage agent",
  refund: "Refund agent",
  notifier: "Notifier agent",
};

function Detail({ s, d, cypher }: { s: State; d: ActionDetail; cypher: Record<string, string> }) {
  const a = d.action;
  const tool = String(a.tool);
  const status = (a.status as UiStatus) ?? "ok";
  const I = ICONS[tool] ?? ICONS.triage;
  const undone = d.effects.filter((e) => e.sent && !e.live);
  const retiredKeys = new Set(d.reads.filter((f) => f.active === false).map((f) => String(f.key)));
  const query =
    status !== "ok" ? cypher.MustRerun : s.path ? cypher.ExplainPath : cypher.BlastRadius;
  const queryTitle =
    status !== "ok"
      ? "Cypher that decided it"
      : s.path
        ? "Cypher for the causal path"
        : "Cypher for the blast radius";
  return (
    <>
      <div className="border-b border-rule px-4 pt-4 pb-3.5">
        <div className="flex items-center gap-2 font-sans text-[13px] text-muted">
          <I size={15} />
          {AGENT_NAME[tool] ?? tool}
          {a.ticket_id ? (
            <span className="text-faint">decided ticket {a.ticket_id as string}</span>
          ) : null}
        </div>
        <h2 className="mt-2 mb-2.5 font-sans text-xl leading-[26px] font-medium">{title(d)}</h2>
        {status !== "ok" && <Chip status={status} />}
      </div>

      <Section>
        {d.reran && status === "rerun_changed" ? (
          <>
            <div className="grid grid-cols-[1fr_20px_1fr] items-center gap-2">
              <div className="rounded-md border border-rule bg-canvas p-2.5">
                <div className="font-sans text-xs text-faint">Poisoned run</div>
                <div className="mt-0.5 font-sans text-[15px] text-muted line-through decoration-faint">
                  {outcome(tool, d.reran.old_output)}
                </div>
              </div>
              <ArrowRightIcon size={16} className="text-faint" />
              <div className="rounded-md border border-gold-line bg-gold-tint p-2.5">
                <div className="font-sans text-xs text-faint">After repair</div>
                <div className="mt-0.5 font-sans text-[15px] text-gold">
                  {outcome(tool, a.output)}
                </div>
              </div>
            </div>
            <div className="mt-2 font-mono text-xs text-faint">
              output {d.reran.old_hash.slice(0, 6)} became {d.reran.new_hash.slice(0, 6)}
            </div>
          </>
        ) : (
          <>
            <div className="rounded-md border border-rule bg-canvas p-2.5">
              <div className="font-sans text-xs text-faint">
                {status === "new" ? "Created by the repair" : "Output"}
              </div>
              <div
                className={cn(
                  "mt-0.5 font-sans text-[15px]",
                  status === "new" ? "text-gold" : "text-text",
                )}
              >
                {outcome(tool, a.output)}
              </div>
            </div>
            <div className="mt-2 font-mono text-xs text-faint">
              output {((a.output_hash as string | undefined) ?? "").slice(0, 6)}
            </div>
          </>
        )}
      </Section>

      {d.reads.length > 0 && (
        <Section title="What it read">
          {d.reads.map((f) => {
            const retired = f.active === false;
            const restored = !retired && retiredKeys.has(String(f.key));
            return (
              <div
                key={String(f.id)}
                className="grid grid-cols-[20px_1fr_auto] items-center gap-2 py-[5px]"
              >
                <DatabaseIcon size={15} className="text-faint" />
                <span className={cn("font-mono text-[13px]", retired && "text-faint line-through")}>
                  {String(f.key)} v{String(f.version)}
                </span>
                {retired ? (
                  <Chip status="superseded" label="Retired" />
                ) : restored ? (
                  <Chip status="rerun_changed" label="Restored" className="[&_svg]:hidden" />
                ) : (
                  <Chip status="ok" />
                )}
              </div>
            );
          })}
        </Section>
      )}

      {d.handoffs.length > 0 && (
        <Section title="Handed off to">
          {d.handoffs.map((h) => {
            const HI = ICONS[String(h.tool)] ?? ICONS.triage;
            const st = (h.status as UiStatus) ?? "ok";
            return (
              <div
                key={String(h.id)}
                className="grid grid-cols-[20px_1fr_auto] items-center gap-2 py-[5px]"
              >
                <HI size={15} className="text-faint" />
                <span className="font-sans text-[13px]">
                  {String(h.tool)}{" "}
                  <span className="font-mono text-muted">{String(h.ticket_id)}</span>
                </span>
                <Chip status={st} />
              </div>
            );
          })}
        </Section>
      )}

      {undone.length > 0 && (
        <Section title="Can't be undone">
          <div className="flex flex-col gap-2.5">
            {undone.map((e) => (
              <UndoCard
                key={String(e.id)}
                item={s.report?.compensation.find((c) => c.effect_id === e.id)}
                sentence={effectSentence(e)}
              />
            ))}
          </div>
        </Section>
      )}

      <Section title={status === "ok" ? "Why it was kept" : "Why it re-ran"} last>
        <Prose>{why(d)}</Prose>
        <Collapsible defaultOpen className="mt-2.5">
          <CollapsibleTrigger className="group flex items-center gap-1.5 font-sans text-[13px] text-muted hover:text-text">
            <CaretRightIcon
              size={12}
              className="transition-transform duration-150 group-data-panel-open:rotate-90"
            />
            <CodeIcon size={14} />
            {queryTitle}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-2 overflow-x-auto rounded-md border border-rule bg-canvas p-2.5 font-mono text-xs leading-[18px] whitespace-pre-wrap text-text">
              {query?.trim()}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      </Section>
    </>
  );
}

function Empty({ s, cypher }: { s: State; cypher: Record<string, string> }) {
  if (s.phase === "repaired") return <Report s={s} />;
  if (s.phase === "flagged" || s.phase === "repairing") {
    return (
      <Section last>
        <h2 className="mb-2 font-sans text-xl leading-[26px] font-medium">
          {s.blast.length} actions in the blast radius
        </h2>
        <Prose>
          {s.phase === "flagged"
            ? "Everything that read the flagged page, or was handed work by something that did. Repair re-runs them against clean memory, in order, and stops where a re-run decides the same."
            : "Each action re-runs only if something it depends on changed. Select one to see why."}
        </Prose>
        <CypherBlock
          title={s.phase === "flagged" ? "Blast radius" : "Must re-run"}
          query={s.phase === "flagged" ? cypher.BlastRadius : cypher.MustRerun}
        />
      </Section>
    );
  }
  return (
    <Section last>
      <h2 className="mb-2 font-sans text-xl leading-[26px] font-medium">What kintsugi does</h2>
      <div className="flex flex-col gap-2">
        <Prose>
          Agents share memory in FalkorDB, and every read, write and handoff is recorded as a graph.
        </Prose>
        <Prose>
          When a source turns out to be poisoned, kintsugi walks that graph to find everything that
          depended on it.
        </Prose>
        <Prose>
          It re-runs only those actions against clean memory, and lists the effects that cannot be
          undone.
        </Prose>
        {s.phase !== "idle" && (
          <Prose>Select an action to see what it read and who it handed off to.</Prose>
        )}
      </div>
    </Section>
  );
}

export function Inspector({ s, cypher }: { s: State; cypher: Record<string, string> }) {
  const [d, setD] = useState<{ id: string; detail: ActionDetail } | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Refetch when the selection changes, and when its status changes during a repair.
  const status = s.selected ? (s.overrides[s.selected] ?? "") : "";
  useEffect(() => {
    if (!s.selected) return;
    let live = true;
    const id = s.selected;
    getJSON<ActionDetail>("/api/action", { id }).then(
      (detail) => {
        if (!live) return;
        setD({ id, detail });
        setError(null);
      },
      (e: unknown) => live && setError(e instanceof Error ? e.message : String(e)),
    );
    return () => {
      live = false;
    };
  }, [s.selected, status, s.phase]);

  return (
    <aside className="min-h-0 overflow-auto rounded-lg border border-rule bg-panel">
      {s.selected && error ? (
        <Section last>
          <Prose>error: {error}</Prose>
        </Section>
      ) : s.selected && d?.id === s.selected ? (
        <Detail s={s} d={d.detail} cypher={cypher} />
      ) : s.selected ? null : (
        <Empty s={s} cypher={cypher} />
      )}
    </aside>
  );
}
