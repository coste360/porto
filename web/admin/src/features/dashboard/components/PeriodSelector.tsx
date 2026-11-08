import { useT } from '@porto/shared/i18n';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { isPreset, validPeriod, type Period, type PeriodPreset } from '../lib/period';

export type PeriodSelectorProps = {
  presets: readonly PeriodPreset[];
  preset: PeriodPreset;
  period: Partial<Period>;
  onPreset: (preset: PeriodPreset) => void;
  /** Custom dates (only with the custom preset). */
  onPeriod: (period: Partial<Period>) => void;
  idPrefix: string;
};

/** Period presets of the dashboard and comparison (m7-plan §4), with date pickers for a custom period. */
export function PeriodSelector({
  presets,
  preset,
  period,
  onPreset,
  onPeriod,
  idPrefix,
}: PeriodSelectorProps) {
  const { t } = useT();
  const invalid = preset === 'custom' && !validPeriod(period);
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-end">
      <Field className="sm:max-w-56">
        <FieldLabel htmlFor={`${idPrefix}-preset`}>{t('dashboard:period')}</FieldLabel>
        <Select value={preset} onValueChange={(v) => isPreset(v) && onPreset(v)}>
          <SelectTrigger id={`${idPrefix}-preset`} className="min-h-11 w-full sm:min-h-9">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {presets.map((p) => (
              <SelectItem key={p} value={p}>
                {t(`dashboard:periods.${p}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      {preset === 'custom' ? (
        <>
          <Field className="sm:max-w-44" data-invalid={invalid || undefined}>
            <FieldLabel htmlFor={`${idPrefix}-from`}>{t('dashboard:from')}</FieldLabel>
            <DatePicker
              id={`${idPrefix}-from`}
              value={period.from ?? null}
              onChange={(from) => onPeriod({ ...period, from: from ?? undefined })}
              aria-invalid={invalid}
            />
          </Field>
          <Field className="sm:max-w-44" data-invalid={invalid || undefined}>
            <FieldLabel htmlFor={`${idPrefix}-to`}>{t('dashboard:to')}</FieldLabel>
            <DatePicker
              id={`${idPrefix}-to`}
              value={period.to ?? null}
              onChange={(to) => onPeriod({ ...period, to: to ?? undefined })}
              aria-invalid={invalid}
            />
          </Field>
          {invalid ? (
            <FieldError className="sm:basis-full">{t('dashboard:invalidPeriod')}</FieldError>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
