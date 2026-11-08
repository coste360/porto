import { useQueryClient } from '@tanstack/react-query';
import { Link, Outlet } from '@tanstack/react-router';
import { MonitorIcon } from 'lucide-react';
import { useEffect } from 'react';

import { subscribeMarinaStream } from '@porto/shared/api/sse';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { BrandLogo } from '@porto/shared/ui/patterns';
import { OfflineIndicator } from '@porto/shared/ui/patterns/OfflineIndicator';

import { useCurrentMarina } from '../../auth/hooks';
import { setPreferFullVersion } from './lib/fullVersion';

/**
 * Shell of the phone screens (m6b-plan §6.1): top bar (brand mark, marina, online status and queue, link to the
 * full version), the content and, inside each page, its bottom tab bar (PhoneTabBar). Realtime updates as in
 * AppShell. Works from 360 px wide; touch targets are at least 44 px.
 */
export function PhoneShell() {
  const { t } = useT();
  const queryClient = useQueryClient();
  const { marinaId, marina } = useCurrentMarina();

  useEffect(() => subscribeMarinaStream(queryClient, marinaId), [queryClient, marinaId]);

  return (
    <div
      data-testid="phone-shell"
      className="bg-background flex min-h-dvh flex-col"
      aria-label={t('phone:shellLabel')}
    >
      <header className="bg-background border-border sticky top-0 z-40 flex h-14 items-center gap-2 border-b px-2">
        <BrandLogo variant="symbol" size="sm" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium" data-testid="phone-marina">
          {marina?.name}
        </span>
        <OfflineIndicator marinaId={marinaId} />
        <Button
          variant="ghost"
          size="icon"
          className="size-11"
          asChild
          aria-label={t('phone:fullVersion')}
          title={t('phone:fullVersion')}
        >
          <Link
            to="/m/$marinaId"
            params={{ marinaId }}
            onClick={() => setPreferFullVersion(true)}
            data-testid="phone-full-version"
          >
            <MonitorIcon />
          </Link>
        </Button>
      </header>
      <main className="flex-1 px-4 pt-4 pb-24">
        <Outlet />
      </main>
    </div>
  );
}
