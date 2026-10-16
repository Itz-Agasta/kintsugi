import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cn } from "cn";

function Tabs({ className, ...props }: TabsPrimitive.Root.Props) {
  return <TabsPrimitive.Root data-slot="tabs" className={cn("flex", className)} {...props} />;
}

function TabsList({ className, ...props }: TabsPrimitive.List.Props) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn("flex items-stretch gap-5", className)}
      {...props}
    />
  );
}

// The active tab gets a 2px underline in --text, sitting on the panel rule.
function TabsTrigger({ className, ...props }: TabsPrimitive.Tab.Props) {
  return (
    <TabsPrimitive.Tab
      data-slot="tabs-trigger"
      className={cn(
        "relative flex items-center gap-2 font-sans text-[13px] text-faint transition-colors duration-150 hover:text-muted data-active:text-text",
        "after:absolute after:inset-x-0 after:-bottom-px after:h-0.5 after:bg-text after:opacity-0 data-active:after:opacity-100",
        className,
      )}
      {...props}
    />
  );
}

export { Tabs, TabsList, TabsTrigger };
