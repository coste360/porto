import { Link, Navigate } from '@tanstack/react-router';
import { AnchorIcon, SmartphoneIcon } from 'lucide-react';

import { useIsMobile } from '@porto/shared/hooks/use-mobile';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../auth/hooks';
import { DeadlinesCard } from '../features/registry/deadlines/DeadlinesCard';

export function DashboardPage() {
  const { t } = useT();
  const { marina, marinaId, can } = useCurrentMarina();
  const isMobile = useIsMobile();
  // The dock staff land on the phone panel on small screens (m3-plan §8.1); other roles keep the overview.
  const dockOnly = marina?.roles.length === 1 && marina.roles[0] === 'dock';
  if (dockOnly && isMobile)
    return <Navigate to="/m/$marinaId/dock" params={{ marinaId }} replace />;
  return (
    <>
      <PageHeader
        title={marina?.name ?? t('common:dashboard.title')}
        description={t('common:dashboard.description')}
      />
      <div className="flex flex-col gap-6">
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
        </div>
        <EmptyState icon={<AnchorIcon />} title={t('common:dashboard.comingSoon')} />
      </div>
    </>
  );
}
