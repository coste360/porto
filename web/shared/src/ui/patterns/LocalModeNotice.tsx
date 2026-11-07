import { CloudOffIcon } from 'lucide-react';
import type { ReactNode } from 'react';

import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';

import { useT } from '../../i18n/useT';

/** Online features that the desktop edition (local mode) cannot offer (m6a-plan §4.3, D23). */
export type LocalModeFeature = 'publicPage' | 'onlinePayments' | 'paymentLinks' | 'remoteSignature';

export type LocalModeNoticeProps = {
  /** `meta.local_mode`: nothing is rendered when false. */
  localMode: boolean;
  feature: LocalModeFeature;
  /** The "how to move online" link (a router link in the SPA). */
  action?: ReactNode;
  className?: string;
};

/**
 * Explains, in the desktop edition, that an online feature is unavailable because the instance is not reachable
 * from the internet. The feature stays in the API; this only sets expectations.
 */
export function LocalModeNotice({ localMode, feature, action, className }: LocalModeNoticeProps) {
  const { t } = useT();
  if (!localMode) return null;
  return (
    <Alert data-testid="local-mode-notice" data-feature={feature} className={className}>
      <CloudOffIcon />
      <AlertTitle>{t('desktop:localMode.title')}</AlertTitle>
      <AlertDescription>
        <p>{t(`desktop:localMode.${feature}`)}</p>
        {action ? <div className="pt-1">{action}</div> : null}
      </AlertDescription>
    </Alert>
  );
}
