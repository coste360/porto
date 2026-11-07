import { useState } from 'react';

import { usePublicUnsubscribe } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';

import { Done, type LinkRef } from './common';

/** Unsubscribe from promotional emails (spec 28): one confirmation click records the refused marketing consent. */
export function UnsubscribePage({
  linkRef,
  subscribed,
}: {
  linkRef: LinkRef;
  subscribed: boolean;
}) {
  const { t } = useT();
  const unsubscribe = usePublicUnsubscribe(linkRef);
  const [done, setDone] = useState(!subscribed);
  if (done)
    return (
      <Done
        title={t('customerLinks:unsubscribePage.done')}
        description={t('customerLinks:unsubscribePage.doneHint')}
      />
    );
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">{t('customerLinks:unsubscribePage.intro')}</p>
      <ProblemAlert error={unsubscribe.error} />
      <Button
        size="lg"
        disabled={unsubscribe.isPending}
        onClick={() => unsubscribe.mutate(undefined, { onSuccess: () => setDone(true) })}
      >
        {t('customerLinks:unsubscribePage.confirm')}
      </Button>
    </div>
  );
}
