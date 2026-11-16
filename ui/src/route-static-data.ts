// Typed staticData for the app shell. Routes set it; the header breadcrumb reads it.
declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    // Breadcrumb label of this route. The last crumb in the list is the current page.
    crumb?: string;
  }
}

export {};
