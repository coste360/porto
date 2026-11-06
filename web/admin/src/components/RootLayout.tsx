import { Outlet } from '@tanstack/react-router';

import { Toaster } from '@porto/shared/ui/sonner';
import { TooltipProvider } from '@porto/shared/ui/tooltip';

export function RootLayout() {
  return (
    <TooltipProvider>
      <Outlet />
      <Toaster richColors closeButton />
    </TooltipProvider>
  );
}
