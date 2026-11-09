import { Link } from '@tanstack/react-router';

import { useSensorAlerts, useSensorMap } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { sensorEntriesOf } from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Separator } from '@porto/shared/ui/separator';

import { AlertActions } from './AlertActions';

/**
 * The sensor section of the quick card (spec 9.3, m12-plan §9.2): device code, physical state and since, health,
 * and the open alerts of the resource with their actions; the history page has the readings.
 */
export function SensorQuickSection({
  marinaId,
  mapId,
  date,
  resourceId,
}: {
  marinaId: string;
  mapId: string;
  date: string;
  resourceId: string;
}) {
  const { t, i18n } = useT();
  const map = useSensorMap(marinaId, mapId, date);
  const entries = sensorEntriesOf(map.data, resourceId);
  const alerts = useSensorAlerts(marinaId, { status: 'open', map_id: mapId }, entries.length > 0);
  if (entries.length === 0) return null;
  const fmt = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'short', timeStyle: 'short' });
  const ids = new Set(entries.map((e) => e.device_id));
  return (
    <section className="flex flex-col gap-2" data-testid="quick-sensors">
      <Separator />
      <h3 className="text-sm font-semibold">{t('sensors:quick.title')}</h3>
      {entries.map((e) => (
        <div
          key={e.device_id}
          className="flex flex-col gap-1 text-sm"
          data-testid={`quick-sensor-${e.code}`}
        >
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{e.code}</span>
            <Badge variant={e.physical === 'occupied' ? 'default' : 'secondary'}>
              {t(`sensors:physical.${e.physical}`)}
            </Badge>
            {e.health ? (
              <Badge variant={e.health === 'ok' ? 'outline' : 'destructive'}>
                {t(`sensors:health.${e.health}`)}
              </Badge>
            ) : null}
            {e.discrepancy ? (
              <Badge variant="destructive">{t('sensors:lens.discrepancy')}</Badge>
            ) : null}
          </div>
          {e.physical_since ? (
            <span className="text-muted-foreground">
              {t('sensors:quick.since', { since: fmt.format(new Date(e.physical_since)) })}
            </span>
          ) : null}
        </div>
      ))}
      {(alerts.data?.items ?? [])
        .filter((a) => ids.has(a.device_id))
        .map((a) => (
          <div key={a.id} className="flex flex-col gap-1">
            <span className="text-sm">{t(`sensors:alertKind.${a.kind}`)}</span>
            <AlertActions marinaId={marinaId} alert={a} />
          </div>
        ))}
      <Link
        to="/m/$marinaId/sensors/history"
        params={{ marinaId }}
        search={{ resource_type: entries[0]!.resource_type, resource_id: resourceId }}
        className="text-sm underline-offset-4 hover:underline"
      >
        {t('sensors:quick.history')}
      </Link>
    </section>
  );
}
