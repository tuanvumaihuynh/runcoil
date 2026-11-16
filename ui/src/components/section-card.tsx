import { ChevronRight, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import {
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item";

type SectionCardContentProps = {
  icon: LucideIcon;
  title: string;
  description: string;
};

// The inside of an overview card. The caller wraps it in `<Item variant="outline" asChild>` and a `Link`.
export function SectionCardContent({ icon: Icon, title, description }: SectionCardContentProps) {
  return (
    <>
      <ItemMedia variant="icon">
        <Icon />
      </ItemMedia>
      <ItemContent>
        <ItemTitle>{title}</ItemTitle>
        <ItemDescription>{description}</ItemDescription>
      </ItemContent>
      <ItemActions>
        <ChevronRight className="size-4 text-muted-foreground" />
      </ItemActions>
    </>
  );
}

export function SectionCardGrid({ children }: { children: ReactNode }) {
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{children}</div>;
}
