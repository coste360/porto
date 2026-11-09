import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { useSensorHistory, type SensorNight, type SensorResourceType } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { useState } from 'react';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { PageFrame } from '../../contracts-m4/components/kit';

export type SensorHistorySearch = {
  resource_type?: SensorResourceType;
  resource_id?: string;
  from?: string;
  to?: string;
};

const route = getRouteApi('/auth/m/$marinaId/sensors/history');

const PHYSICAL_BG: Record<string, string> = {
  occupied: 'bg-harbor-sensor-occupied',
  free: 'bg-harbor-free',
  unknown: 'bg-muted',
};
const CONTRACT_BG: Record<string, string> = {
  free: 'bg-harbor-free',
  contract: 'bg-harbor-annual',
  transit: 'bg-harbor-transit',
  released: 'bg-harbor-released',
  released_resold: 'bg-harbor-transit',
  block: 'bg-harbor-blocked',
  maintenance: 'bg-harbor-maintenance',
};

/** One night of the timeline: contractual bar above, physical bar below, discrepancy marked (not colour only). */
function NightCell({ n }: { n: SensorNight }) {
  const { t } = useT();
  const label = `${n.night}: ${t(`reports:values.sensorContractual.${n.contractual}`)} / ${t(`sensors:physical.${n.physical}`)}`;
  return (
    <li
      className="flex w-6 flex-col items-center gap-1"
      title={label}
      aria-label={label}
      data-testid={`night-${n.night}`}
    >
      <span className={`h-3 w-full rounded-sm ${CONTRACT_BG[n.contractual] ?? 'bg-muted'}`} />
      <span className={`h-3 w-full rounded-sm ${PHYSICAL_BG[n.physical] ?? 'bg-muted'}`} />
      <span className="text-destructive h-3 text-xs leading-none">{n.discrepancy ? '!' : ''}</span>
    </li>
  );
}

/**
 * Physical history of a resource (m12-plan §10.1): timeline of the physical night against the contractual one,
 * state changes, alerts and, within the retention, the raw readings.
 */
export function SensorHistoryPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const search = route.useSearch();
  const navigate = useNavigate();
  const [readings, setReadings] = useState(false);
  const history = useSensorHistory(marinaId, { ...search, include_readings: readings });
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  const setRange = (k: 'from' | 'to', v: string) =>
    void navigate({
      to: '/m/$marinaId/sensors/history',
      params: { marinaId },
      search: { ...search, [k]: v || undefined },
    });
  return (
    <RequirePermission allowed={can('map.view')}>
      <PageFrame>
        <PageHeader
          title={t('sensors:history.title')}
          description={t('sensors:history.description')}
        />
        {!search.resource_id ? (
          <p className="text-muted-foreground text-sm">{t('sensors:history.pick')}</p>
        ) : history.isPending ? (
          <LoadingState rows={4} />
        ) : history.isError ? (
          <QueryErrorState error={history.error} onRetry={() => void history.refetch()} />
        ) : (
          <div className="flex flex-col gap-4" data-testid="sensor-history">
            <div className="flex flex-wrap items-end gap-3">
              <Field className="w-auto">
                <FieldLabel htmlFor="h-from">{t('sensors:dialog.from')}</FieldLabel>
                <Input
                  id="h-from"
                  type="date"
                  value={history.data.from}
                  onChange={(e) => setRange('from', e.target.value)}
                />
              </Field>
              <Field className="w-auto">
                <FieldLabel htmlFor="h-to">{t('sensors:dialog.to')}</FieldLabel>
                <Input
                  id="h-to"
                  type="date"
                  value={history.data.to}
                  onChange={(e) => setRange('to', e.target.value)}
                />
              </Field>
              <Field orientation="horizontal" className="w-auto">
                <Checkbox
                  id="h-readings"
                  checked={readings}
                  onCheckedChange={(v) => setReadings(v === true)}
                />
                <FieldLabel htmlFor="h-readings">{t('sensors:history.readings')}</FieldLabel>
              </Field>
            </div>
            <Card>
              <CardHeader>
                <CardTitle>{t('sensors:history.timeline')}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                <p className="text-muted-foreground text-xs">{t('sensors:history.legend')}</p>
                <ol className="flex flex-wrap gap-1">
                  {history.data.nights.map((n) => (
                    <NightCell key={n.night} n={n} />
                  ))}
                </ol>
                <p className="text-sm">
                  {t('sensors:history.stateAtFrom', {
                    state: t(`sensors:physical.${history.data.state_at_from}`),
                  })}
                </p>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>{t('sensors:history.changes')}</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('sensors:history.at')}</TableHead>
                      <TableHead>{t('sensors:history.change')}</TableHead>
                      <TableHead>{t('sensors:history.reason')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {history.data.changes.map((c) => (
                      <TableRow key={c.id}>
                        <TableCell>{fmt.format(new Date(c.at))}</TableCell>
                        <TableCell>
                          {t(`sensors:physical.${c.from}`)} → {t(`sensors:physical.${c.to}`)}
                        </TableCell>
                        <TableCell>{t(`sensors:reason.${c.reason}`)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
            {history.data.alerts.length > 0 ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('sensors:tabs.alerts')}</CardTitle>
                </CardHeader>
                <CardContent className="flex flex-wrap gap-2">
                  {history.data.alerts.map((a) => (
                    <Badge key={a.id} variant="outline">
                      {t(`sensors:alertKind.${a.kind}`)} · {t(`sensors:alertStatus.${a.status}`)}
                    </Badge>
                  ))}
                </CardContent>
              </Card>
            ) : null}
            {readings && history.data.readings ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('sensors:history.readings')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('sensors:history.at')}</TableHead>
                        <TableHead>{t('sensors:devices.physical')}</TableHead>
                        <TableHead className="text-right">{t('sensors:devices.battery')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {history.data.readings.map((r) => (
                        <TableRow key={`${r.device_id}-${r.observed_at}`}>
                          <TableCell>{fmt.format(new Date(r.observed_at))}</TableCell>
                          <TableCell>{t(`sensors:physical.${r.state}`)}</TableCell>
                          <TableCell className="text-right tabular-nums">
                            {r.battery_pct !== undefined ? `${r.battery_pct}%` : '–'}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </CardContent>
              </Card>
            ) : null}
          </div>
        )}
      </PageFrame>
    </RequirePermission>
  );
}
