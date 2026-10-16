import { CodeIcon, CopyIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";

// One Cypher query as the server runs it. The text always comes from /api/cypher.
export function CypherBlock({ title, query }: { title: string; query: string | undefined }) {
  if (!query) return null;
  return (
    <div className="mt-2">
      <div className="flex items-center justify-between font-sans text-[13px] text-muted">
        <span className="flex items-center gap-1.5">
          <CodeIcon size={14} />
          {title}
        </span>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void navigator.clipboard?.writeText(query.trim())}
        >
          <CopyIcon size={13} />
          Copy
        </Button>
      </div>
      <pre className="mt-1.5 overflow-x-auto rounded-md border border-rule bg-canvas p-2.5 font-mono text-xs leading-[18px] whitespace-pre-wrap text-text">
        {query.trim()}
      </pre>
    </div>
  );
}
