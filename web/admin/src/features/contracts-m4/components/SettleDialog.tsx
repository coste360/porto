import { useState } from 'react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker } from '@porto/shared/ui/patterns';

import { useMarinaToday } from '../../registry/lib/today';
import { SETTLEMENT_METHODS, bulkSettleBody, type SettleForm } from '../lib/installments';
import { EnumSelect, M4ProblemAlert, Money } from './kit';

import type { Schemas } from '@porto/shared/api';

/**
 * Manual settlement of one or more instalments (contract or holder, INV-32): method, reference and date, all or
 * nothing. The caller passes the selected items and the mutation (single or bulk endpoint).
 */
export function SettleDialog({
  open,
  items,
  totalCents,
  pending,
  error,
  onClose,
  onSettle,
}: {
  open: boolean;
  items: readonly { id: string; version: number }[];
  totalCents: number;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onSettle: (body: Schemas['BulkSettleInput']) => void;
}) {
  const { t } = useT();
  const today = useMarinaToday();
  const [form, setForm] = useState<SettleForm>({ method: '', reference: '', settledOn: today });
  const [touched, setTouched] = useState(false);
  const mapped = bulkSettleBody(items, form, today);
  const err = touched && !mapped.ok ? mapped.error : null;
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('contractsM4:settle.title', { count: items.length })}</DialogTitle>
          <DialogDescription>
            {t('contractsM4:settle.description')} <Money cents={totalCents} />
          </DialogDescription>
        </DialogHeader>
        <EnumSelect
          id="settle-method"
          label={t('contractsM4:settle.method')}
          group="settlementMethod"
          values={SETTLEMENT_METHODS}
          value={form.method}
          onChange={(method) => setForm({ ...form, method })}
        />
        <Field data-invalid={err === 'reference' || undefined}>
          <FieldLabel htmlFor="settle-reference">{t('contractsM4:settle.reference')}</FieldLabel>
          <Input
            id="settle-reference"
            value={form.reference}
            maxLength={140}
            onChange={(e) => setForm({ ...form, reference: e.target.value })}
          />
        </Field>
        <Field data-invalid={err === 'date' || undefined}>
          <FieldLabel htmlFor="settle-on">{t('contractsM4:settle.on')}</FieldLabel>
          <DatePicker
            id="settle-on"
            value={form.settledOn}
            onChange={(settledOn) => setForm({ ...form, settledOn })}
          />
        </Field>
        {err ? <FieldError>{t(`contractsM4:settle.errors.${err}`)}</FieldError> : null}
        <M4ProblemAlert error={error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={pending}
            onClick={() => {
              setTouched(true);
              if (mapped.ok) onSettle(mapped.body);
            }}
          >
            {t('contractsM4:settle.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
