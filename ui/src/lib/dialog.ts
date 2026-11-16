// For a DialogContent that can be taller than a short screen: it scrolls instead of running off the page.
export const scrollableDialogClass = "max-h-[calc(100svh-2rem)] overflow-y-auto";

// Props for a DialogContent whose request is running. The dialog cannot be dismissed, so a request
// that ends later cannot close a dialog the user has reopened. Pair it with an onOpenChange that
// ignores closing while pending: Radix also closes through other paths than these two.
export function dismissGuard(pending: boolean) {
  return {
    // The close button comes from the shared dialog, which cannot be disabled, so it is left out.
    showCloseButton: !pending,
    onEscapeKeyDown: (event: Event) => {
      if (pending) {
        event.preventDefault();
      }
    },
    onInteractOutside: (event: Event) => {
      if (pending) {
        event.preventDefault();
      }
    },
  };
}
