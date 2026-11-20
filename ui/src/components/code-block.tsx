import { Copy } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";

type CodeBlockProps = {
  code: string;
  /** Names the code for the copy button, such as "main.go". */
  label: string;
};

export function CodeBlock({ code, label }: CodeBlockProps) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      toast.success("Copied");
    } catch {
      toast.error("Could not copy");
    }
  }

  return (
    <div className="relative">
      {/* Wrapped, like the JSON blocks, so a long line cannot widen the page. */}
      <pre
        className="rounded-md bg-muted p-3 pr-10 font-mono text-xs whitespace-pre-wrap wrap-anywhere"
        style={{ tabSize: 4 }}
      >
        {code}
      </pre>
      <Button
        variant="ghost"
        size="icon-xs"
        className="absolute top-2 right-2"
        aria-label={`Copy ${label}`}
        onClick={() => void copy()}
      >
        <Copy />
      </Button>
    </div>
  );
}
