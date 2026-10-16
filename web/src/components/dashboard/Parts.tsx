import type { ReactNode } from "react";
import { CopyIcon, NotePencilIcon, PaperPlaneTiltIcon, ReceiptIcon } from "@phosphor-icons/react";
import { cn } from "cn";
import { Button } from "@/components/ui/button";
import type { UiStatus } from "@/dashboard/state";
import type { CompItem } from "@/lib/types";
import { Chip } from "./Chip";

export function Section({
  title,
  children,
  last,
}: {
  title?: string;
  children: ReactNode;
  last?: boolean;
}) {
  return (
    <div className={cn("px-4 py-3.5", !last && "border-b border-rule")}>
      {title && <h3 className="mb-2.5 font-sans text-[13px] font-medium text-muted">{title}</h3>}
      {children}
    </div>
  );
}

export function Prose({ children }: { children: ReactNode }) {
  return <p className="max-w-[60ch] font-sans text-sm leading-[22px] text-muted">{children}</p>;
}

// A drafted follow-up for one effect that cannot be undone.
export function UndoCard({ item, sentence }: { item?: CompItem; sentence: string }) {
  const I = item?.kind === "refund" ? ReceiptIcon : PaperPlaneTiltIcon;
  return (
    <div className="overflow-hidden rounded-[7px] border border-line">
      <div className="flex items-center gap-2 bg-raised px-2.5 py-[9px] font-sans text-[13px]">
        <I size={15} className="text-muted" />
        <span className="flex-1">{sentence}</span>
        {item && <Chip status={item.action_status as UiStatus} />}
      </div>
      <div className="p-2.5 font-sans text-[13px] leading-5 text-muted">
        {item
          ? item.draft || item.draft_error
          : "The drafted follow-up arrives with the repair report."}
      </div>
      {item?.draft && (
        <div className="flex items-center justify-between border-t border-rule px-2.5 py-2">
          <span className="inline-flex h-[22px] items-center gap-[5px] rounded-sm px-[7px] font-sans text-xs text-gold shadow-[inset_0_0_0_1px_var(--gold-line)]">
            <NotePencilIcon size={13} />
            Draft, not sent
          </span>
          <Button size="sm" onClick={() => void navigator.clipboard?.writeText(item.draft)}>
            <CopyIcon size={14} />
            Copy
          </Button>
        </div>
      )}
    </div>
  );
}
