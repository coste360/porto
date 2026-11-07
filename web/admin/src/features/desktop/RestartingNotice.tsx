import { RefreshCwIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';

/**
 * Shown after a restore was accepted (202): the server restarts on the restored database and sessions are not
 * in backups, so everybody signs in again. A full reload drops every cached query of the old database.
 */
export function RestartingNotice() {
  const { t } = useT();
  return (
    <Alert data-testid="desktop-restarting">
      <RefreshCwIcon />
      <AlertTitle>{t('desktop:restore.title')}</AlertTitle>
      <AlertDescription>
        <p>{t('desktop:restore.description')}</p>
        <Button
          type="button"
          className="mt-2"
          onClick={() => window.location.assign('/admin/login')}
        >
          {t('desktop:restore.login')}
        </Button>
      </AlertDescription>
    </Alert>
  );
}
