// Building blocks of the rental fleet tabs (m11-plan §14.1): number and slot fields, the category checklist and
// the item picker (rental items only where the server requires the revenue class rental, D3).
import { type RentalCategory } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { CATEGORIES, SLOT_OPTIONS } from '../lib';
import type { ItemOption } from './useItemOptions';

const NONE = '__none__';

/** An integer input (digits only); empty is null. */
export function IntField({
  id,
  label,
  value,
  onChange,
}: {
  id: string;
  label: string;
  value: number | null;
  onChange: (v: number | null) => void;
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        inputMode="numeric"
        value={value === null ? '' : String(value)}
        onChange={(e) => {
          const d = e.target.value.replace(/\D/g, '');
          onChange(d === '' ? null : Number(d));
        }}
      />
    </Field>
  );
}

/** A select of slot boundaries (HH:MM). */
export function SlotSelect({
  id,
  label,
  value,
  onChange,
  min = 0,
  max = 48,
}: {
  id: string;
  label: string;
  value: number;
  onChange: (v: number) => void;
  min?: number;
  max?: number;
}) {
  return (
    <Field className="w-auto min-w-28">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
        <SelectTrigger id={id}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {SLOT_OPTIONS.filter((o) => o.value >= min && o.value <= max).map((o) => (
            <SelectItem key={o.value} value={String(o.value)}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

/** Checkboxes of the unit categories (none checked = every category). */
export function CategoryChecks({
  idPrefix,
  label,
  value,
  onChange,
}: {
  idPrefix: string;
  label: string;
  value: readonly RentalCategory[];
  onChange: (v: RentalCategory[]) => void;
}) {
  const { t } = useT();
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium">{label}</legend>
      <div className="flex flex-wrap gap-3">
        {CATEGORIES.map((c) => {
          const id = `${idPrefix}-${c}`;
          const on = value.includes(c);
          return (
            <div key={c} className="flex items-center gap-2">
              <Checkbox
                id={id}
                checked={on}
                onCheckedChange={(v) => onChange(v ? [...value, c] : value.filter((x) => x !== c))}
              />
              <Label htmlFor={id}>{t(`rentalFleet:enums.category.${c}`)}</Label>
            </div>
          );
        })}
      </div>
      <p className="text-muted-foreground text-xs">{t('rentalFleet:hints.allCategories')}</p>
    </fieldset>
  );
}

/** A select of items; `rentalOnly` keeps the items of the revenue class rental; `optional` adds "none". */
export function ItemSelect({
  id,
  label,
  items,
  value,
  onChange,
  rentalOnly = false,
  optional = false,
}: {
  id: string;
  label: string;
  items: readonly ItemOption[];
  value: string;
  onChange: (v: string) => void;
  rentalOnly?: boolean;
  optional?: boolean;
}) {
  const { t } = useT();
  const shown = rentalOnly ? items.filter((i) => i.rental || i.id === value) : items;
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value || NONE} onValueChange={(v) => onChange(v === NONE ? '' : v)}>
        <SelectTrigger id={id}>
          <SelectValue placeholder={t('rentalFleet:hints.chooseItem')} />
        </SelectTrigger>
        <SelectContent>
          {optional || !value ? (
            <SelectItem value={NONE}>{t('rentalFleet:hints.none')}</SelectItem>
          ) : null}
          {shown.map((i) => (
            <SelectItem key={i.id} value={i.id}>
              {i.code}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}
