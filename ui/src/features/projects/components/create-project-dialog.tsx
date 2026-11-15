import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { toast } from "sonner";

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

const defaultValues: CreateProjectValues = { name: "" };

export function CreateProjectDialog() {
  const [open, setOpen] = useState(false);
  const createProject = useCreateProject();

  const form = useForm<CreateProjectValues>({
    resolver: zodResolver(createProjectSchema),
    defaultValues,
  });

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) {
      form.reset(defaultValues);
    }
  }

  function onSubmit({ name }: CreateProjectValues) {
    createProject.mutate(
      { body: { name } },
      {
        onSuccess: () => {
          toast.success("Project created");
          handleOpenChange(false);
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
      <DialogTrigger asChild>
        <Button>New project</Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
          <DialogHeader>
            <DialogTitle>New project</DialogTitle>
            <DialogDescription>Projects hold runs, tasks, schedules and workers.</DialogDescription>
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
