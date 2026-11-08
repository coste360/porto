import { useStorageMovements } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { canUseStoragePhone } from '../../../lib/landing';
import { useMarinaToday } from '../../registry/lib/today';
import { MovementList } from '../../storage/public';

/**
 * Storage phone screen (m9-plan §12.1, yard_mgr): today's movements in order with equipment and the blocked exits
 * of the day; "Fatto" is online only (D18), the list is read-only when offline.
 */
export function StoragePhonePage() {
  const { t } = useT();
  const { marinaId, marina, can } = useCurrentMarina();
  const today = useMarinaToday();
  const list = useStorageMovements(marinaId, today);
  return (
    <RequirePermission allowed={canUseStoragePhone(marina?.permissions ?? [])}>
      <div data-testid="storage-phone" className="flex flex-col gap-4 p-4">
        <div>
          <h1 className="text-xl font-semibold">{t('storage:phone.title')}</h1>
          <p className="text-muted-foreground text-sm">
            {t('storage:phone.today', { date: formatIsoDate(today) })}
          </p>
        </div>
        {list.isPending ? (
          <LoadingState />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <MovementList marinaId={marinaId} list={list.data} canComplete={can('storage.manage')} />
        )}
      </div>
    </RequirePermission>
  );
}
