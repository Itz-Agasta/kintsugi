import {
  ArrowCounterClockwiseIcon,
  ArrowsClockwiseIcon,
  CaretRightIcon,
  CheckIcon,
  CircleDashedIcon,
  CircleIcon,
  CircleNotchIcon,
  CirclesThreePlusIcon,
  CodeIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  FlagIcon,
  GitForkIcon,
  GraphIcon,
  MagnifyingGlassIcon,
  PaperPlaneTiltIcon,
  PlayIcon,
  ReceiptIcon,
  ShieldCheckIcon,
  SkipForwardIcon,
  StackIcon,
  TimerIcon,
  WrenchIcon,
  type Icon,
} from "@phosphor-icons/react";
import { cn } from "cn";
import { CopyId } from "@/components/CopyId";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SOURCE } from "@/lib/types";
import { busy, stats, step, strip, type State, type Tab, type UiStatus } from "@/dashboard/state";

export type Times = { flagged: string | null; started: number | null; ended: number | null };

function Logo() {
  return (
    <svg width="22" height="22" viewBox="0 0 22 22" aria-hidden="true">
      <path
        d="M3 8 Q3 19 11 19 Q19 19 19 8 Z"
        fill="none"
        stroke="var(--text)"
        strokeWidth="1.4"
        strokeLinejoin="round"
      />
      <path
        d="M7 8 L9 11.5 L7.5 14 L11 17.5"
        fill="none"
        stroke="var(--gold)"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function ChromeStrip({
  graphId,
  repairId,
  up,
  onCypher,
  onReset,
  resetDisabled,
}: {
  graphId: string;
  repairId: string | null;
  up: boolean | null;
  onCypher: () => void;
  onReset: () => void;
  resetDisabled: boolean;
}) {
  return (
    <div className="flex h-11 items-center gap-3.5 pr-1.5 pl-2.5">
      <div className="flex items-center gap-[9px] font-sans text-[15px] font-medium tracking-[-0.01em]">
        <Logo />
        Kintsugi
      </div>
      <div className="flex min-w-0 items-center gap-2 overflow-hidden text-faint">
        <CaretRightIcon size={14} />
        <CopyId value={graphId} icon={<GraphIcon size={13} />} />
        {repairId && (
          <>
            <CaretRightIcon size={14} />
            <CopyId value={repairId} icon={<WrenchIcon size={13} />}>
              repair {repairId.slice(0, 8)}
            </CopyId>
          </>
        )}
      </div>
      <div className="flex-1" />
      <span className="flex items-center gap-[7px] font-sans text-[13px] whitespace-nowrap text-muted max-[1000px]:hidden">
        {up === false ? (
          <CircleIcon size={9} className="text-faint" />
        ) : (
          <CircleIcon size={9} weight="fill" className="text-muted" />
        )}
        {up === null ? "Checking API" : up ? "FalkorDB connected" : "API unreachable"}
      </span>
      <Button onClick={onCypher}>
        <CodeIcon size={16} />
        Cypher
      </Button>
      <Button onClick={onReset} disabled={resetDisabled}>
        <ArrowCounterClockwiseIcon size={16} />
        Reset
        <kbd className="rounded-[3px] border border-line px-1 font-mono text-[11px] leading-4 text-faint">
          R
        </kbd>
      </Button>
    </div>
  );
}

const TITLES = {
  idle: "Ready to run",
  running: "Running scenario",
  ran: "Scenario recorded",
  flagged: `${SOURCE} flagged`,
  repairing: `Repairing ${SOURCE}`,
  repaired: `Repaired ${SOURCE}`,
};

const STAT_ICONS: Record<string, Icon> = {
  Actions: StackIcon,
  Facts: DatabaseIcon,
  Effects: PaperPlaneTiltIcon,
  "Blast radius": CirclesThreePlusIcon,
  Outside: ShieldCheckIcon,
  "Re-run": ArrowsClockwiseIcon,
  Skipped: SkipForwardIcon,
  Pending: CircleDashedIcon,
  "Can't undo": PaperPlaneTiltIcon,
};

const AGENTS: Icon[] = [MagnifyingGlassIcon, GitForkIcon, ReceiptIcon, EnvelopeSimpleIcon];

function seconds(ms: number) {
  return `${Math.max(0, Math.round(ms / 1000))}s`;
}

function Meta({ s, times, now }: { s: State; times: Times; now: number }) {
  if (s.phase === "idle") {
    return <span>Records a support desk where agents share memory in FalkorDB.</span>;
  }
  const agents = (
    <span className="flex items-center gap-1.5">
      <span className="flex gap-0.5">
        {AGENTS.map((I, i) => (
          <i
            key={i}
            className="grid size-[22px] place-items-center rounded-[5px] bg-raised text-muted"
          >
            <I size={13} />
          </i>
        ))}
      </span>
      across {s.graph.nodes.filter((n) => n.label === "Agent").length} agents
    </span>
  );
  return (
    <>
      {times.flagged && (
        <span className="flex items-center gap-1.5">
          <FlagIcon size={16} className="text-faint" />
          Flagged at {times.flagged}
        </span>
      )}
      {times.started !== null && (
        <span className="flex items-center gap-1.5">
          <TimerIcon size={16} className="text-faint" />
          {times.ended !== null
            ? `took ${seconds(times.ended - times.started)}`
            : `${seconds(now - times.started)} so far`}
        </span>
      )}
      {agents}
    </>
  );
}

const PRIMARY: Partial<Record<State["phase"], { label: string; icon: Icon }>> = {
  idle: { label: "Run scenario", icon: PlayIcon },
  running: { label: "Running", icon: CircleNotchIcon },
  ran: { label: `Flag ${SOURCE}`, icon: FlagIcon },
  flagged: { label: "Repair", icon: WrenchIcon },
  repairing: { label: "Repairing", icon: CircleNotchIcon },
};

export function RunHeader({
  s,
  times,
  now,
  onPrimary,
  onTab,
}: {
  s: State;
  times: Times;
  now: number;
  onPrimary: () => void;
  onTab: (t: Tab) => void;
}) {
  const primary = PRIMARY[s.phase];
  const spinning = busy(s);
  const repairing = s.phase === "repairing";
  // Once a run starts the header folds to one row, so the stage gets the height.
  const compact = s.phase !== "idle";
  const tile = (
    <span
      className={cn(
        "grid shrink-0 place-items-center rounded-[7px] border",
        compact ? "size-8" : "size-7",
        repairing ? "border-gold-line bg-gold-tint text-gold" : "border-line bg-raised text-muted",
      )}
    >
      {spinning ? (
        <CircleNotchIcon size={16} className="animate-spin motion-reduce:animate-none" />
      ) : s.phase === "repaired" ? (
        <CheckIcon size={16} />
      ) : (
        <Logo />
      )}
    </span>
  );
  const button = primary ? (
    <Button
      size={compact ? "default" : "lg"}
      variant={s.phase === "flagged" || repairing ? "repair" : "primary"}
      disabled={spinning}
      onClick={onPrimary}
      className={compact ? "h-9 px-3.5" : undefined}
    >
      <primary.icon
        size={16}
        className={spinning ? "animate-spin motion-reduce:animate-none" : undefined}
      />
      {primary.label}
    </Button>
  ) : null;

  if (compact) {
    return (
      <div className="flex min-h-16 flex-wrap items-center gap-x-6 gap-y-2 rounded-[10px] border border-rule bg-panel px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-3">
          {tile}
          <div className="min-w-0">
            <h1 className="truncate font-sans text-[17px] leading-[22px] font-medium tracking-[-0.01em]">
              {TITLES[s.phase]}
            </h1>
            <div className="flex gap-4 font-sans text-xs leading-[18px] text-muted">
              <Meta s={s} times={times} now={now} />
            </div>
          </div>
        </div>
        <StepDots s={s} />
        <div className="flex-1" />
        <div className="flex">
          {stats(s).map((st) => {
            const I = STAT_ICONS[st.label];
            return (
              <div key={st.label} className="border-l border-rule px-4">
                <div className="flex items-center gap-1.5 font-sans text-[11.5px] leading-4 text-muted">
                  {I && <I size={12} />}
                  {st.label}
                </div>
                <div className={cn("font-mono text-[22px] leading-7", st.gold && "text-gold")}>
                  {st.value}
                </div>
              </div>
            );
          })}
        </div>
        {s.phase === "repaired" ? (
          <Tabs value={s.tab} onValueChange={(v) => onTab(v as Tab)}>
            <TabsList className="h-9">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="diff">Diff</TabsTrigger>
              <TabsTrigger value="drafts">Drafts</TabsTrigger>
            </TabsList>
          </Tabs>
        ) : null}
        {button}
      </div>
    );
  }

  return (
    <div className="rounded-[10px] border border-rule bg-panel">
      <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-7 px-[22px] py-[18px]">
        <div className="min-w-0">
          <div className="flex items-center gap-3">
            {tile}
            <h1 className="truncate font-sans text-[22px] leading-7 font-medium tracking-[-0.015em]">
              {TITLES[s.phase]}
            </h1>
          </div>
          <div className="mt-[7px] ml-10 flex flex-wrap gap-5 font-sans text-[13px] text-muted">
            <Meta s={s} times={times} now={now} />
          </div>
        </div>
        {button}
      </div>
      <div className="flex items-center border-t border-rule px-[22px] py-[11px]">
        <StepDots s={s} labels />
      </div>
    </div>
  );
}

const STEPS = ["Run scenario", "Flag source", "Repair", "Verify against clean run"];

// StepDots is the four-step track. Compact, only the current step is named.
function StepDots({ s, labels }: { s: State; labels?: boolean }) {
  const cur = step(s);
  const allDone = s.phase === "repaired";
  return (
    <div className="flex items-center">
      {STEPS.map((label, i) => {
        const n = i + 1;
        const done = n < cur || allDone;
        const now = n === cur && !allDone;
        return (
          <span key={label} className="flex items-center" title={label}>
            {i > 0 && (
              <span className={cn("mx-2 h-px w-5", done || now ? "bg-muted" : "bg-line")} />
            )}
            <span
              className={cn(
                "grid size-5 place-items-center rounded-full border font-mono text-[11px]",
                done
                  ? "border-muted bg-raised text-muted"
                  : now
                    ? "border-gold text-gold"
                    : "border-line text-faint",
              )}
            >
              {done ? <CheckIcon size={11} /> : n}
            </span>
            {(labels || now) && (
              <span
                className={cn(
                  "ml-2 font-sans text-[13px]",
                  now ? "text-text" : done ? "text-muted" : "text-faint",
                )}
              >
                {label}
              </span>
            )}
          </span>
        );
      })}
    </div>
  );
}

const CELL: Record<UiStatus, string> = {
  ok: "border-faint",
  poisoned: "border-text bg-text",
  rerun_changed: "border-gold bg-gold",
  rerun_same: "border-muted bg-muted",
  skipped: "border-muted",
  superseded:
    "border-faint bg-[repeating-linear-gradient(135deg,transparent_0_3px,var(--faint)_3px_4px)]",
  new: "border-2 border-gold",
  running: "border-gold shadow-[0_0_0_2px_var(--gold-line)]",
  pending: "border-dashed border-faint opacity-80",
};

export function ProgressStrip({ s }: { s: State }) {
  const cells = strip(s);
  if (cells.length === 0) return null;
  const resolved = cells.filter((c) => !["pending", "running", "ok"].includes(c.status)).length;
  return (
    <div className="flex items-center gap-3 font-sans text-[12.5px] text-muted">
      {resolved} of {cells.length} resolved
      <div className="flex flex-wrap gap-[3px]">
        {cells.map((c) => (
          <span
            key={c.id}
            className={cn(
              "size-[13px] rounded-[3px] border transition-colors duration-150",
              CELL[c.status],
            )}
          />
        ))}
      </div>
    </div>
  );
}
