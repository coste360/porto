// Slots other features fill in the phone screens without editing them (m6b-plan §11.3): the desktop bridge
// registers its "Stampa scontrino" button for the dock Cassa tab. A plain module registry: set once at start-up.
import type { ReactNode } from 'react';

export type DockSlots = {
  /** Rendered next to each payment recorded from the Cassa tab (e.g. the bridge receipt button). */
  receiptAction?: (paymentId: string) => ReactNode;
};

let slots: DockSlots = {};

export function registerDockSlots(next: DockSlots): void {
  slots = { ...slots, ...next };
}

export function dockSlots(): DockSlots {
  return slots;
}
