import { Link } from '@tanstack/react-router';
import { CompassIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { EmptyState } from '@porto/shared/ui/patterns';

export function NotFoundPage() {
  const { t } = useT();
  return (
    <main className="mx-auto max-w-xl p-6">
      <EmptyState
        icon={<CompassIcon />}
        title={t('common:page.notFound')}
        action={
          <Button asChild variant="outline">
            <Link to="/">{t('common:page.goHome')}</Link>
          </Button>
        }
      />
    </main>
  );
}
