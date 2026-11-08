import { Link } from '@tanstack/react-router';
import { PlusIcon, WarehouseIcon } from 'lucide-react';

import { useStorageAreas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm2, formatInteger } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { EmptyState, PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';

/** Storage areas list (m9-plan §12.1): yard and shed surfaces with today's occupancy and blocked exits. */
export function StorageAreasPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const areas = useStorageAreas(marinaId);
  const items = areas.data?.items ?? [];
  return (
    <RequirePermission allowed={can('storage.read')}>
      <PageHeader
        title={t('storage:areas.title')}
        description={t('storage:areas.description')}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/m/$marinaId/storage/movements" params={{ marinaId }}>
                {t('storage:movements.title')}
              </Link>
            </Button>
            {can('storage.manage') ? (
              <Button asChild>
                <Link to="/m/$marinaId/storage/contracts/new" params={{ marinaId }}>
                  <PlusIcon />
                  {t('storage:contract.new')}
                </Link>
              </Button>
            ) : null}
          </>
        }
      />
      {areas.isPending ? (
        <LoadingState />
      ) : areas.isError ? (
        <QueryErrorState error={areas.error} onRetry={() => void areas.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState icon={<WarehouseIcon />} title={t('storage:areas.empty')} />
      ) : (
        <Table data-testid="storage-areas">
          <TableHeader>
            <TableRow>
              <TableHead>{t('storage:areas.area')}</TableHead>
              <TableHead>{t('storage:areas.mapKind')}</TableHead>
              <TableHead className="text-right">{t('storage:areas.occupancy')}</TableHead>
              <TableHead className="text-right">{t('storage:areas.boats')}</TableHead>
              <TableHead className="text-right">{t('storage:areas.exitsBlocked')}</TableHead>
              <TableHead className="text-right">{t('storage:areas.scenarios')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((a) => (
              <TableRow key={a.id} data-area={a.id}>
                <TableCell>
                  <Link
                    to="/m/$marinaId/storage/$areaId"
                    params={{ marinaId, areaId: a.id }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {a.name || a.code}
                  </Link>
                  {!a.gates_valid ? (
                    <Badge variant="destructive" className="ml-2">
                      {t('storage:areas.gatesInvalid')}
                    </Badge>
                  ) : null}
                </TableCell>
                <TableCell>
                  {t(`storage:mapKind.${a.map_kind}`, { defaultValue: a.map_kind })}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatCm2(a.occupied_cells_today * a.cell_cm * a.cell_cm, i18n.language)} /{' '}
                  {formatCm2(a.usable_cm2, i18n.language)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatInteger(a.boats_today, i18n.language)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {a.exits_blocked_next_30 > 0 ? (
                    <Badge variant="destructive">
                      {formatInteger(a.exits_blocked_next_30, i18n.language)}
                    </Badge>
                  ) : (
                    formatInteger(0, i18n.language)
                  )}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  <Link
                    to="/m/$marinaId/storage/$areaId/scenarios"
                    params={{ marinaId, areaId: a.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {formatInteger(a.draft_scenarios, i18n.language)}
                  </Link>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </RequirePermission>
  );
}
