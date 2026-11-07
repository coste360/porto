import { useDeferredValue, useState } from 'react';

import { useCustomers } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { ElementCombobox } from '@porto/shared/ui/patterns';

export type CustomerChoice = { id: string; label: string } | null;

/** Customer combobox with server-side search (customers.read). */
export function CustomerField({
  marinaId,
  id,
  value,
  onChange,
  disabled,
}: {
  marinaId: string;
  id: string;
  value: CustomerChoice;
  onChange: (c: CustomerChoice) => void;
  disabled?: boolean;
}) {
  const { t } = useT();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim());
  const customers = useCustomers(marinaId, { q: deferred });
  const options = (customers.data?.pages.flatMap((p) => p.items) ?? []).map((c) => ({
    value: c.id,
    label: c.display_name || t('customers:anonymized'),
  }));
  if (value && !options.some((o) => o.value === value.id))
    options.unshift({ value: value.id, label: value.label });
  return (
    <ElementCombobox
      id={id}
      options={options}
      value={value?.id ?? null}
      onSearch={setQ}
      onChange={(v) =>
        onChange(v ? { id: v, label: options.find((o) => o.value === v)?.label ?? '' } : null)
      }
      placeholder={t('desk:fields.customerPlaceholder')}
      searchPlaceholder={t('desk:fields.customerSearch')}
      emptyText={t('desk:fields.noCustomers')}
      {...(disabled ? { disabled } : {})}
    />
  );
}
