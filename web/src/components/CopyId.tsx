import { useState, type ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

// A machine id that copies itself on click.
export function CopyId({
  value,
  icon,
  children,
}: {
  value: string;
  icon?: ReactNode;
  children?: ReactNode;
}) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    });
  };
  return (
    <Tooltip>
      <TooltipTrigger
        onClick={copy}
        className="inline-flex items-center gap-1.5 rounded-sm border border-rule bg-panel px-[7px] py-[3px] font-mono text-[12.5px] leading-4 text-muted hover:text-text [&_svg]:text-faint"
      >
        {icon}
        {children ?? value}
      </TooltipTrigger>
      <TooltipContent>{copied ? "Copied" : "Copy"}</TooltipContent>
    </Tooltip>
  );
}
