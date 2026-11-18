import type { ReactNode } from "react";

function Block({ children }: { children: ReactNode }) {
  return (
    <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
      {children}
    </pre>
  );
}

export function JsonView({ value }: { value: unknown }) {
  return <Block>{JSON.stringify(value, null, 2)}</Block>;
}

export function TextView({ text }: { text: string }) {
  return <Block>{text}</Block>;
}
