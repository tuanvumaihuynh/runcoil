import type { ReactNode } from "react";

type SettingsRowProps = {
  label: string;
  description?: string;
  children: ReactNode;
};

export function SettingsRow({ label, description, children }: SettingsRowProps) {
  return (
    <div className="flex flex-col gap-3 border-b py-4 last:border-b-0 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <div className="flex min-w-0 flex-col gap-1">
        <div className="text-sm font-medium">{label}</div>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      <div className="shrink-0 text-sm">{children}</div>
    </div>
  );
}
