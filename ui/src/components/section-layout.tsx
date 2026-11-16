import { Link, type LinkProps } from "@tanstack/react-router";
import type { ReactNode } from "react";

type SectionLayoutProps = {
  label: string;
  sections: readonly { title: string; to: LinkProps["to"] }[];
  params?: LinkProps["params"];
  children: ReactNode;
};

// A left sub-nav (a horizontal row below md) next to the section page, shared by the
// project settings and the server admin layouts.
export function SectionLayout({ label, sections, params, children }: SectionLayoutProps) {
  return (
    <div className="grid gap-8 md:grid-cols-[12rem_minmax(0,1fr)]">
      <nav aria-label={label} className="flex gap-1 overflow-x-auto md:flex-col md:self-start">
        {sections.map((section) => (
          <Link
            key={section.to}
            to={section.to}
            params={params}
            className="shrink-0 rounded-md px-2 py-1.5 text-sm whitespace-nowrap"
            activeProps={{ className: "bg-accent font-medium text-foreground" }}
            inactiveProps={{ className: "text-muted-foreground hover:bg-accent/50" }}
          >
            {section.title}
          </Link>
        ))}
      </nav>
      {children}
    </div>
  );
}
