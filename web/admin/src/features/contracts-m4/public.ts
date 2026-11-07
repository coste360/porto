// Public surface of the contracts-m4 feature for the other features (m4-plan §8.1): the M4 tabs, badges and
// actions of the contract page (desk), the settle dialog and plan preview (rights holder instalments), the quote
// send and accept extensions, and the kit of small building blocks shared by the M4 pages.
export {
  ContractM4Actions,
  ContractM4Badges,
  ContractM4Tabs,
  ProposalCard,
} from './components/ContractM4Tabs';
export { CONTRACT_TABS, type ContractTab } from './lib/tabs';
export { SettleDialog } from './components/SettleDialog';
export { PlanPreview } from './components/PlanPreview';
export { SendQuoteDialog, AcceptPlanFields } from './components/QuoteM4';
