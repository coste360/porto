import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useConcession,
  useConcessionObligations,
  useCreateConcessionObligation,
  useDeleteConcessionObligation,
  useMarkObligationDone,
  useUpdateConcession,
  type Concession,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, MoneyInput, PageHeader } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  PageFrame,
  Section,
} from '../contracts-m4/components/kit';

const KINDS: readonly Schemas['ObligationKind'][] = [
  'fee_instalment',
  'deposit',
  'insurance',
  'periodic_report',
  'other',
];
const STATUSES = ['active', 'expired', 'revoked'] as const;

/** State concession (spec 14.7): the concession card (marina.configure to edit) and its obligations. */
export function ConcessionPage() {
  const { can, marinaId } = useCurrentMarina();
  const concession = useConcession(marinaId);
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        {concession.isPending ? (
          <LoadingState rows={6} />
        ) : concession.isError ? (
          <QueryErrorState error={concession.error} onRetry={() => void concession.refetch()} />
        ) : (
          <>
            <ConcessionCard key={concession.data.version} concession={concession.data} />
            <Obligations />
          </>
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function ConcessionCard({ concession }: { concession: Concession }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const save = useUpdateConcession(marinaId);
  const [c, setC] = useState(concession);
  const edit = can('marina.configure');
  const text = (k: 'authority' | 'deed_kind' | 'deed_number') => (
    <Field>
      <FieldLabel htmlFor={`conc-${k}`}>{t(`rights:concession.${k}`)}</FieldLabel>
      <Input
        id={`conc-${k}`}
        disabled={!edit}
        value={c[k]}
        maxLength={200}
        onChange={(e) => setC({ ...c, [k]: e.target.value })}
      />
    </Field>
  );
  return (
    <>
      <PageHeader
        title={t('rights:concession.title')}
        description={t('rights:concession.description')}
        actions={
          edit ? (
            <Button
              disabled={save.isPending}
              onClick={() =>
                save.mutate(
                  {
                    version: concession.version,
                    body: {
                      authority: c.authority,
                      deed_kind: c.deed_kind,
                      deed_number: c.deed_number,
                      ...(c.granted_on ? { granted_on: c.granted_on } : {}),
                      ...(c.expires_on ? { expires_on: c.expires_on } : {}),
                      annual_fee_cents: c.annual_fee_cents,
                      surfaces: c.surfaces,
                      prescriptions: c.prescriptions,
                      status: c.status,
                    },
                  },
                  { onSuccess: () => toast.success(t('rights:concession.saved')) },
                )
              }
            >
              {t('common:actions.save')}
            </Button>
          ) : null
        }
      />
      <M4ProblemAlert error={save.error} />
      <Section title={t('rights:concession.card')}>
        <div className="grid gap-3 sm:grid-cols-3">
          {text('authority')}
          {text('deed_kind')}
          {text('deed_number')}
          <Field>
            <FieldLabel htmlFor="conc-granted">{t('rights:concession.granted_on')}</FieldLabel>
            <DatePicker
              id="conc-granted"
              disabled={!edit}
              value={c.granted_on ?? null}
              onChange={(v) => setC({ ...c, granted_on: v ?? undefined })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="conc-expires">{t('rights:concession.expires_on')}</FieldLabel>
            <DatePicker
              id="conc-expires"
              disabled={!edit}
              value={c.expires_on ?? null}
              onChange={(v) => setC({ ...c, expires_on: v ?? undefined })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="conc-fee">{t('rights:concession.annual_fee')}</FieldLabel>
            <MoneyInput
              id="conc-fee"
              disabled={!edit}
              locale={i18n.language}
              value={c.annual_fee_cents}
              onChange={(v) => setC({ ...c, annual_fee_cents: v ?? 0 })}
            />
          </Field>
          <EnumSelect
            id="conc-status"
            label={t('rights:concession.status')}
            values={STATUSES}
            labelOf={(v) => t(`rights:concession.statuses.${v}`)}
            value={c.status}
            onChange={(v) => setC({ ...c, status: v || 'active' })}
          />
        </div>
        <Field>
          <FieldLabel htmlFor="conc-prescriptions">
            {t('rights:concession.prescriptions')}
          </FieldLabel>
          <Textarea
            id="conc-prescriptions"
            disabled={!edit}
            rows={4}
            value={c.prescriptions}
            onChange={(e) => setC({ ...c, prescriptions: e.target.value })}
          />
        </Field>
      </Section>
    </>
  );
}

function Obligations() {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const list = useConcessionObligations(marinaId);
  const create = useCreateConcessionObligation(marinaId);
  const done = useMarkObligationDone(marinaId);
  const del = useDeleteConcessionObligation(marinaId);
  const [kind, setKind] = useState<Schemas['ObligationKind'] | ''>('fee_instalment');
  const [dueOn, setDueOn] = useState<string | null>(null);
  const [desc, setDesc] = useState('');
  const [amount, setAmount] = useState<number | null>(null);
  const [yearly, setYearly] = useState<'none' | 'yearly' | ''>('yearly');
  return (
    <Section
      title={t('rights:concession.obligations')}
      description={t('rights:concession.obligationsHint')}
    >
      <M4ProblemAlert error={create.error ?? done.error ?? del.error} />
      {list.isPending ? (
        <LoadingState rows={3} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} />
      ) : (
        <ul className="flex flex-col gap-2">
          {list.data.map((o) => (
            <li
              key={o.id}
              className="flex flex-wrap items-center gap-2 rounded-md border p-2 text-sm"
            >
              <Badge variant="outline">{t(`docsEnums:obligationKind.${o.kind}`)}</Badge>
              <IsoDate date={o.due_on} />
              <span>{o.description}</span>
              {o.amount_cents ? <Money cents={o.amount_cents} /> : null}
              {o.recurrence === 'yearly' ? (
                <Badge variant="secondary">{t('rights:concession.yearly')}</Badge>
              ) : null}
              {o.done_on ? (
                <Badge>{t('rights:concession.doneOn', { date: o.done_on })}</Badge>
              ) : (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={done.isPending}
                  onClick={() => done.mutate({ id: o.id, version: o.version })}
                >
                  {t('rights:concession.markDone')}
                </Button>
              )}
              <Button
                size="icon"
                variant="ghost"
                aria-label={t('rights:concession.remove')}
                disabled={del.isPending}
                onClick={() => del.mutate({ id: o.id, version: o.version })}
              >
                <TrashIcon />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap items-end gap-2">
        <EnumSelect
          id="obl-kind"
          label={t('rights:concession.kind')}
          group="obligationKind"
          values={KINDS}
          value={kind}
          onChange={setKind}
        />
        <Field className="w-auto">
          <FieldLabel htmlFor="obl-due">{t('rights:concession.dueOn')}</FieldLabel>
          <DatePicker id="obl-due" value={dueOn} onChange={setDueOn} />
        </Field>
        <Field className="w-56">
          <FieldLabel htmlFor="obl-desc">{t('rights:concession.descriptionLabel')}</FieldLabel>
          <Input
            id="obl-desc"
            value={desc}
            maxLength={300}
            onChange={(e) => setDesc(e.target.value)}
          />
        </Field>
        <Field className="w-40">
          <FieldLabel htmlFor="obl-amount">{t('rights:concession.amount')}</FieldLabel>
          <MoneyInput id="obl-amount" locale={i18n.language} value={amount} onChange={setAmount} />
        </Field>
        <EnumSelect
          id="obl-recurrence"
          label={t('rights:concession.recurrence')}
          values={['none', 'yearly'] as const}
          labelOf={(v) => t(`rights:concession.recurrences.${v}`)}
          value={yearly}
          onChange={setYearly}
        />
        <Button
          disabled={!kind || !dueOn || create.isPending}
          onClick={() =>
            kind &&
            dueOn &&
            create.mutate(
              {
                kind,
                due_on: dueOn,
                ...(desc.trim() ? { description: desc.trim() } : {}),
                ...(amount !== null ? { amount_cents: amount } : {}),
                ...(yearly ? { recurrence: yearly } : {}),
              },
              {
                onSuccess: () => {
                  setDesc('');
                  setAmount(null);
                  setDueOn(null);
                },
              },
            )
          }
        >
          <PlusIcon />
          {t('rights:concession.add')}
        </Button>
      </div>
    </Section>
  );
}
