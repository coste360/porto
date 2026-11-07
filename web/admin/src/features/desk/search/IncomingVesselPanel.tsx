import { SearchIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateInquiry,
  useOccupancySearch,
  type OccupancySolution,
  type PlacementInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';
import { useNavigate } from '@tanstack/react-router';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { HullSelect, VesselField } from '../components/VesselField';
import { NO_VESSEL, type VesselValue } from '../lib/vessel';
import { daysBetween, isIsoDate, lastNightOf } from '../lib/dates';
import { contractSearchFromSolution } from '../lib/prefill';
import { RESOURCE_TYPES, emptySearchForm, toSearchRequest, type SearchForm } from '../lib/search';
import { NewTransitDialog } from '../transits/NewTransitDialog';
import { NewQuoteDialog } from '../quotes/NewQuoteDialog';
import { ReleaseDialog } from '../dialogs/ReleaseDialog';
import type { DeskPrefill } from '../public';
import { SolutionList } from './SolutionList';

const TERMS = ['annual', 'seasonal', 'monthly', 'period'] as const;

/** "Imbarcazione in ingresso" (spec 9.6): search form, results synchronised with the map, desk actions. */
export function IncomingVesselPanel({
  marinaId,
  date,
  onSolutions,
  onFocusSolution,
  onPickOnMap: _onPickOnMap,
}: {
  marinaId: string;
  date: string;
  onSolutions: (s: OccupancySolution[]) => void;
  onFocusSolution?: (s: OccupancySolution | null) => void;
  onPickOnMap?: () => Promise<PlacementInput | null>;
}) {
  const { t, i18n } = useT();
  const { can } = useCurrentMarina();
  const navigate = useNavigate();
  const [form, setForm] = useState<SearchForm>(() => emptySearchForm(date));
  const [vessel, setVessel] = useState<VesselValue>(NO_VESSEL);
  const [byDims, setByDims] = useState(true);
  const [formError, setFormError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [dialog, setDialog] = useState<
    | { kind: 'transit' | 'quote'; prefill: DeskPrefill }
    | { kind: 'release'; allocationId: string; then: DeskPrefill }
    | null
  >(null);
  const [saved, setSaved] = useState(false);
  const search = useOccupancySearch(marinaId);
  const inquiry = useCreateInquiry(marinaId);
  const set = (patch: Partial<SearchForm>) => setForm((f) => ({ ...f, ...patch }));

  const run = () => {
    const f: SearchForm = {
      ...form,
      vesselId: byDims ? null : vessel.kind === 'existing' ? vessel.id : null,
    };
    const built = toSearchRequest(f);
    if (!built.ok) {
      setFormError(built.error);
      return;
    }
    setFormError(null);
    setSaved(false);
    search.mutate(built.request, { onSuccess: (r) => onSolutions(r.solutions) });
  };

  const prefillOf = (s: OccupancySolution): DeskPrefill => ({
    solution: s,
    ...(form.arrival ? { arrival: form.arrival } : {}),
    ...(form.departure ? { departure: form.departure } : {}),
    ...(!byDims && vessel.kind === 'existing' && vessel.id ? { vesselId: vessel.id } : {}),
  });

  const open = (kind: 'transit' | 'quote', s: OccupancySolution) => {
    setSelected(s.key);
    if (s.requires_release) {
      setDialog({
        kind: 'release',
        allocationId: s.requires_release.allocation_id,
        then: prefillOf(s),
      });
      return;
    }
    setDialog({ kind, prefill: prefillOf(s) });
  };

  const saveInquiry = () => {
    if (!isIsoDate(form.arrival) || !isIsoDate(form.departure)) return;
    inquiry.mutate(
      {
        purpose: form.purpose,
        source: 'desk',
        channel: 'desk',
        arrival: form.arrival,
        departure: form.departure,
        outcome: 'declined_no_space',
        ...(form.purpose === 'contract' ? { term: form.term } : {}),
        ...(!byDims && vessel.kind === 'existing' && vessel.id
          ? { vessel_id: vessel.id }
          : {
              ...(form.loaCm ? { loa_cm: form.loaCm } : {}),
              ...(form.beamCm ? { beam_cm: form.beamCm } : {}),
              ...(form.draftCm ? { draft_cm: form.draftCm } : {}),
            }),
      },
      { onSuccess: () => setSaved(true) },
    );
  };

  const nights =
    isIsoDate(form.arrival) && isIsoDate(form.departure)
      ? daysBetween(form.arrival, form.departure)
      : 0;
  const solutions = search.data?.solutions ?? [];

  return (
    <section
      className="flex flex-col gap-4"
      aria-labelledby="incoming-title"
      data-testid="desk-incoming"
    >
      <h2 id="incoming-title" className="text-lg font-semibold">
        {t('desk:search.title')}
      </h2>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          run();
        }}
      >
        <ToggleGroup
          type="single"
          variant="outline"
          value={form.purpose}
          onValueChange={(v) => v && set({ purpose: v as SearchForm['purpose'] })}
          aria-label={t('desk:search.purpose')}
        >
          <ToggleGroupItem value="transit">{t('desk:search.purposeTransit')}</ToggleGroupItem>
          <ToggleGroupItem value="contract">{t('desk:search.purposeContract')}</ToggleGroupItem>
        </ToggleGroup>
        {form.purpose === 'contract' ? (
          <Field>
            <FieldLabel htmlFor="search-term">{t('desk:fields.term')}</FieldLabel>
            <Select value={form.term} onValueChange={(v) => set({ term: v as SearchForm['term'] })}>
              <SelectTrigger id="search-term" className="w-full">
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
        ) : null}
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">{t('desk:search.resourceTypes')}</legend>
          <div className="flex flex-wrap gap-3">
            {RESOURCE_TYPES.map((rt) => (
              <Field key={rt} orientation="horizontal" className="w-auto">
                <Checkbox
                  id={`search-rt-${rt}`}
                  checked={form.resourceTypes.includes(rt)}
                  onCheckedChange={(c) =>
                    set({
                      resourceTypes:
                        c === true
                          ? [...form.resourceTypes, rt]
                          : form.resourceTypes.filter((x) => x !== rt),
                    })
                  }
                />
                <FieldLabel htmlFor={`search-rt-${rt}`}>
                  {t(`occupancyEnums:resourceType.${rt}`)}
                </FieldLabel>
              </Field>
            ))}
          </div>
        </fieldset>
        <ToggleGroup
          type="single"
          variant="outline"
          value={byDims ? 'dims' : 'vessel'}
          onValueChange={(v) => v && setByDims(v === 'dims')}
          aria-label={t('desk:search.vesselBy')}
        >
          <ToggleGroupItem value="dims">{t('desk:search.byDimensions')}</ToggleGroupItem>
          <ToggleGroupItem value="vessel">{t('desk:search.byVessel')}</ToggleGroupItem>
        </ToggleGroup>
        {byDims ? (
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="search-loa">{t('desk:fields.loa')}</FieldLabel>
              <LengthInput
                id="search-loa"
                locale={i18n.language}
                value={form.loaCm}
                onChange={(loaCm) => set({ loaCm })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="search-beam">{t('desk:fields.beam')}</FieldLabel>
              <LengthInput
                id="search-beam"
                locale={i18n.language}
                value={form.beamCm}
                onChange={(beamCm) => set({ beamCm })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="search-draft">{t('desk:fields.draft')}</FieldLabel>
              <LengthInput
                id="search-draft"
                locale={i18n.language}
                value={form.draftCm}
                onChange={(draftCm) => set({ draftCm })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="search-hull">{t('desk:fields.hullType')}</FieldLabel>
              <HullSelect
                id="search-hull"
                value={form.hullType}
                onChange={(hullType) => set({ hullType })}
              />
            </Field>
          </div>
        ) : (
          <VesselField marinaId={marinaId} idPrefix="search" value={vessel} onChange={setVessel} />
        )}
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="search-arrival">{t('desk:fields.arrival')}</FieldLabel>
            <DatePicker
              id="search-arrival"
              value={form.arrival}
              onChange={(arrival) => set({ arrival })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="search-departure">{t('desk:fields.departure')}</FieldLabel>
            <DatePicker
              id="search-departure"
              value={form.departure}
              onChange={(departure) => set({ departure })}
            />
          </Field>
        </div>
        <p className="text-muted-foreground text-sm" data-testid="desk-search-nights">
          {t('desk:search.nights', { count: Math.max(0, nights) })}
        </p>
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="search-power">{t('desk:search.power')}</FieldLabel>
            <Select
              value={form.powerA === null ? 'none' : String(form.powerA)}
              onValueChange={(v) => set({ powerA: v === 'none' ? null : Number(v) })}
            >
              <SelectTrigger id="search-power" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">{t('desk:search.powerAny')}</SelectItem>
                {[16, 32, 63, 125].map((a) => (
                  <SelectItem key={a} value={String(a)}>
                    {t('desk:search.powerAmps', { amps: a })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="search-segments">{t('desk:search.maxSegments')}</FieldLabel>
            <Select
              value={String(form.maxSegments)}
              onValueChange={(v) => set({ maxSegments: Number(v) })}
            >
              <SelectTrigger id="search-segments" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[0, 1, 2].map((n) => (
                  <SelectItem key={n} value={String(n)}>
                    {t('desk:search.segments', { count: n })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
        <Field orientation="horizontal">
          <Checkbox
            id="search-water"
            checked={form.water}
            onCheckedChange={(c) => set({ water: c === true })}
          />
          <FieldLabel htmlFor="search-water">{t('desk:search.water')}</FieldLabel>
        </Field>
        <Field orientation="horizontal">
          <Checkbox
            id="search-out"
            checked={form.considerOut}
            onCheckedChange={(c) => set({ considerOut: c === true })}
          />
          <FieldLabel htmlFor="search-out">{t('desk:search.considerOut')}</FieldLabel>
        </Field>
        {formError ? <FieldError>{t(formError)}</FieldError> : null}
        <Button type="submit" disabled={search.isPending}>
          <SearchIcon />
          {t('desk:search.run')}
        </Button>
      </form>
      <DeskProblemAlert error={search.error} />
      {search.isSuccess ? (
        solutions.length === 0 ? (
          <div className="flex flex-col gap-2">
            <p className="text-muted-foreground text-sm">{t('desk:search.empty')}</p>
            {can('transits.desk') ? (
              <Button variant="outline" onClick={saveInquiry} disabled={inquiry.isPending || saved}>
                {saved ? t('desk:search.inquirySaved') : t('desk:search.saveInquiry')}
              </Button>
            ) : null}
            <DeskProblemAlert error={inquiry.error} />
          </div>
        ) : (
          <SolutionList
            solutions={solutions}
            selectedKey={selected}
            {...(onFocusSolution ? { onFocusSolution } : {})}
            actions={(s) => (
              <>
                {form.purpose === 'transit' && can('transits.desk') ? (
                  <Button size="sm" onClick={() => open('transit', s)}>
                    {t('desk:search.newTransit')}
                  </Button>
                ) : null}
                {can('contracts.write') ? (
                  <Button size="sm" variant="outline" onClick={() => open('quote', s)}>
                    {t('desk:search.newQuote')}
                  </Button>
                ) : null}
                {form.purpose === 'contract' && can('contracts.write') ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() =>
                      void navigate({
                        to: '/m/$marinaId/contracts/new',
                        params: { marinaId },
                        search: contractSearchFromSolution(s, {
                          term: form.term,
                          ...(!byDims && vessel.kind === 'existing' && vessel.id
                            ? { vesselId: vessel.id }
                            : {}),
                        }),
                      })
                    }
                  >
                    {t('desk:search.newContract')}
                  </Button>
                ) : null}
              </>
            )}
          />
        )
      ) : null}
      <NewTransitDialog
        marinaId={marinaId}
        open={dialog?.kind === 'transit'}
        onOpenChange={(o) => !o && setDialog(null)}
        {...(dialog?.kind === 'transit' ? { prefill: dialog.prefill } : {})}
      />
      <NewQuoteDialog
        marinaId={marinaId}
        open={dialog?.kind === 'quote'}
        onOpenChange={(o) => !o && setDialog(null)}
        {...(dialog?.kind === 'quote' ? { prefill: dialog.prefill } : {})}
      />
      {dialog?.kind === 'release' ? (
        <ReleaseDialog
          marinaId={marinaId}
          open
          allocationId={dialog.allocationId}
          defaultFrom={form.arrival ?? date}
          {...(form.departure ? { defaultTo: lastNightOf(form.departure) } : {})}
          onOpenChange={(o) => !o && setDialog(null)}
          onReleased={() => setDialog({ kind: 'transit', prefill: dialog.then })}
        />
      ) : null}
    </section>
  );
}
