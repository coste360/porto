import { useTranslation } from 'react-i18next';

import { NAMESPACES } from './resources';

/**
 * Translation hook over every namespace. Always pass fully qualified keys
 * (`t('team:members.title')`): i18n-check verifies those literals exist.
 */
export function useT() {
  return useTranslation([...NAMESPACES]);
}
