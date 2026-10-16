import { cn } from "cn";
import type { LogLine } from "@/dashboard/state";

export function LogStrip({ log }: { log: LogLine[] }) {
  return (
    <div className="flex min-h-[78px] flex-col gap-0.5 border-t border-rule px-3.5 py-2 font-mono text-xs leading-[18px] text-muted">
      {log.slice(0, 3).map((l, i) => (
        <div
          key={`${l.t}-${i}-${l.text}`}
          className={cn("truncate", l.gold && "text-gold", l.error && "text-text")}
        >
          <span className="mr-2.5 text-faint">{l.t}</span>
          {l.text}
        </div>
      ))}
    </div>
  );
}
