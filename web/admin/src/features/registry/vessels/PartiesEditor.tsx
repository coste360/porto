import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useDeferredValue, useState } from 'react';

import { useCustomers } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@porto/shared/ui/input-group';
import { ElementCombobox, type ComboboxOption } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { PARTY_ROLES } from '../lib/enums';
import { customerName, formatShare } from '../lib/format';
import { newRowKey, type PartyRow } from './parties';

export type PartiesEditorProps = {
  rows: PartyRow[];
  onChange: (rows: PartyRow[]) => void;
  /** Error keys by row key, plus `total` for the owner share sum. */
  errors: Record<string, string>;
  ownerTotal: number;
  disabled?: boolean;
};

/** Customers linked to a vessel with their role; owners carry a share in percent (stored in ‰). */
export function PartiesEditor({
  rows,
  onChange,
  errors,
  ownerTotal,
  disabled,
}: PartiesEditorProps) {
  const { t, i18n } = useT();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const { marinaId } = useCurrentMarina();
  const search = useCustomers(marinaId, { q: deferred });

  const found: ComboboxOption[] = (search.data?.pages.flatMap((p) => p.items) ?? [])
    .filter((c) => !c.anonymized_at && !c.merged_into_id)
    .map((c) => ({
      value: c.id,
      label: customerName(t, c),
      keywords: [c.fiscal_code ?? '', c.vat_number ?? ''],
    }));

  const patch = (key: string, change: Partial<PartyRow>) =>
    onChange(rows.map((r) => (r.key === key ? { ...r, ...change } : r)));

  return (
    <div className="flex flex-col gap-3">
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('vessels:parties.empty')}</p>
      ) : null}
      {rows.map((r, i) => {
        // The selected customer stays selectable even when the current search does not return it.
        const options =
          r.customer_id && !found.some((o) => o.value === r.customer_id)
            ? [{ value: r.customer_id, label: r.display_name }, ...found]
            : found;
        const error = errors[r.key];
        return (
          <div
            key={r.key}
            className="grid items-start gap-2 rounded-md border p-3 sm:grid-cols-12"
            data-testid="party-row"
          >
            <Field className="sm:col-span-6">
              <FieldLabel htmlFor={`party-customer-${r.key}`}>
                {t('vessels:parties.customer')}
              </FieldLabel>
              <ElementCombobox
                id={`party-customer-${r.key}`}
                aria-label={t('vessels:parties.customerN', { n: i + 1 })}
                options={options}
                value={r.customer_id || null}
                disabled={disabled}
                onSearch={setQ}
                placeholder={t('vessels:parties.pickCustomer')}
                searchPlaceholder={t('vessels:parties.searchCustomer')}
                emptyText={t('vessels:parties.noCustomers')}
                onChange={(id) =>
                  patch(r.key, {
                    customer_id: id ?? '',
                    display_name: options.find((o) => o.value === id)?.label ?? '',
                  })
                }
              />
            </Field>
            <Field className="sm:col-span-3">
              <FieldLabel htmlFor={`party-role-${r.key}`}>{t('vessels:parties.role')}</FieldLabel>
              <Select
                value={r.role}
                disabled={disabled}
                onValueChange={(role) =>
                  patch(r.key, {
                    role: role as PartyRow['role'],
                    share: role === 'owner' ? r.share : '',
                  })
                }
              >
                <SelectTrigger id={`party-role-${r.key}`} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PARTY_ROLES.map((role) => (
                    <SelectItem key={role} value={role}>
                      {t(`vessels:parties.roles.${role}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field className="sm:col-span-2">
              <FieldLabel htmlFor={`party-share-${r.key}`}>{t('vessels:parties.share')}</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id={`party-share-${r.key}`}
                  inputMode="decimal"
                  value={r.share}
                  disabled={disabled || r.role !== 'owner'}
                  aria-invalid={error?.includes('share') || undefined}
                  onChange={(e) => patch(r.key, { share: e.target.value })}
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>{t('vessels:parties.percent')}</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <div className="flex sm:col-span-1 sm:justify-end sm:pt-6">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                disabled={disabled}
                aria-label={t('vessels:parties.remove', { n: i + 1 })}
                onClick={() => onChange(rows.filter((x) => x.key !== r.key))}
              >
                <Trash2Icon />
              </Button>
            </div>
            {error ? <FieldError className="sm:col-span-12">{t(error)}</FieldError> : null}
          </div>
        );
      })}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled}
          onClick={() =>
            onChange([
              ...rows,
              {
                key: newRowKey(),
                customer_id: '',
                display_name: '',
                role: rows.some((r) => r.role === 'owner') ? 'contact' : 'owner',
                share: rows.some((r) => r.role === 'owner') ? '' : '100',
              },
            ])
          }
        >
          <PlusIcon />
          {t('vessels:parties.add')}
        </Button>
        <span className="text-muted-foreground text-sm" aria-live="polite">
          {t('vessels:parties.ownerTotal', { value: formatShare(ownerTotal, i18n.language) })}
        </span>
      </div>
      {errors.total ? <FieldError>{t(errors.total)}</FieldError> : null}
    </div>
  );
}
