import { PlusIcon, TrashIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';

import { CustomerField } from '../../desk/components/CustomerField';
import { checkShares, equalShares, holderRows, type HolderDraft } from '../lib/shares';

/** Holders of a right with their shares in millesimi and the live Σ‰ indicator (INV-40). */
export function HoldersEditor({
  marinaId,
  idPrefix,
  rows,
  onChange,
  showErrors,
}: {
  marinaId: string;
  idPrefix: string;
  rows: HolderDraft[];
  onChange: (rows: HolderDraft[]) => void;
  showErrors: boolean;
}) {
  const { t } = useT();
  const check = checkShares(holderRows(rows));
  return (
    <div className="flex flex-col gap-2">
      {rows.map((r, i) => (
        <div key={i} className="flex flex-wrap items-end gap-2">
          <Field className="min-w-56 flex-1">
            <FieldLabel htmlFor={`${idPrefix}-holder-${i}`}>
              {t('rights:holders.customer', { n: i + 1 })}
            </FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id={`${idPrefix}-holder-${i}`}
              value={r.customer}
              onChange={(customer) =>
                onChange(rows.map((x, j) => (j === i ? { ...x, customer } : x)))
              }
            />
          </Field>
          <Field className="w-32">
            <FieldLabel htmlFor={`${idPrefix}-share-${i}`}>{t('rights:holders.share')}</FieldLabel>
            <Input
              id={`${idPrefix}-share-${i}`}
              inputMode="numeric"
              value={r.text}
              onChange={(e) =>
                onChange(rows.map((x, j) => (j === i ? { ...x, text: e.target.value } : x)))
              }
            />
          </Field>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('rights:holders.remove', { n: i + 1 })}
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          >
            <TrashIcon />
          </Button>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => onChange([...rows, { customer: null, text: '' }])}
        >
          <PlusIcon />
          {t('rights:holders.add')}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={rows.length === 0}
          onClick={() => {
            const shares = equalShares(rows.length);
            onChange(rows.map((r, i) => ({ ...r, text: String(shares[i]) })));
          }}
        >
          {t('rights:holders.equal')}
        </Button>
        <span
          className={
            check.sum === 1000 ? 'text-sm font-medium' : 'text-destructive text-sm font-medium'
          }
          data-testid="holders-sum"
          aria-live="polite"
        >
          {t('rights:holders.sum', { sum: check.sum, remaining: check.remaining })}
        </span>
      </div>
      {showErrors && !check.ok
        ? check.errors.map((e) => (
            <FieldError key={e}>{t(`rights:holders.errors.${e}`)}</FieldError>
          ))
        : null}
    </div>
  );
}
