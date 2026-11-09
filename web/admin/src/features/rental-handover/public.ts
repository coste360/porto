// Public surface of the rental handover feature (m11-plan §14.2-§14.3): the booking page of the planner feature
// (BOOKING) imports the desktop panels from here.
export { QuickCashDialog as RentalQuickCashDialog } from './components/QuickCashDialog';
export { RentalHandoverPanel } from './components/RentalHandoverPanel';
export { SignaturePad } from './components/SignaturePad';
export { phaseOf as rentalHandoverPhase } from './lib/wizard';
