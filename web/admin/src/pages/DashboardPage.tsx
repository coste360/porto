import { Link, Navigate } from '@tanstack/react-router';
import { SmartphoneIcon } from 'lucide-react';

import { useIsMobile } from '@porto/shared/hooks/use-mobile';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../auth/hooks';
import { ManagerDashboard } from '../features/dashboard/ManagerDashboard';
import { useCanSeeDashboard } from '../features/dashboard/lib/access';
import { LocalModeCard } from '../features/desktop/LocalModeCard';
import { DeadlinesCard } from '../features/registry/deadlines/DeadlinesCard';
import { preferFullVersion } from '../features/phone/lib/fullVersion';
import { landingFor } from '../lib/landing';

export function DashboardPage() {
  const { t } = useT();
  const { marina, marinaId, can } = useCurrentMarina();
  const isMobile = useIsMobile();
  const canSeeTiles = useCanSeeDashboard();
  // On a phone (< 768 px) each role lands on its phone screen (m6b-plan §6.1, landingFor), unless the user chose
  // "Versione completa" in this browser session.
  const landing = landingFor(marina?.permissions ?? [], 'mobile', marinaId);
  const phoneTarget = landing.endsWith('/dock')
    ? 'dock'
    : landing.endsWith('/direction')
      ? 'direction'
      : null;
  if (isMobile && marina && phoneTarget && !preferFullVersion())
    return phoneTarget === 'dock' ? (
      <Navigate to="/m/$marinaId/dock" params={{ marinaId }} replace />
    ) : (
      <Navigate to="/m/$marinaId/direction" params={{ marinaId }} replace />
    );
  return (
    <>
      <PageHeader
        title={marina?.name ?? t('common:dashboard.title')}
        description={t(
          canSeeTiles ? 'common:dashboard.description' : 'common:dashboard.descriptionNoReports',
        )}
      />
      <div className="flex flex-col gap-6">
        <ManagerDashboard />
        <div className="grid gap-4 lg:grid-cols-2">
          {can('presence.write') ? (
            <Card data-testid="dashboard-dock-card">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <SmartphoneIcon />
                  {t('dock:card.title')}
                </CardTitle>
                <CardDescription>{t('dock:card.description')}</CardDescription>
              </CardHeader>
              <CardFooter>
                <Button asChild className="min-h-11">
                  <Link to="/m/$marinaId/dock" params={{ marinaId }}>
                    {t('dock:card.open')}
                  </Link>
                </Button>
              </CardFooter>
            </Card>
          ) : null}
          {can('customers.read') ? <DeadlinesCard /> : null}
          <LocalModeCard />
        </div>
      </div>
    </>
  );
}
