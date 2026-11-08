import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { PrinterIcon } from 'lucide-react';

import { useStorageMovements } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { MovementList } from '../components/MovementList';

const route = getRouteApi('/auth/m/$marinaId/storage/movements');

/** Movements of a day (m9-plan §7.4), print friendly. */
export function MovementsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate({ from: '/m/$marinaId/storage/movements' });
  const today = useMarinaToday();
  const date = search.date ?? today;
  const list = useStorageMovements(marinaId, date);
  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={t('storage:movements.title')}
        description={t('storage:movements.description', { date: formatIsoDate(date) })}
        actions={
          <Button variant="outline" onClick={() => window.print()} className="print:hidden">
            <PrinterIcon />
            {t('storage:movements.print')}
          </Button>
        }
      />
      <Field className="mb-4 max-w-48 print:hidden">
        <FieldLabel htmlFor="movements-date">{t('storage:movements.date')}</FieldLabel>
        <DatePicker
          id="movements-date"
          value={date}
          onChange={(d) => void navigate({ search: d ? { date: d } : {}, replace: true })}
        />
      </Field>
      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <MovementList marinaId={marinaId} list={list.data} canComplete={can('storage.manage')} />
      )}
    </RequirePermission>
  );
}
