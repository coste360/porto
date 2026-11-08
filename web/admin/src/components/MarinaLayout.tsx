import { useMatches } from '@tanstack/react-router';
import { useEffect } from 'react';
import { toast } from 'sonner';

import { useT } from '@porto/shared/i18n';
import { takeMarinaSwitchBlocked, useMarinaSwitchBlocked } from '@porto/shared/offline';

import { PhoneShell } from '../features/phone/PhoneShell';
import { AppShell } from './AppShell';

declare module '@tanstack/react-router' {
  interface StaticDataRouteOption {
    /** 'phone' renders the route inside PhoneShell instead of AppShell (M6b phone screens, m6b-plan §6.1). */
    shell?: 'phone';
  }
}

/** Layout of the marina routes: the phone shell for routes marked `staticData.shell = 'phone'`, else AppShell. */
export function MarinaLayout() {
  const phone = useMatches({ select: (ms) => ms.some((m) => m.staticData?.shell === 'phone') });
  const { t } = useT();
  const blocked = useMarinaSwitchBlocked();
  // The route guard refused to enter another marina with unsent ops here (D9).
  useEffect(() => {
    if (takeMarinaSwitchBlocked()) toast.error(t('offline:marinaSwitchBlocked'));
  }, [blocked, t]);
  return phone ? <PhoneShell /> : <AppShell />;
}
