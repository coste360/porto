import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateContract,
  type OccupancySolution,
  type PlacementInput,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { CustomerField, type CustomerChoice } from '../components/CustomerField';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { ResourceField } from '../components/ResourceField';
import { VesselField } from '../components/VesselField';
import { vesselDims, type VesselValue } from '../lib/vessel';
import { contractBody, defaultEndsOn, type ContractDraft } from '../lib/contract';
import { addDays, isIsoDate } from '../lib/dates';
import { prefillTarget } from '../lib/prefill';
import { solutionCodes } from '../lib/search';
import { SolutionPicker } from '../search/SolutionPicker';

const route = getRouteApi('/auth/m/$marinaId/contracts/new');
const TERMS = ['annual', 'seasonal', 'monthly', 'period'] as const;

/** Mooring contract wizard: customer, vessel, term and period, placement (search or map), extra land resources, draft or active. */
export function ContractWizardPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const search = route.useSearch();
  const navigate = useNavigate();
  const create = useCreateContract(marinaId);
  const term0 = search.term ?? 'annual';
  const start0 = search.from ?? today;
  const [draft, setDraft] = useState<ContractDraft>({
    term: term0,
    customerId: null,
    vesselId: search.vessel ?? null,
    startsOn: start0,
    endsOn: search.to ?? defaultEndsOn(term0, start0),
    target: prefillTarget(search),
    extraLandIds: [],
    notes: '',
    activate: false,
    fitOverrideReason: null,
  });
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [vessel, setVessel] = useState<VesselValue>({
    kind: 'existing',
    id: search.vessel ?? null,
    label: '',
    dims: null,
  });
  const [picked, setPicked] = useState<OccupancySolution | null>(null);
  const [extras, setExtras] = useState<(PlacementInput | null)[]>([]);
  const [error, setError] = useState<string | null>(null);
  const set = (patch: Partial<ContractDraft>) => setDraft((d) => ({ ...d, ...patch }));

  const full: ContractDraft = {
    ...draft,
    customerId: customer?.id ?? null,
    vesselId: vessel.kind === 'existing' ? vessel.id : null,
    extraLandIds: extras.flatMap((e) => (e ? [e.resource_id] : [])),
  };
  const endsOn =
    draft.endsOn ?? (draft.startsOn ? defaultEndsOn(draft.term, draft.startsOn) : null);
  const searchRequest: Schemas['OccupancySearchRequest'] | null =
    full.vesselId && isIsoDate(draft.startsOn) && isIsoDate(endsOn) && draft.startsOn < endsOn
      ? {
          purpose: 'contract',
          term: draft.term,
          vessel_id: full.vesselId,
          arrival: draft.startsOn,
          departure: endsOn,
          channel: 'desk',
          resource_types: ['berth', 'dynamic_area'],
          ...(full.customerId ? { customer_id: full.customerId } : {}),
        }
      : null;

  const submit = (activate: boolean, override?: string) => {
    const built = contractBody({ ...full, activate, fitOverrideReason: override ?? null });
    if (!built.ok) {
      setError(built.error);
      return;
    }
    setError(null);
    create.mutate(built.body, {
      onSuccess: (c) =>
        void navigate({
          to: '/m/$marinaId/contracts/$contractId',
          params: { marinaId, contractId: c.id },
        }),
    });
  };

  return (
    <RequirePermission allowed={can('contracts.write')}>
      <PageHeader
        title={t('desk:contract.newTitle')}
        description={t('desk:contract.newDescription')}
      />
      <div className="flex flex-col gap-4" data-testid="desk-contract-wizard">
        <Card>
          <CardHeader>
            <CardTitle>{t('desk:contract.partiesStep')}</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="contract-customer">{t('desk:fields.customer')}</FieldLabel>
              <CustomerField
                marinaId={marinaId}
                id="contract-customer"
                value={customer}
                onChange={setCustomer}
              />
            </Field>
            <VesselField
              marinaId={marinaId}
              idPrefix="contract"
              value={vessel}
              onChange={setVessel}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('desk:contract.periodStep')}</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="contract-term">{t('desk:fields.term')}</FieldLabel>
              <Select
                value={draft.term}
                onValueChange={(v) => {
                  const term = v as ContractDraft['term'];
                  set({
                    term,
                    endsOn: draft.startsOn ? defaultEndsOn(term, draft.startsOn) : null,
                  });
                }}
              >
                <SelectTrigger id="contract-term" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TERMS.map((term) => (
                    <SelectItem key={term} value={term}>
                      {t(`occupancyEnums:contractTerm.${term}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="contract-starts">{t('desk:fields.startsOn')}</FieldLabel>
              <DatePicker
                id="contract-starts"
                value={draft.startsOn}
                onChange={(startsOn) =>
                  set({
                    startsOn,
                    endsOn: startsOn ? (defaultEndsOn(draft.term, startsOn) ?? draft.endsOn) : null,
                  })
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="contract-ends">{t('desk:fields.endsOn')}</FieldLabel>
              <DatePicker
                id="contract-ends"
                value={draft.endsOn}
                onChange={(e) => set({ endsOn: e })}
              />
              <FieldDescription>{t(`desk:contract.endsHint.${draft.term}`)}</FieldDescription>
            </Field>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('desk:contract.placementStep')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <SolutionPicker
              marinaId={marinaId}
              request={searchRequest}
              onPick={(s) => {
                const seg = s.segments[0];
                setPicked(s);
                if (seg)
                  set({
                    target: {
                      resource_type: seg.resource_type,
                      resource_id: seg.resource_id,
                      ...(seg.x_cm !== undefined ? { x_cm: seg.x_cm } : {}),
                    },
                  });
              }}
            />
            {picked ? (
              <p className="text-sm" data-testid="desk-contract-picked">
                {t('desk:transit.fromSearch', { codes: solutionCodes(picked) })}
                {picked.pricing
                  ? ` · ${formatCents(picked.pricing.total_cents, 'EUR', i18n.language)}`
                  : ''}
              </p>
            ) : null}
            <ResourceField
              marinaId={marinaId}
              idPrefix="contract"
              types={['berth', 'dynamic_area']}
              value={draft.target}
              onChange={(target) => {
                setPicked(null);
                set({ target });
              }}
              vessel={vesselDims(vessel)}
              date={draft.startsOn ?? today}
              from={draft.startsOn ?? today}
              to={endsOn ? addDays(endsOn, -1) : today}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('desk:contract.extrasStep')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {extras.map((e, i) => (
              <div key={i} className="flex items-end gap-2">
                <div className="flex-1">
                  <ResourceField
                    marinaId={marinaId}
                    idPrefix={`contract-extra-${i}`}
                    types={['land_resource']}
                    value={e}
                    onChange={(p) => setExtras(extras.map((x, j) => (j === i ? p : x)))}
                    date={draft.startsOn ?? today}
                    from={draft.startsOn ?? today}
                    to={endsOn ?? today}
                  />
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('common:actions.remove')}
                  onClick={() => setExtras(extras.filter((_, j) => j !== i))}
                >
                  <Trash2Icon />
                </Button>
              </div>
            ))}
            <div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setExtras([...extras, null])}
              >
                <PlusIcon />
                {t('desk:contract.addExtra')}
              </Button>
            </div>
          </CardContent>
        </Card>
        <Field>
          <FieldLabel htmlFor="contract-notes">{t('desk:fields.notes')}</FieldLabel>
          <Textarea
            id="contract-notes"
            value={draft.notes}
            maxLength={4000}
            onChange={(e) => set({ notes: e.target.value })}
          />
        </Field>
        {error ? <FieldError>{t(error)}</FieldError> : null}
        <DeskProblemAlert
          error={create.error}
          onAlternative={(s) => {
            create.reset();
            const seg = s.segments[0];
            setPicked(s);
            if (seg)
              set({ target: { resource_type: seg.resource_type, resource_id: seg.resource_id } });
          }}
          {...(can('occupancy.override_fit')
            ? { onOverride: (r: string) => submit(draft.activate, r) }
            : {})}
        />
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            variant="outline"
            disabled={create.isPending}
            onClick={() => {
              set({ activate: false });
              submit(false);
            }}
          >
            {t('desk:contract.saveDraft')}
          </Button>
          <Button
            disabled={create.isPending}
            onClick={() => {
              set({ activate: true });
              submit(true);
            }}
          >
            {t('desk:contract.activateNow')}
          </Button>
        </div>
      </div>
    </RequirePermission>
  );
}
