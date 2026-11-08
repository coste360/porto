// Slots other features fill in the phone screens without editing them (m6b-plan §11.3): the desktop bridge
// registers its "Stampa scontrino" button for the dock Cassa tab; M9 utilities registers the dock tab "Letture"
// (m9-plan §12.2). A plain module registry: set once at start-up.
import type { ReactNode } from 'react';

import type { Permission } from '@porto/shared/api';

/** An extra dock tab, shown before "Altro" when the user has the permission. */
export type DockExtraTab = {
  /** Value of ?tab= (lowercase letters, not one of the built-in tabs). */
  value: string;
  /** i18n key of the tab label. */
  labelKey: string;
  icon: ReactNode;
  /** Shown only with this permission (none = always). */
  permission?: Permission;
  /** The tab content for the marina day (YYYY-MM-DD). */
  render: (date: string) => ReactNode;
};

export type DockSlots = {
  /** Rendered next to each payment recorded from the Cassa tab (e.g. the bridge receipt button). */
  receiptAction?: (paymentId: string) => ReactNode;
  /** Extra tabs of the dock screen (M9: "Letture"); at most one per feature. */
  extraTabs?: readonly DockExtraTab[];
};

let slots: DockSlots = {};

export function registerDockSlots(next: DockSlots): void {
  const extraTabs = [...(slots.extraTabs ?? []), ...(next.extraTabs ?? [])];
  slots = { ...slots, ...next, ...(extraTabs.length > 0 ? { extraTabs } : {}) };
}

export function dockSlots(): DockSlots {
  return slots;
}
