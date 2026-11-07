import { Link } from '@tanstack/react-router';

import { useLocalMode } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { LocalModeNotice, type LocalModeFeature } from '@porto/shared/ui/patterns/LocalModeNotice';

/**
 * One-line insert for the pages whose online part is unavailable in local mode (m6a-plan §4.3): renders the
 * shared notice with a link to the move-online guide, or nothing outside the desktop edition.
 */
export function OnlineFeatureNotice({ feature }: { feature: LocalModeFeature }) {
  const { t } = useT();
  const localMode = useLocalMode();
  return (
    <LocalModeNotice
      localMode={localMode}
      feature={feature}
      action={
        <Link
          to="/desktop/online"
          className="text-primary font-medium underline underline-offset-4"
        >
          {t('desktop:localMode.learnMore')}
        </Link>
      }
    />
  );
}
