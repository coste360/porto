import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

import { useCreateCharge } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import { BillingProblemAlert, EnumSelect, PageFrame, Section } from '../components/kit';
import { LinesEditor } from '../components/LinesEditor';
import { type LineDraft, emptyLine, linesValid, toLineInputs } from '../lib/drafts';

const ORIGINS = ['manual', 'counter_sale'] as const;

/** Manual charge or counter sale on account (m5-plan §9.1): customer, item lines, VAT preview. */
export function NewChargePage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const navigate = useNavigate();
  const create = useCreateCharge(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [origin, setOrigin] = useState<(typeof ORIGINS)[number]>('manual');
  const [due, setDue] = useState<string | null>(null);
  const [lines, setLines] = useState<LineDraft[]>(() => [emptyLine()]);
  const valid = !!customer && linesValid(lines);
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader title={t('charges:new.title')} description={t('charges:new.description')} />
        <Section title={t('charges:new.header')}>
          <div className="flex flex-wrap items-end gap-3">
            <Field className="w-72">
              <FieldLabel htmlFor="new-charge-customer">{t('charges:fields.customer')}</FieldLabel>
              <CustomerField
                marinaId={marinaId}
                id="new-charge-customer"
                value={customer}
                onChange={setCustomer}
              />
            </Field>
            <EnumSelect
              id="new-charge-origin"
              label={t('charges:fields.origin')}
              values={ORIGINS}
              labelOf={(o) => t(`billingEnums:originType.${o}`)}
              value={origin}
              onChange={(o) => o && setOrigin(o)}
            />
            <Field className="w-auto">
              <FieldLabel htmlFor="new-charge-due">{t('charges:fields.dueOn')}</FieldLabel>
              <DatePicker id="new-charge-due" value={due} onChange={setDue} />
            </Field>
          </div>
          <p className="text-muted-foreground text-xs">{t('charges:new.dueHint')}</p>
        </Section>
        <Section title={t('charges:detail.lines')}>
          <LinesEditor marinaId={marinaId} lines={lines} onChange={setLines} idPrefix="new" />
        </Section>
        <BillingProblemAlert error={create.error} />
        <div className="flex justify-end">
          <Button
            disabled={!valid || create.isPending}
            onClick={() =>
              create.mutate(
                {
                  origin_type: origin,
                  customer_id: customer?.id ?? '',
                  lines: toLineInputs(lines),
                  ...(due ? { due_on: due } : {}),
                },
                {
                  onSuccess: (c) =>
                    void navigate({
                      to: '/m/$marinaId/charges/$chargeId',
                      params: { marinaId, chargeId: c.id },
                    }),
                },
              )
            }
          >
            {t('charges:new.submit')}
          </Button>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}
