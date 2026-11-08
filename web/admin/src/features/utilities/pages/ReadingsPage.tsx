import { useState } from 'react';

import {
  useConfirmMeterReading,
  useMeterReadingPhotoLink,
  useMeterReadings,
  useUtilityMeters,
  useVoidMeterReading,
  type MeterReading,
  type MeterReadingStatus,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import {
  EnumSelect,
  M4ProblemAlert,
  PageFrame,
  ReasonDialog,
} from '../../contracts-m4/components/kit';
import { Qty, ReadingStatusBadge, UtilitiesNav } from '../components/kit';

const STATUSES: readonly MeterReadingStatus[] = ['valid', 'anomaly', 'confirmed', 'void'];

/** Readings per meter (m9-plan §9.2): anomalies to confirm or void, photo links. */
export function ReadingsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const meters = useUtilityMeters(marinaId);
  const [meterId, setMeterId] = useState('');
  const [status, setStatus] = useState<MeterReadingStatus | ''>('');
  const readings = useMeterReadings(marinaId, {
    ...(meterId ? { meter_id: meterId } : {}),
    ...(status ? { status } : {}),
  });
  const confirm = useConfirmMeterReading(marinaId);
  const voidR = useVoidMeterReading(marinaId);
  const photo = useMeterReadingPhotoLink(marinaId);
  const [voiding, setVoiding] = useState<MeterReading | null>(null);
  const meterList = meters.data?.items ?? [];
  const unitOf = (id: string) => meterList.find((m) => m.id === id)?.unit;
  const items = readings.data?.items ?? [];
  const review = can('charges.manage');
  return (
    <RequirePermission allowed={can('marina.configure') || can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('utilities:readings.title')}
          description={t('utilities:readings.description')}
        />
        <UtilitiesNav marinaId={marinaId} current="readings" />
        <div className="flex flex-wrap items-end gap-4">
          <EnumSelect
            id="reading-meter"
            label={t('utilities:fields.meter')}
            values={meterList.map((m) => m.id)}
            value={meterId}
            onChange={setMeterId}
            labelOf={(id) => meterList.find((m) => m.id === id)?.code ?? id}
            allLabel={t('utilities:all')}
          />
          <EnumSelect
            id="reading-status"
            label={t('utilities:fields.status')}
            values={STATUSES}
            value={status}
            onChange={setStatus}
            labelOf={(s) => t(`utilities:enums.readingStatus.${s}`)}
            allLabel={t('utilities:all')}
          />
        </div>
        <M4ProblemAlert error={confirm.error ?? photo.error} />
        {readings.isPending ? (
          <LoadingState rows={5} />
        ) : readings.isError ? (
          <QueryErrorState error={readings.error} onRetry={() => void readings.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('utilities:fields.meter'),
              t('utilities:fields.readAt'),
              t('utilities:fields.value'),
              t('utilities:fields.delta'),
              t('utilities:fields.status'),
              '',
            ]}
            empty={t('utilities:readings.empty')}
            rows={items.length}
          >
            {items.map((r) => (
              <TableRow key={r.id} data-testid="reading-row">
                <TableCell>{meterList.find((m) => m.id === r.meter_id)?.code ?? ''}</TableCell>
                <TableCell>{formatDateTime(r.read_at, i18n.language)}</TableCell>
                <TableCell>
                  <Qty milli={r.value_milli} unit={unitOf(r.meter_id)} />
                </TableCell>
                <TableCell>
                  <Qty milli={r.delta_milli} unit={unitOf(r.meter_id)} />
                </TableCell>
                <TableCell className="flex flex-wrap gap-1">
                  <ReadingStatusBadge status={r.status} />
                  {r.anomaly_codes.map((c) => (
                    <Badge key={c} variant="outline">
                      {t(`utilities:enums.anomaly.${c}`)}
                    </Badge>
                  ))}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-2">
                    {r.photo_file_id ? (
                      <Button
                        size="sm"
                        variant="link"
                        onClick={() =>
                          photo.mutate(r.id, {
                            onSuccess: (l) => window.open(l.url, '_blank', 'noopener'),
                          })
                        }
                      >
                        {t('utilities:readings.photo')}
                      </Button>
                    ) : null}
                    {review && r.status === 'anomaly' ? (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={confirm.isPending}
                        onClick={() => confirm.mutate({ id: r.id, version: r.version })}
                      >
                        {t('utilities:readings.confirm')}
                      </Button>
                    ) : null}
                    {review && r.status !== 'void' ? (
                      <Button size="sm" variant="outline" onClick={() => setVoiding(r)}>
                        {t('utilities:readings.void')}
                      </Button>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        <ReasonDialog
          open={!!voiding}
          title={t('utilities:readings.void')}
          description={t('utilities:readings.voidHint')}
          confirmLabel={t('utilities:readings.void')}
          required
          destructive
          pending={voidR.isPending}
          error={voidR.error}
          onClose={() => setVoiding(null)}
          onConfirm={(reason) =>
            voiding &&
            voidR.mutate(
              { id: voiding.id, version: voiding.version, reason },
              { onSuccess: () => setVoiding(null) },
            )
          }
        />
      </PageFrame>
    </RequirePermission>
  );
}
