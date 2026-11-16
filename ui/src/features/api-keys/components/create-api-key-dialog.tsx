import { zodResolver } from "@hookform/resolvers/zod";
import { Copy } from "lucide-react";
import { useState, type Ref } from "react";
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
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { useCreateApiKey } from "@/features/api-keys/queries";
import { createApiKeySchema, type CreateApiKeyValues } from "@/features/api-keys/schemas";
import { dismissGuard, scrollableDialogClass } from "@/lib/dialog";
import { setServerErrors } from "@/lib/form-errors";

const defaultValues: CreateApiKeyValues = { name: "" };

type CreateApiKeyDialogProps = {
  projectId: string;
  /** Lets the page move focus to the button. */
  triggerRef?: Ref<HTMLButtonElement>;
};

// Two steps in one dialog: the name form, then the new key. The key is shown only once, so it
// lives in this state and nowhere else.
export function CreateApiKeyDialog({ projectId, triggerRef }: CreateApiKeyDialogProps) {
  const [open, setOpen] = useState(false);
  const [key, setKey] = useState<string>();
  const createApiKey = useCreateApiKey();

  const form = useForm<CreateApiKeyValues>({
    resolver: zodResolver(createApiKeySchema),
    defaultValues,
  });

  function close() {
    setOpen(false);
    setKey(undefined);
    createApiKey.reset();
    form.reset(defaultValues);
  }

  function handleOpenChange(next: boolean) {
    if (next) {
      setOpen(true);
    } else if (!createApiKey.isPending) {
      close();
    }
  }

  function onSubmit({ name }: CreateApiKeyValues) {
    createApiKey.mutate(
      { params: { path: { projectId } }, body: { name } },
      {
        onSuccess: (created) => setKey(created.key),
        onError: (error) =>
          setServerErrors(
            form.setError,
            error,
            ["name"],
            "Could not create the API key. Try again.",
          ),
      },
    );
  }

  async function copyKey() {
    try {
      await navigator.clipboard.writeText(key ?? "");
      toast.success("Copied");
    } catch {
      toast.error("Could not copy");
    }
  }

  const guard = dismissGuard(createApiKey.isPending);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button ref={triggerRef}>New API key</Button>
      </DialogTrigger>
      <DialogContent
        className={scrollableDialogClass}
        {...guard}
        // A stray click must not lose the key.
        onInteractOutside={key === undefined ? guard.onInteractOutside : (e) => e.preventDefault()}
      >
        {key === undefined ? (
          <form onSubmit={form.handleSubmit(onSubmit)} noValidate>
            <DialogHeader>
              <DialogTitle>New API key</DialogTitle>
              <DialogDescription className="sr-only">New API key</DialogDescription>
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
                      placeholder="production workers"
                      aria-invalid={fieldState.invalid}
                    />
                    {fieldState.invalid && <FieldError errors={[fieldState.error]} />}
                  </Field>
                )}
              />
              {form.formState.errors.root && <FieldError errors={[form.formState.errors.root]} />}
            </FieldGroup>
            <DialogFooter>
              <Button type="submit" disabled={createApiKey.isPending}>
                Create key
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Save your API key</DialogTitle>
              <DialogDescription>Copy this key now: it will not be shown again.</DialogDescription>
            </DialogHeader>
            <div className="flex flex-col gap-2 py-4">
              <InputGroup>
                <InputGroupInput
                  readOnly
                  autoFocus
                  className="font-mono"
                  aria-label="API key"
                  value={key}
                  onFocus={(event) => event.currentTarget.select()}
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="Copy key"
                    onClick={() => void copyKey()}
                  >
                    <Copy />
                  </InputGroupButton>
                </InputGroupAddon>
              </InputGroup>
              <p className="text-sm text-muted-foreground">
                Set it as <code className="font-mono">RUNCOIL_API_KEY</code> where your workers run.
              </p>
            </div>
            <DialogFooter>
              <Button onClick={close}>Done</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
