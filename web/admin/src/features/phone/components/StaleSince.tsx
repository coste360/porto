import { useT } from '@porto/shared/i18n';
import { useOnlineStatus } from '@porto/shared/offline';

/** "Dati aggiornati alle HH:MM" while offline (m6b-plan §4.2), from the query's dataUpdatedAt. */
export function StaleSince({ updatedAt }: { updatedAt: number }) {
  const { t, i18n } = useT();
  const { online } = useOnlineStatus();
  if (online || !updatedAt) return null;
  const time = new Intl.DateTimeFormat(i18n.language, {
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(updatedAt));
  return (
    <p className="text-muted-foreground text-xs" role="status" data-testid="stale-since">
      {t('offline:staleSince', { time })}
    </p>
  );
}
