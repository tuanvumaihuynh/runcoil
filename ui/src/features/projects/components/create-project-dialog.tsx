import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

import type { components } from "@/api/schema";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { setServerErrors } from "@/lib/form-errors";
import { useCreateProject } from "@/features/projects/queries";
import { createProjectSchema, type CreateProjectValues } from "@/features/projects/schemas";

type Project = components["schemas"]["Project"];

const defaultValues: CreateProjectValues = { name: "" };

type CreateProjectDialogProps = {
  // Pass open and onOpenChange to control the dialog from elsewhere. Without them it renders its own button.
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  onCreated?: (project: Project) => void;
};

export function CreateProjectDialog({
  open: controlledOpen,
  onOpenChange,
  onCreated,
}: CreateProjectDialogProps) {
  const [ownOpen, setOwnOpen] = useState(false);
  const controlled = controlledOpen !== undefined;
  const open = controlledOpen ?? ownOpen;
  const createProject = useCreateProject();

  const form = useForm<CreateProjectValues>({
    resolver: zodResolver(createProjectSchema),
    defaultValues,
  });

  function handleOpenChange(next: boolean) {
    setOwnOpen(next);
    onOpenChange?.(next);
    if (!next) {
      form.reset(defaultValues);
    }
  }

  function onSubmit({ name }: CreateProjectValues) {
    createProject.mutate(
      { body: { name } },
      {
        onSuccess: (project) => {
          toast.success("Project created");
          handleOpenChange(false);
          onCreated?.(project);
        },
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["name"],
            "Could not create the project. Try again.",
          ),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      {!controlled && (
        <DialogTrigger asChild>
          <Button>New project</Button>
        </DialogTrigger>
      )}
      <DialogContent>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>New project</DialogTitle>
            <DialogDescription>
              Group tasks, runs, workers and members in one place.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="py-4">
            <Controller
              name="name"
              control={form.control}
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid}>
                  <FieldLabel htmlFor={field.name}>Name</FieldLabel>
                  <Input
                    {...field}
                    id={field.name}
                    autoComplete="off"
                    aria-invalid={fieldState.invalid}
                  />
                  {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                </Field>
              )}
            />
            {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
          </FieldGroup>
          <DialogFooter>
            <Button type="submit" disabled={createProject.isPending}>
              Create project
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
