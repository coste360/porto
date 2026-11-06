import { AnchorIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../auth/hooks';
import { DeadlinesCard } from '../features/registry/deadlines/DeadlinesCard';

export function DashboardPage() {
  const { t } = useT();
  const { marina, can } = useCurrentMarina();
  return (
    <>
      <PageHeader
        title={marina?.name ?? t('common:dashboard.title')}
        description={t('common:dashboard.description')}
      />
      <div className="flex flex-col gap-6">
        {can('customers.read') ? (
          <div className="grid gap-4 lg:grid-cols-2">
            <DeadlinesCard />
          </div>
        ) : null}
        <EmptyState icon={<AnchorIcon />} title={t('common:dashboard.comingSoon')} />
      </div>
    </>
  );
}
