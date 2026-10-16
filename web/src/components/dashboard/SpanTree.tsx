import { useState } from "react";
import {
  CaretDownIcon,
  CaretRightIcon,
  EnvelopeSimpleIcon,
  GitForkIcon,
  MagnifyingGlassIcon,
  ReceiptIcon,
  ShieldCheckIcon,
  type Icon,
} from "@phosphor-icons/react";
import { cn } from "cn";
import { statusOf, type State } from "@/dashboard/state";
import { groups, poisonedKey, type TicketGroup } from "@/dashboard/tickets";
import { Chip } from "./Chip";

const AGENT: Record<string, Icon> = {
  research: MagnifyingGlassIcon,
  triage: GitForkIcon,
  refund: ReceiptIcon,
  notifier: EnvelopeSimpleIcon,
};

function sourceOf(s: State, id: string): string {
  return s.graph.edges.find((e) => e.from === id && e.type === "INGESTED")?.to ?? "research";
}

function Group({
  g,
  s,
  open,
  onToggle,
  onSelect,
}: {
  g: TicketGroup;
  s: State;
  open: boolean;
  onToggle: () => void;
  onSelect: (id: string) => void;
}) {
  const selected = g.actions.some((a) => a.id === s.selected);
  const Caret = open ? CaretDownIcon : CaretRightIcon;
  return (
    <div className={cn("mb-0.5 rounded-md", selected && "bg-raised")}>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="grid h-[38px] w-full grid-cols-[18px_42px_1fr_auto] items-center gap-2 rounded-md px-2 text-left hover:bg-raised"
      >
        <Caret size={13} className="text-faint" />
        {g.id ? (
          <>
            <span className="font-mono text-[13px]">{g.id}</span>
            <span className="truncate font-sans text-[13px] text-muted">{g.decision}</span>
          </>
        ) : (
          <span className="col-span-2 font-sans text-[13px]">Sources</span>
        )}
        {g.outcome && g.outcome !== "ok" ? <Chip status={g.outcome} /> : <span />}
      </button>
      {open &&
        g.actions.map((a) => {
          const st = statusOf(s, a.id) ?? "ok";
          const I = AGENT[String(a.props.tool)] ?? GitForkIcon;
          const cur = a.id === s.selected;
          return (
            <button
              key={a.id}
              type="button"
              onClick={() => onSelect(a.id)}
              className={cn(
                "grid h-[30px] w-full grid-cols-[18px_20px_1fr_auto] items-center gap-2 rounded-[5px] pr-2 pl-[26px] text-left font-sans text-[13px] text-muted hover:text-text",
                cur && "bg-hover text-text",
              )}
            >
              <span />
              <I size={14} className="text-faint" />
              {a.props.ticket_id ? (
                <span className="truncate">
                  {String(a.props.tool)}
                  {st === "new" && " (new)"}
                </span>
              ) : (
                <span className="truncate font-mono text-[12.5px]">{sourceOf(s, a.id)}</span>
              )}
              {st !== "ok" ? <Chip status={st} /> : <span />}
            </button>
          );
        })}
    </div>
  );
}

export function SpanTree({ s, onSelect }: { s: State; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState<Set<string>>(new Set());
  const { shown, total, kept } = groups(s);
  const selTicket = s.graph.nodes.find((n) => n.id === s.selected)?.props.ticket_id as
    | string
    | undefined;
  const key = poisonedKey(s);
  const toggle = (id: string) =>
    setOpen((o) => {
      const n = new Set(o);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const shownTickets = shown.filter((g) => g.id).length;
  return (
    <section className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-rule bg-panel">
      <div className="flex h-11 items-center justify-between border-b border-rule px-3.5 font-sans font-medium">
        Affected tickets
        {total > 0 && (
          <small className="text-[12.5px] font-normal text-faint">
            {shownTickets} of {total}
          </small>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {s.phase === "idle" && (
          <p className="p-2 font-sans text-sm leading-[22px] text-muted">
            Run the scenario to record agents working a support queue. Each ticket shows up here as
            its actions land in the graph.
          </p>
        )}
        {shown.map((g) => (
          <Group
            key={g.id}
            g={g}
            s={s}
            // The selected ticket and the flagged sources start open; a click flips that.
            open={open.has(g.id) !== (selTicket === g.id || (g.id === "" && s.phase === "flagged"))}
            onToggle={() => toggle(g.id)}
            onSelect={onSelect}
          />
        ))}
        {kept > 0 && (
          <div className="mx-1 mt-2 mb-1 flex items-center gap-2 rounded-md border border-dashed border-line p-2.5 font-sans text-[13px] text-muted">
            <ShieldCheckIcon size={16} className="shrink-0 text-faint" />
            {kept} tickets never read {key ?? "the flagged facts"}. Kept as they were.
          </div>
        )}
      </div>
    </section>
  );
}
