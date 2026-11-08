import { FlaskConicalIcon } from 'lucide-react';

import {
  useValidateFormula,
  type FormulaIssue,
  type FormulaValidateInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatQtyMilli } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Textarea } from '@porto/shared/ui/textarea';

import { IssueList } from './IssueList';

type Purpose = NonNullable<FormulaValidateInput['purpose']>;

/**
 * A formula field (m10-plan §14.1): monospace textarea, the issues reported by the last save and a live test on
 * the marina test vessel (or the vessel chosen) through validateFormula.
 */
export function FormulaField({
  id,
  label,
  value,
  onChange,
  marinaId,
  purpose = 'quantity',
  itemId,
  vesselId,
  issues = [],
  description,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  marinaId: string;
  purpose?: Purpose;
  itemId?: string;
  vesselId?: string;
  issues?: readonly FormulaIssue[];
  description?: string;
}) {
  const { t, i18n } = useT();
  const test = useValidateFormula(marinaId);
  const result = test.data;
  const run = () =>
    test.mutate({
      formula: value,
      purpose,
      ...(itemId ? { item_id: itemId } : {}),
      ...(vesselId ? { vessel_id: vesselId } : {}),
    });
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Textarea
        id={id}
        className="font-mono"
        rows={2}
        maxLength={500}
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={run}
          disabled={!value || test.isPending}
        >
          <FlaskConicalIcon />
          {t('yardCatalog:formula.test')}
        </Button>
        {result?.valid ? (
          <span className="text-sm tabular-nums" data-testid={`${id}-result`}>
            {result.price_cents !== undefined
              ? t('yardCatalog:formula.priceResult', {
                  value: formatCents(result.price_cents, 'EUR', i18n.language),
                })
              : result.qty_milli !== undefined
                ? t('yardCatalog:formula.qtyResult', {
                    value: formatQtyMilli(result.qty_milli, i18n.language),
                  })
                : t('yardCatalog:formula.valid')}
          </span>
        ) : null}
      </div>
      <IssueList
        issues={result && !result.valid ? result.issues : issues}
        testId={`${id}-issues`}
      />
    </Field>
  );
}
