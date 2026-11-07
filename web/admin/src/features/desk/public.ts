import type { components } from '@porto/shared/api';

// Public surface of the desk for WEB-OPSMAP (m3-plan §8.7; signatures fixed). WEB-OPSMAP imports desk components
// only from here. The components accept the props of §8.7; some take extra optional props for desk pages.

type Solution = components['schemas']['OccupancySolution'];
type PlacementInput = components['schemas']['PlacementInput'];

export type DeskPrefill = {
  target?: PlacementInput;
  arrival?: string;
  departure?: string;
  vesselId?: string;
  solution?: Solution;
};

export { IncomingVesselPanel } from './search/IncomingVesselPanel';
export { TodayMovementsPanel } from './movements/TodayMovementsPanel';
export { NewTransitDialog } from './transits/NewTransitDialog';
export { NewQuoteDialog } from './quotes/NewQuoteDialog';
export { PresenceDialog } from './dialogs/PresenceDialog';
export { ReleaseDialog } from './dialogs/ReleaseDialog';
export { BlockDialog } from './dialogs/BlockDialog';
export { UnexpectedArrivalDialog } from './dialogs/UnexpectedArrivalDialog';
