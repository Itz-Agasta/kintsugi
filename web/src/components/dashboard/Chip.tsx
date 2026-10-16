import {
  ArrowsClockwiseIcon,
  CircleDashedIcon,
  CircleNotchIcon,
  EqualsIcon,
  FlagIcon,
  PlusIcon,
  SkipForwardIcon,
  XIcon,
  type Icon,
} from "@phosphor-icons/react";
import { cn } from "cn";
import type { UiStatus } from "@/dashboard/state";

const CHIPS: Record<UiStatus, { label: string; icon?: Icon; cls: string }> = {
  ok: { label: "Same", cls: "text-faint" },
  poisoned: { label: "Poisoned", icon: FlagIcon, cls: "bg-text text-bg" },
  rerun_changed: { label: "Changed", icon: ArrowsClockwiseIcon, cls: "bg-gold-tint text-gold" },
  rerun_same: { label: "Same, stopped", icon: EqualsIcon, cls: "bg-raised text-text" },
  skipped: { label: "Skipped", icon: SkipForwardIcon, cls: "text-muted" },
  superseded: { label: "Superseded", icon: XIcon, cls: "text-faint line-through" },
  new: { label: "New", icon: PlusIcon, cls: "text-gold shadow-[inset_0_0_0_1px_var(--gold-line)]" },
  running: { label: "Running", icon: CircleNotchIcon, cls: "text-gold" },
  pending: { label: "Pending", icon: CircleDashedIcon, cls: "text-faint" },
};

export function Chip({
  status,
  label,
  className,
}: {
  status: UiStatus;
  label?: string;
  className?: string;
}) {
  const c = CHIPS[status];
  const I = c.icon;
  return (
    <span
      className={cn(
        "inline-flex h-[22px] items-center gap-[5px] rounded-sm px-[7px] font-sans text-xs whitespace-nowrap transition-colors duration-150",
        c.cls,
        className,
      )}
    >
      {I && (
        <I
          size={13}
          weight={status === "poisoned" ? "fill" : "regular"}
          className={status === "running" ? "animate-spin motion-reduce:animate-none" : undefined}
        />
      )}
      {label ?? c.label}
    </span>
  );
}
