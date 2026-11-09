import { PlusIcon, TrashIcon } from 'lucide-react';
import { Controller, useFieldArray, type Control } from 'react-hook-form';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import type { ProfileValues } from '../lib/profileForm';

const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const;

/** Weekly opening intervals (weekday 1 = Monday, "HH:MM"). */
export function HoursEditor({
  control,
  name,
  legend,
}: {
  control: Control<ProfileValues>;
  name: 'office_hours' | 'dock_hours';
  legend: string;
}) {
  const { t } = useT();
  const rows = useFieldArray({ control, name });
  return (
    <FieldSet>
      <FieldLegend>{legend}</FieldLegend>
      <ul className="flex flex-col gap-2" data-testid={`profile-${name}`}>
        {rows.fields.map((row, i) => (
          <li key={row.id} className="flex flex-wrap items-end gap-2">
            <Controller
              control={control}
              name={`${name}.${i}.weekday`}
              render={({ field }) => (
                <Select
                  value={String(field.value)}
                  onValueChange={(v) => field.onChange(Number(v))}
                >
                  <SelectTrigger className="w-40" aria-label={t('profile:hours.weekday')}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {WEEKDAYS.map((d) => (
                      <SelectItem key={d} value={String(d)}>
                        {t(`publicProfile:weekday.${d}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            <Controller
              control={control}
              name={`${name}.${i}.from`}
              render={({ field, fieldState }) => (
                <Input
                  type="time"
                  className="w-32"
                  aria-label={t('profile:hours.from')}
                  aria-invalid={fieldState.invalid}
                  {...field}
                />
              )}
            />
            <Controller
              control={control}
              name={`${name}.${i}.to`}
              render={({ field, fieldState }) => (
                <Input
                  type="time"
                  className="w-32"
                  aria-label={t('profile:hours.to')}
                  aria-invalid={fieldState.invalid}
                  {...field}
                />
              )}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('profile:hours.remove')}
              onClick={() => rows.remove(i)}
            >
              <TrashIcon />
            </Button>
          </li>
        ))}
      </ul>
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={rows.fields.length >= 21}
          onClick={() => rows.append({ weekday: 1, from: '08:00', to: '18:00' })}
        >
          <PlusIcon />
          {t('profile:hours.add')}
        </Button>
      </div>
    </FieldSet>
  );
}
