import { Link } from '@tanstack/react-router';

import { useSensorAlerts } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { useOnlineStatus } from '@porto/shared/offline';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardContent } from '@porto/shared/ui/card';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { AlertActions } from '../../sensors/components/AlertActions';

/**
 * Dock tab "Avvisi" (m12-plan §9.3): the open sensor alerts (kind, berth code, since) with "Mostra sulla mappa" and
 * the allowed actions. Online only: sensor alerts are not in the offline allowlist, so the tab says so offline.
 */
export function AlertsTab({ today }: { today: string }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const { online } = useOnlineStatus();
  const alerts = useSensorAlerts(marinaId, 'open', online);
  const fmt = new Intl.DateTimeFormat(i18n.language, {
    hour: '2-digit',
    minute: '2-digit',
    day: '2-digit',
    month: '2-digit',
  });
  if (!online)
    return (
      <p className="text-muted-foreground text-sm" data-testid="dock-alerts-offline">
        {t('sensors:dock.offline')}
      </p>
    );
  if (alerts.isPending) return <LoadingState rows={2} />;
  if (alerts.isError)
    return <QueryErrorState error={alerts.error} onRetry={() => void alerts.refetch()} />;
  const items = alerts.data.items;
  return (
    <section className="flex flex-col gap-3" data-testid="dock-alerts" data-date={today}>
      <h2 className="flex items-center gap-2 text-base font-semibold">
        {t('sensors:tabs.alerts')}
        <Badge data-testid="dock-alerts-count">{items.length}</Badge>
      </h2>
      {items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('sensors:alerts.empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {items.map((a) => (
            <li key={a.id} data-testid={`dock-alert-${a.kind}`}>
              <Card>
                <CardContent className="flex flex-col gap-2 p-3">
                  <div className="flex flex-wrap items-baseline justify-between gap-2">
                    <span className="font-medium">{t(`sensors:alertKind.${a.kind}`)}</span>
                    <span className="text-muted-foreground text-xs">
                      {fmt.format(new Date(a.opened_at))}
                    </span>
                  </div>
                  <span className="text-sm">{a.resource_code}</span>
                  <AlertActions marinaId={marinaId} alert={a} size="default" />
                  <Link
                    to="/m/$marinaId/ops"
                    params={{ marinaId }}
                    search={{ focus: a.resource_id }}
                    className="text-sm underline-offset-4 hover:underline"
                  >
                    {t('sensors:dock.showOnMap')}
                  </Link>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
