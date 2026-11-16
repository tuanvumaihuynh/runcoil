import { ChevronsUpDown } from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Command, CommandEmpty, CommandItem, CommandList } from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { useMembers, useUserLookup } from "@/features/members/queries";

type UserPickerProps = {
  projectId: string;
  /** The chosen username, or an empty string when nothing is chosen. */
  value: string;
  onChange: (username: string) => void;
  onBlur?: () => void;
  id?: string;
  "aria-invalid"?: boolean;
};

export function UserPicker({
  projectId,
  value,
  onChange,
  onBlur,
  id,
  "aria-invalid": ariaInvalid,
}: UserPickerProps) {
  const [open, setOpen] = useState(false);
  const lookup = useUserLookup(open);
  const members = useMembers(projectId);

  const memberIds = useMemo(
    () => new Set(members.data?.members.map((member) => member.userId)),
    [members.data],
  );
  const users = useMemo(
    () => lookup.data?.pages.flatMap((page) => page.users) ?? [],
    [lookup.data],
  );

  return (
    // Modal so the mouse wheel scrolls the list while the picker sits inside a dialog.
    <Popover open={open} onOpenChange={setOpen} modal>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-invalid={ariaInvalid}
          onBlur={onBlur}
          className="w-full justify-between font-normal"
        >
          {value ? value : <span className="text-muted-foreground">Select a user</span>}
          <ChevronsUpDown className="opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) p-0" align="start">
        <Command shouldFilter={false}>
          <CommandList>
            {lookup.isPending && !lookup.isError && (
              <div className="flex flex-col gap-1 p-1" aria-busy="true">
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
              </div>
            )}
            {lookup.isSuccess && <CommandEmpty>No users.</CommandEmpty>}
            {users.map((user) => {
              const isMember = memberIds.has(user.id);
              const selected = user.username === value;
              return (
                <CommandItem
                  key={user.id}
                  value={user.id}
                  disabled={isMember}
                  data-checked={selected}
                  onSelect={() => {
                    onChange(user.username);
                    setOpen(false);
                  }}
                >
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate">{user.username}</span>
                    {user.email && (
                      <span className="truncate text-xs text-muted-foreground">{user.email}</span>
                    )}
                  </div>
                  {isMember && (
                    <span className="shrink-0 text-xs text-muted-foreground">Already a member</span>
                  )}
                </CommandItem>
              );
            })}
            {lookup.hasNextPage && (
              <CommandItem
                value="load-more"
                disabled={lookup.isFetchingNextPage}
                className="text-muted-foreground"
                onSelect={() => void lookup.fetchNextPage()}
              >
                {lookup.isFetchingNextPage ? "Loading…" : "Load more"}
              </CommandItem>
            )}
            {lookup.isError && (
              <p className="px-2 py-1.5 text-sm text-muted-foreground">Could not load users.</p>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
