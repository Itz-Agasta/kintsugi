import { cn } from "cn";

// ViewSwitch flips the provenance canvas between the flat lanes and the 3D stack.
export function ViewSwitch({
  view,
  onView,
}: {
  view: "2d" | "3d";
  onView: (v: "2d" | "3d") => void;
}) {
  return (
    <div className="flex rounded-md border border-line p-0.5 font-mono text-xs">
      {(["2d", "3d"] as const).map((v) => (
        <button
          key={v}
          type="button"
          onClick={() => onView(v)}
          aria-pressed={view === v}
          className={cn(
            "rounded-[4px] px-2 py-0.5 transition-colors duration-150",
            view === v ? "bg-raised text-text" : "text-faint hover:text-muted",
          )}
        >
          {v.toUpperCase()}
        </button>
      ))}
    </div>
  );
}
