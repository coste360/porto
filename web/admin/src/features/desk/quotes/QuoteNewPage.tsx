import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { useT } from '@porto/shared/i18n';
import { PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { defaultEndsOn } from '../lib/contract';
import { addDays } from '../lib/dates';
import { prefillTarget } from '../lib/prefill';
import { emptyOption } from '../lib/quote';
import { QuoteEditor } from './QuoteEditor';

const route = getRouteApi('/auth/m/$marinaId/quotes/new');

export function QuoteNewPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const search = route.useSearch();
  const navigate = useNavigate();
  const startsOn = search.from ?? today;
  const kind = search.term ? 'mooring' : 'transit';
  const term = search.term ?? 'annual';
  return (
    <RequirePermission allowed={can('contracts.write')}>
      <PageHeader title={t('desk:quote.newTitle')} />
      <QuoteEditor
        marinaId={marinaId}
        initial={{
          kind,
          term,
          customerId: search.customer ?? null,
          vesselId: search.vessel ?? null,
          startsOn,
          endsOn:
            search.to ??
            (kind === 'mooring' ? defaultEndsOn(term, startsOn) : null) ??
            addDays(startsOn, 1),
          validUntil: null,
          notes: '',
          options: [emptyOption(prefillTarget(search))],
        }}
        onCancel={() => void navigate({ to: '/m/$marinaId/quotes', params: { marinaId } })}
        onSaved={(q) =>
          void navigate({ to: '/m/$marinaId/quotes/$quoteId', params: { marinaId, quoteId: q.id } })
        }
      />
    </RequirePermission>
  );
}
