import { Link } from '@tanstack/react-router';
import { CloudOffIcon } from 'lucide-react';
import { useState } from 'react';

import { useLocalMode } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@porto/shared/ui/card';

const DISMISS_KEY = 'porto.desktop.localModeCard';

function readDismissed(): boolean {
  try {
    return window.localStorage.getItem(DISMISS_KEY) === '1';
  } catch {
    return false;
  }
}

/**
 * Dashboard card of the desktop edition (m6a-plan §4.3): explains once what local mode cannot do and links the
 * move-online guide. Dismissal is a per-browser convenience (local storage); the guide stays reachable.
 */
export function LocalModeCard() {
  const { t } = useT();
  const localMode = useLocalMode();
  const [dismissed, setDismissed] = useState(readDismissed);
  if (!localMode || dismissed) return null;
  const dismiss = () => {
    setDismissed(true);
    try {
      window.localStorage.setItem(DISMISS_KEY, '1');
    } catch {
      // Storage unavailable: the card comes back on the next visit.
    }
  };
  return (
    <Card data-testid="dashboard-local-mode-card">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CloudOffIcon />
          {t('desktop:localMode.cardTitle')}
        </CardTitle>
        <CardDescription>{t('desktop:localMode.cardDescription')}</CardDescription>
      </CardHeader>
      <CardFooter className="flex flex-wrap gap-2">
        <Button asChild variant="outline">
          <Link to="/desktop/online">{t('desktop:localMode.learnMore')}</Link>
        </Button>
        <Button type="button" variant="ghost" onClick={dismiss}>
          {t('desktop:localMode.dismiss')}
        </Button>
      </CardFooter>
    </Card>
  );
}
