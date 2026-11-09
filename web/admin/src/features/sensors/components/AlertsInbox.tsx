import { Link } from '@tanstack/react-router';
import { useState } from 'react';

import { useSensorAlerts, type SensorAlertKind, type SensorAlertStatus } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { EnumSelect } from '../../contracts-m4/components/kit';
import { ALERT_KINDS, ALERT_STATUSES } from '../lib';
import { AlertActions } from './AlertActions';

/** The alert inbox of the Sensori page (m12-plan §16.1): filters by status and kind, actions per alert. */
export function AlertsInbox({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const [status, setStatus] = useState<SensorAlertStatus | ''>('open');
  const [kind, setKind] = useState<SensorAlertKind | ''>('');
  const alerts = useSensorAlerts(marinaId, {
    status: status || 'open',
    ...(kind ? { kind } : {}),
  });
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap gap-3">
        <EnumSelect
          id="alerts-status"
          label={t('sensors:alerts.status')}
          values={ALERT_STATUSES}
          value={status}
          onChange={setStatus}
          labelOf={(v) => t(`sensors:alertStatus.${v}`)}
        />
        <EnumSelect
          id="alerts-kind"
          label={t('sensors:alerts.kind')}
          values={ALERT_KINDS}
          value={kind}
          onChange={setKind}
          labelOf={(v) => t(`sensors:alertKind.${v}`)}
          allLabel={t('sensors:alerts.allKinds')}
        />
      </div>
      {alerts.isPending ? (
        <LoadingState rows={3} />
      ) : alerts.isError ? (
        <QueryErrorState error={alerts.error} onRetry={() => void alerts.refetch()} />
      ) : alerts.data.items.length === 0 ? (
        <p className="text-muted-foreground text-sm" data-testid="alerts-empty">
          {t('sensors:alerts.empty')}
        </p>
      ) : (
        <ul className="flex flex-col gap-3" data-testid="alerts-list">
          {alerts.data.items.map((a) => (
            <li key={a.id} data-testid={`alert-${a.kind}`} data-alert-id={a.id}>
              <Card>
                <CardHeader className="flex flex-row flex-wrap items-center gap-2">
                  <CardTitle className="text-base">{t(`sensors:alertKind.${a.kind}`)}</CardTitle>
                  <Badge variant="outline">{t(`sensors:alertStatus.${a.status}`)}</Badge>
                  {a.kind === 'early_return' && a.refs.resold ? (
                    <Badge variant="destructive">{t('sensors:alerts.resold')}</Badge>
                  ) : null}
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  <p className="text-sm">
                    {t('sensors:alerts.where', {
                      resource: a.resource_code,
                      device: a.device_code,
                      since: fmt.format(new Date(a.opened_at)),
                    })}
                  </p>
                  {a.refs.proposed_from && a.refs.proposed_to ? (
                    <p className="text-muted-foreground text-sm">
                      {t('sensors:alerts.proposal', {
                        from: a.refs.proposed_from,
                        to: a.refs.proposed_to,
                      })}
                    </p>
                  ) : null}
                  <div className="flex flex-wrap items-center gap-3">
                    <AlertActions marinaId={marinaId} alert={a} />
                    <Link
                      to="/m/$marinaId/sensors/history"
                      params={{ marinaId }}
                      search={{ resource_type: a.resource_type, resource_id: a.resource_id }}
                      className="text-sm underline-offset-4 hover:underline"
                    >
                      {t('sensors:alerts.history')}
                    </Link>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
