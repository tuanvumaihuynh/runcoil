import { PageHeader } from "@/components/page-header";

type PagePlaceholderProps = {
  title: string;
  description: string;
};

export function PagePlaceholder({ title, description }: PagePlaceholderProps) {
  return (
    <div className="flex w-full flex-col gap-6">
      <PageHeader title={title} description={description} />
      <p className="text-sm text-muted-foreground">This page is not built yet.</p>
    </div>
  );
}
