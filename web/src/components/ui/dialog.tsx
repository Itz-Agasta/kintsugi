import type { ReactNode } from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { XIcon } from "@phosphor-icons/react";
import { cn } from "cn";

function Dialog(props: DialogPrimitive.Root.Props) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />;
}

// A right-hand sheet on the page background: no blur, no shadow, a hairline edge.
function DialogContent({
  className,
  title,
  children,
  ...props
}: DialogPrimitive.Popup.Props & { title: ReactNode }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-bg/70 transition-opacity duration-150 data-ending-style:opacity-0 data-starting-style:opacity-0" />
      <DialogPrimitive.Popup
        data-slot="dialog-content"
        className={cn(
          "fixed inset-y-2.5 right-2.5 z-50 flex w-[min(640px,calc(100%-20px))] flex-col overflow-hidden rounded-lg border border-line bg-panel text-text outline-none",
          className,
        )}
        {...props}
      >
        <div className="flex h-11 shrink-0 items-center justify-between border-b border-rule px-4">
          <DialogPrimitive.Title className="font-sans text-sm font-medium">
            {title}
          </DialogPrimitive.Title>
          <DialogPrimitive.Close className="grid size-7 place-items-center rounded-md text-muted hover:bg-raised hover:text-text">
            <XIcon size={16} />
            <span className="sr-only">Close</span>
          </DialogPrimitive.Close>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-4">{children}</div>
      </DialogPrimitive.Popup>
    </DialogPrimitive.Portal>
  );
}

export { Dialog, DialogContent };
