import { Link } from '@tanstack/react-router';
import { DatabaseBackupIcon, KeyRoundIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useCreateDesktopBackup } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';

import { toastProblem } from '../../lib/toast';
import { useDesktopAccess } from './lib';

const UNAVAILABLE = ['publicPage', 'onlinePayments', 'paymentLinks', 'remoteSignature'] as const;
const STEPS = ['backup', 'secret', 'server', 'pair', 'stop'] as const;

/**
 * How to move from the desktop edition to an online instance (m6a-plan §4.3), readable by every role: what is
 * unavailable and why, and the steps (backup → secret key → `porto restore` on the server → pair the phones again
 * → stop using the desktop copy). The owner in the app window gets the backup and key actions as buttons.
 */
export function MoveOnlinePage() {
  const { t } = useT();
  const access = useDesktopAccess();
  const backup = useCreateDesktopBackup();

  return (
    <div data-testid="move-online-page" className="flex flex-col gap-6">
      <PageHeader title={t('desktop:online.title')} description={t('desktop:online.description')} />
      <Card>
        <CardHeader>
          <CardTitle>{t('desktop:online.unavailableTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
            {UNAVAILABLE.map((key) => (
              <li key={key}>{t(`desktop:localMode.${key}`)}</li>
            ))}
          </ul>
        </CardContent>
      </Card>
      <Alert>
        <AlertTitle>{t('desktop:online.whyTitle')}</AlertTitle>
        <AlertDescription>
          <p>{t('desktop:online.why')}</p>
          <p>{t('desktop:online.noSync')}</p>
        </AlertDescription>
      </Alert>
      <Card>
        <CardHeader>
          <CardTitle>{t('desktop:online.stepsTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="flex list-decimal flex-col gap-4 pl-5 text-sm">
            {STEPS.map((step) => (
              <li key={step} className="flex flex-col gap-2" data-testid={`online-step-${step}`}>
                <span>{t(`desktop:online.steps.${step}`)}</span>
                {step === 'server' ? (
                  <code className="bg-muted rounded-md p-2 font-mono text-xs break-all">
                    {t('desktop:online.restoreCommand')}
                  </code>
                ) : null}
                {step === 'backup' && access.admin ? (
                  <div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={backup.isPending}
                      onClick={() =>
                        backup.mutate(undefined, {
                          onSuccess: () => toast.success(t('desktop:backup.done')),
                          onError: (e) => toastProblem(t, e),
                        })
                      }
                    >
                      <DatabaseBackupIcon />
                      {t('desktop:backup.now')}
                    </Button>
                  </div>
                ) : null}
                {step === 'secret' && access.admin ? (
                  <div>
                    <Button asChild variant="outline" size="sm">
                      <Link to="/desktop">
                        <KeyRoundIcon />
                        {t('desktop:secret.reveal')}
                      </Link>
                    </Button>
                  </div>
                ) : null}
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>
    </div>
  );
}
