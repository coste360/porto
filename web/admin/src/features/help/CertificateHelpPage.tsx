import { DownloadIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';

import { CA_DOWNLOAD, CA_PROFILE, fingerprintFromSearch } from './certificate';

/**
 * Public help page: install the marina's local CA on Android and iOS (m6b-plan §8.5) so that the service worker,
 * and with it the offline start, works on the desktop's local network.
 */
export function CertificateHelpPage() {
  const { t } = useT();
  const fingerprint =
    typeof window === 'undefined' ? null : fingerprintFromSearch(window.location.search);
  return (
    <div data-testid="certificate-help-page" className="mx-auto flex max-w-xl flex-col gap-4 p-4">
      <h1 className="text-lg font-semibold">{t('offline:certificate.title')}</h1>
      <p className="text-muted-foreground">{t('offline:certificate.intro')}</p>
      <Card>
        <CardHeader>
          <CardTitle>{t('offline:certificate.download')}</CardTitle>
          <CardDescription>{t('offline:certificate.android')}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild className="min-h-11">
            <a href={CA_DOWNLOAD} download data-testid="ca-download">
              <DownloadIcon aria-hidden />
              {t('offline:certificate.download')}
            </a>
          </Button>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('offline:certificate.downloadProfile')}</CardTitle>
          <CardDescription>{t('offline:certificate.ios')}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild variant="outline" className="min-h-11">
            <a href={CA_PROFILE} download data-testid="ca-profile">
              <DownloadIcon aria-hidden />
              {t('offline:certificate.downloadProfile')}
            </a>
          </Button>
        </CardContent>
      </Card>
      {fingerprint ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('offline:certificate.fingerprint')}</CardTitle>
          </CardHeader>
          <CardContent>
            <code data-testid="ca-fingerprint" className="font-mono text-sm break-all">
              {fingerprint}
            </code>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
