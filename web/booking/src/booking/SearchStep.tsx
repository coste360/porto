import { SearchIcon } from 'lucide-react';
import { useForm, useWatch } from 'react-hook-form';
import { z } from 'zod';

import type { BookingConfig, HullType } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField } from '@porto/shared/ui/patterns/FormField';
import { LengthInput } from '@porto/shared/ui/patterns/LengthInput';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { zodFormResolver } from '../common/formResolver';
import { cmToFeetText, VESSEL_LIMITS_CM } from '../common/units';
import { HULL_TYPES } from './hullTypes';
import { addDays, nightsBetween, todayIso, type SearchInput } from './state';

const ISO = /^\d{4}-\d{2}-\d{2}$/;

const length = (max: number, required: boolean) => {
  const base = z
    .number()
    .int()
    .min(1, 'checkout:validation.length')
    .max(max, 'checkout:validation.length')
    .nullable();
  return required ? base.refine((v) => v !== null, 'checkout:validation.length') : base;
};

function makeSchema(rules: BookingConfig['rules'], today: string) {
  return z
    .object({
      arrival: z.string().regex(ISO, 'common:validation.required'),
      departure: z.string().regex(ISO, 'common:validation.required'),
      loa_cm: length(VESSEL_LIMITS_CM.loa, true),
      beam_cm: length(VESSEL_LIMITS_CM.beam, true),
      draft_cm: length(VESSEL_LIMITS_CM.draft, false),
      hull_type: z.enum(HULL_TYPES as [HullType, ...HullType[]]).optional(),
    })
    .superRefine((v, ctx) => {
      if (ISO.test(v.arrival) && v.arrival < today) {
        ctx.addIssue({
          code: 'custom',
          path: ['arrival'],
          message: 'checkout:validation.arrivalPast',
        });
      }
      if (!ISO.test(v.arrival) || !ISO.test(v.departure)) return;
      const nights = nightsBetween(v.arrival, v.departure);
      if (nights <= 0) {
        ctx.addIssue({
          code: 'custom',
          path: ['departure'],
          message: 'checkout:validation.departureAfter',
        });
      } else if (nights < rules.min_nights) {
        ctx.addIssue({
          code: 'custom',
          path: ['departure'],
          message: 'checkout:validation.minNights',
        });
      } else if (nights > rules.max_nights) {
        ctx.addIssue({
          code: 'custom',
          path: ['departure'],
          message: 'checkout:validation.maxNights',
        });
      }
    });
}

type FormIn = {
  arrival: string;
  departure: string;
  loa_cm: number | null;
  beam_cm: number | null;
  draft_cm: number | null;
  hull_type?: HullType | undefined;
};

/** Step 1 "Quando e barca": native date inputs and the vessel in metres (feet shown when enabled). */
export function SearchStep({
  config,
  initial,
  onSearch,
}: {
  config: BookingConfig;
  initial: SearchInput | null;
  onSearch: (s: SearchInput) => void;
}) {
  const { t, i18n } = useT();
  const today = todayIso();
  const maxDate = addDays(today, config.rules.max_advance_days);
  const form = useForm<FormIn>({
    resolver: zodFormResolver(makeSchema(config.rules, today)) as never,
    defaultValues: {
      arrival: initial?.arrival ?? '',
      departure: initial?.departure ?? '',
      loa_cm: initial?.loa_cm ?? null,
      beam_cm: initial?.beam_cm ?? null,
      draft_cm: initial?.draft_cm ?? null,
      hull_type: initial?.hull_type,
    },
  });
  const [arrival, loa, beam, draft] = useWatch({
    control: form.control,
    name: ['arrival', 'loa_cm', 'beam_cm', 'draft_cm'],
  });
  const feet = (cm: number | null) =>
    config.show_feet && cm
      ? t('checkout:search.feet', { value: cmToFeetText(cm, i18n.language) })
      : undefined;

  const submit = form.handleSubmit((v) => {
    onSearch({
      arrival: v.arrival,
      departure: v.departure,
      loa_cm: v.loa_cm as number,
      beam_cm: v.beam_cm as number,
      ...(v.draft_cm ? { draft_cm: v.draft_cm } : {}),
      ...(v.hull_type ? { hull_type: v.hull_type } : {}),
    });
  });

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4" data-testid="booking-search">
      <FieldGroup className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <FormField
          control={form.control}
          name="arrival"
          label={t('checkout:search.arrival')}
          render={(f) => (
            <Input
              {...f}
              type="date"
              min={today}
              max={maxDate}
              value={f.value}
              autoComplete="off"
            />
          )}
        />
        <FormField
          control={form.control}
          name="departure"
          label={t('checkout:search.departure')}
          description={t('checkout:search.nightsHint', {
            min: config.rules.min_nights,
            max: config.rules.max_nights,
          })}
          render={(f) => (
            <Input
              {...f}
              type="date"
              min={arrival ? addDays(arrival, 1) : today}
              value={f.value}
              autoComplete="off"
            />
          )}
        />
        <FormField
          control={form.control}
          name="loa_cm"
          label={t('checkout:search.loa')}
          description={feet(loa)}
          render={(f) => (
            <LengthInput
              id={f.id}
              name={f.name}
              aria-invalid={f['aria-invalid']}
              value={f.value}
              onChange={f.onChange}
              onBlur={f.onBlur}
              locale={i18n.language}
              unit={t('checkout:search.metres')}
            />
          )}
        />
        <FormField
          control={form.control}
          name="beam_cm"
          label={t('checkout:search.beam')}
          description={feet(beam)}
          render={(f) => (
            <LengthInput
              id={f.id}
              name={f.name}
              aria-invalid={f['aria-invalid']}
              value={f.value}
              onChange={f.onChange}
              onBlur={f.onBlur}
              locale={i18n.language}
              unit={t('checkout:search.metres')}
            />
          )}
        />
        <FormField
          control={form.control}
          name="draft_cm"
          label={t('checkout:search.draft')}
          description={feet(draft) ?? t('checkout:search.optional')}
          render={(f) => (
            <LengthInput
              id={f.id}
              name={f.name}
              aria-invalid={f['aria-invalid']}
              value={f.value}
              onChange={f.onChange}
              onBlur={f.onBlur}
              locale={i18n.language}
              unit={t('checkout:search.metres')}
            />
          )}
        />
        <FormField
          control={form.control}
          name="hull_type"
          label={t('checkout:search.hullType')}
          render={(f) => (
            <Select value={f.value ?? ''} onValueChange={(v) => f.onChange(v as HullType)}>
              <SelectTrigger id={f.id} aria-invalid={f['aria-invalid']} className="w-full">
                <SelectValue placeholder={t('checkout:search.hullTypePlaceholder')} />
              </SelectTrigger>
              <SelectContent>
                {HULL_TYPES.map((h) => (
                  <SelectItem key={h} value={h}>
                    {t(`checkout:hullTypes.${h}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
      </FieldGroup>
      <Button type="submit" size="lg" className="w-full sm:w-auto sm:self-end">
        <SearchIcon />
        {t('checkout:search.submit')}
      </Button>
    </form>
  );
}
