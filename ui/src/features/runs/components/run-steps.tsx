import type { components } from "@/api/schema";
import { JsonView, TextView } from "@/components/json-view";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import { formatDuration } from "@/lib/format";

type Step = components["schemas"]["Step"];

type RunStepsProps = {
  steps: Step[];
  /** When the Run started, or was created before it started. Each Step is dated from here. */
  since: string;
};

export function RunSteps({ steps, since }: RunStepsProps) {
  if (steps.length === 0) {
    return <p className="text-sm text-muted-foreground">No steps recorded yet.</p>;
  }

  const origin = new Date(since).getTime();

  return (
    <Accordion type="multiple">
      {steps.map((step, index) => (
        <AccordionItem key={step.id} value={step.id}>
          <AccordionTrigger className="items-center hover:no-underline">
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="text-muted-foreground">{index + 1}</span>
              <span className="font-mono group-hover/accordion-trigger:underline">{step.id}</span>
              <RunStatusBadge status={step.status} />
              <span className="font-normal text-muted-foreground">
                {step.tryCount === 1 ? "1 try" : `${step.tryCount} tries`}
              </span>
              <span className="font-normal text-muted-foreground">
                +{formatDuration(new Date(step.recordedAt).getTime() - origin)}
              </span>
            </span>
          </AccordionTrigger>
          <AccordionContent className="h-auto" style={{ animation: "none" }}>
            {step.error ? (
              <TextView text={`${step.error.code}: ${step.error.message}`} />
            ) : step.output !== undefined ? (
              <JsonView value={step.output} />
            ) : (
              step.status === "succeeded" && <p className="text-muted-foreground">No output.</p>
            )}
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
}
