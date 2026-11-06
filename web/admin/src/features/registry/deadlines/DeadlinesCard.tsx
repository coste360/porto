import { Link } from '@tanstack/react-router';
import { CalendarClockIcon } from 'lucide-react';

import { useDeadlineCount } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@porto/shared/ui/card';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { useCurrentMarina } from '../../../auth/hooks';
import { DEADLINE_URGENCIES } from '../lib/enums';
import { UrgencyBadge } from './DeadlineTable';

/** Dashboard card: open deadlines counted by urgency, linking to the deadlines page. */
export function DeadlinesCard() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <CalendarClockIcon className="size-4" />
          {t('deadlines:dashboard.title')}
        </CardTitle>
        <CardDescription>{t('deadlines:dashboard.description')}</CardDescription>
        <CardAction>
          <Link
            to="/m/$marinaId/deadlines"
            params={{ marinaId }}
            className="text-sm underline-offset-4 hover:underline"
          >
            {t('deadlines:dashboard.viewAll')}
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-3 gap-4">
          {DEADLINE_URGENCIES.map((u) => (
            <UrgencyCount key={u} marinaId={marinaId} urgency={u} />
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function UrgencyCount({
  marinaId,
  urgency,
}: {
  marinaId: string;
  urgency: (typeof DEADLINE_URGENCIES)[number];
}) {
  const { t } = useT();
  const count = useDeadlineCount(marinaId, urgency);
  return (
    <div className="flex flex-col gap-1">
      <dt>
        <UrgencyBadge urgency={urgency} />
      </dt>
      <dd className="text-2xl font-semibold tabular-nums" data-testid={`deadlines-${urgency}`}>
        {count.isPending ? (
          <Skeleton className="h-8 w-12" />
        ) : count.isError ? (
          '–'
        ) : count.data.more ? (
          t('deadlines:dashboard.more', { count: count.data.count })
        ) : (
          count.data.count
        )}
      </dd>
    </div>
  );
}
