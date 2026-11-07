// Tabs of the contract page (m4-plan §8.1).
export const CONTRACT_TABS = [
  'data',
  'installments',
  'documents',
  'signature',
  'addenda',
  'renewal',
  'history',
] as const;
export type ContractTab = (typeof CONTRACT_TABS)[number];
