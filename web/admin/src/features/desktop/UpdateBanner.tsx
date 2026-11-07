import { DownloadIcon } from 'lucide-react';

import { useUpdateDesktopSettings } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { isExternalUrl, openExternal } from '@porto/shared/lib/download';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';

import { useDesktopAccess } from './lib';

/**
 * Update notice of the desktop window (m6a-plan §2.8, §4.4): only on loopback, only when the launcher found a
 * newer version (never when the check is off). Porto never downloads or installs it; the link opens the default
 * browser. Dismissal is stored per version in the desktop settings (owner).
 */
export function UpdateBanner() {
  const { t } = useT();
  const access = useDesktopAccess();
  const dismiss = useUpdateDesktopSettings();
  const update = access.loopback ? access.status?.update : null;
  if (!update || !isExternalUrl(update.url)) return null;
  return (
    <Alert data-testid="update-banner" className="mb-4">
      <DownloadIcon />
      <AlertTitle>{t('desktop:updates.available', { version: update.version })}</AlertTitle>
      <AlertDescription>
        <div className="flex flex-wrap gap-2 pt-1">
          <Button
            type="button"
            size="sm"
            onClick={() => void openExternal(update.url).catch(() => undefined)}
          >
            {t('desktop:updates.download')}
          </Button>
          {access.owner ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={dismiss.isPending}
              onClick={() => dismiss.mutate({ dismissed_update: update.version })}
            >
              {t('desktop:updates.dismiss')}
            </Button>
          ) : null}
        </div>
      </AlertDescription>
    </Alert>
  );
}
