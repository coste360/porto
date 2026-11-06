import { AnchorIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../auth/hooks';

export function DashboardPage() {
  const { t } = useT();
  const { marina } = useCurrentMarina();
  return (
    <>
      <PageHeader
        title={marina?.name ?? t('common:dashboard.title')}
        description={t('common:dashboard.description')}
      />
      <EmptyState icon={<AnchorIcon />} title={t('common:dashboard.comingSoon')} />
    </>
  );
}
