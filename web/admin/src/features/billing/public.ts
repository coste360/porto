// Public surface of the billing feature for the other features (m5-plan §9.1): the customer dashboard and
// statement tabs, the transit collect and payment-link actions, the contract instalment charge panel, the SEPA
// mandates and the mandate settle action.
export {
  CustomerDashboardTab,
  CustomerStatementTab,
  SepaMandatesPanel,
} from './components/CustomerBillingTabs';
export {
  ContractChargesCard,
  DeskPaymentFields,
  MandateSettleButton,
  TransitPaymentLinkButton,
} from './components/Extensions';
export { defaultDeskPayment, deskPaymentBody, type DeskPaymentValue } from './lib/catalog';
