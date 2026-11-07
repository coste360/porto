import { createContext, useContext, type ReactNode } from 'react';

import {
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import {
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';

// A form shown in a dialog (desktop) or in a bottom drawer (phone panel) uses the matching title primitives:
// Radix requires the title inside its own root.

export type SheetKind = 'dialog' | 'drawer' | 'plain';
const SheetContext = createContext<SheetKind>('dialog');

export function SheetKindProvider({ kind, children }: { kind: SheetKind; children: ReactNode }) {
  return <SheetContext.Provider value={kind}>{children}</SheetContext.Provider>;
}

export function SheetHeader({ title, description }: { title: ReactNode; description?: ReactNode }) {
  const kind = useContext(SheetContext);
  if (kind === 'plain')
    return (
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold">{title}</h2>
        {description ? <p className="text-muted-foreground text-sm">{description}</p> : null}
      </div>
    );
  if (kind === 'drawer')
    return (
      <DrawerHeader className="px-0">
        <DrawerTitle>{title}</DrawerTitle>
        {description ? <DrawerDescription>{description}</DrawerDescription> : null}
      </DrawerHeader>
    );
  return (
    <DialogHeader>
      <DialogTitle>{title}</DialogTitle>
      {description ? <DialogDescription>{description}</DialogDescription> : null}
    </DialogHeader>
  );
}

export function SheetFooter({ children }: { children: ReactNode }) {
  const kind = useContext(SheetContext);
  if (kind === 'plain') return <div className="flex flex-wrap justify-end gap-2">{children}</div>;
  return kind === 'drawer' ? (
    <DrawerFooter className="px-0">{children}</DrawerFooter>
  ) : (
    <DialogFooter>{children}</DialogFooter>
  );
}
