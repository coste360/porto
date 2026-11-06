import { Outlet } from '@tanstack/react-router';

import { Toaster } from '@porto/shared/ui/sonner';
import { TooltipProvider } from '@porto/shared/ui/tooltip';

export function RootLayout() {
  return (
    <TooltipProvider>
      <Outlet />
      {/* Theme-token colours (popover); sonner's richColors palette fails WCAG AA contrast. */}
      <Toaster closeButton />
    </TooltipProvider>
  );
}
