import { useState } from 'react';
import { toast } from 'sonner';

import {
  useCreateDunningExclusion,
  useCreateReminderRun,
  useDeleteDunningExclusion,
  useDunningExclusions,
  useReminderItemAction,
  useReminderRun,
  useReminderRuns,
  type DunningExclusion,
  type ReminderItem,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  Section,
  SimpleTable,
} from '../components/kit';

const EXCLUSION_KINDS: readonly DunningExclusion['kind'][] = ['payment_plan', 'disputed', 'other'];

/** Reminders (m5-plan §5.5, §9.1): runs and items by level, approval of final notices, exclusions. */
export function RemindersPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader title={t('reminders:title')} description={t('reminders:description')} />
        <Tabs defaultValue="runs">
          <TabsList>
            <TabsTrigger value="runs">{t('reminders:tabs.runs')}</TabsTrigger>
            <TabsTrigger value="exclusions">{t('reminders:tabs.exclusions')}</TabsTrigger>
          </TabsList>
          <TabsContent value="runs">
            <Runs />
          </TabsContent>
          <TabsContent value="exclusions">
            <Exclusions />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}

function Runs() {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const runs = useReminderRuns(marinaId);
  const create = useCreateReminderRun(marinaId);
  const [runId, setRunId] = useState<string | null>(null);
  const items = runs.data?.pages.flatMap((p) => p.items) ?? [];
  const current = runId ?? items[0]?.id ?? null;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button
          disabled={create.isPending}
          onClick={() =>
            create.mutate(undefined, {
              onSuccess: (r) => {
                setRunId(r.id);
                toast.success(t('reminders:runs.started'));
              },
            })
          }
        >
          {t('reminders:runs.run')}
        </Button>
      </div>
      <BillingProblemAlert error={create.error} />
      {runs.isPending ? (
        <LoadingState />
      ) : runs.isError ? (
        <QueryErrorState error={runs.error} />
      ) : (
        <SimpleTable
          head={[
            t('reminders:runs.runOn'),
            t('reminders:runs.status'),
            t('reminders:runs.counts'),
            t('reminders:runs.startedAt'),
            '',
          ]}
          empty={t('reminders:runs.empty')}
          rows={items.length}
        >
          {items.map((r) => (
            <TableRow key={r.id} data-testid="reminder-run">
              <TableCell>
                <IsoDate date={r.run_on} />
              </TableCell>
              <TableCell>
                <BillingBadge group="runStatus" value={r.status} />
              </TableCell>
              <TableCell className="text-sm">
                {Object.entries(r.counts)
                  .map(([k, v]) => `${t(`billingEnums:reminderItemStatus.${k}`, k)}: ${v}`)
                  .join(' · ')}
              </TableCell>
              <TableCell>{formatDateTime(r.started_at, i18n.language)}</TableCell>
              <TableCell className="text-right">
                <Button
                  size="sm"
                  variant={current === r.id ? 'default' : 'outline'}
                  onClick={() => setRunId(r.id)}
                >
                  {t('reminders:runs.show')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <LoadMore query={runs} />
      {current ? <RunItems runId={current} /> : null}
    </div>
  );
}

function RunItems({ runId }: { runId: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const run = useReminderRun(marinaId, runId);
  const act = useReminderItemAction(marinaId);
  const items: ReminderItem[] = run.data?.pages.flatMap((p) => p.items) ?? [];
  const awaiting = items.filter((i) => i.status === 'awaiting_approval');
  const rows = (list: ReminderItem[], actions: boolean) => (
    <SimpleTable
      head={[
        t('reminders:fields.charge'),
        t('reminders:fields.customer'),
        t('reminders:fields.level'),
        t('reminders:fields.residual'),
        t('reminders:fields.status'),
        '',
      ]}
      empty={t('reminders:items.empty')}
      rows={list.length}
    >
      {list.map((i) => (
        <TableRow key={i.id} data-testid={actions ? 'approval-item' : 'reminder-item'}>
          <TableCell>{i.charge_number ?? i.charge_id}</TableCell>
          <TableCell>{i.customer.display_name ?? ''}</TableCell>
          <TableCell>{t(`billingEnums:reminderLevel.${i.level}`)}</TableCell>
          <TableCell>
            <Money cents={i.residual_cents} />
          </TableCell>
          <TableCell>
            <BillingBadge group="reminderItemStatus" value={i.status} />
          </TableCell>
          <TableCell className="flex justify-end gap-2">
            {actions ? (
              <>
                <Button
                  size="sm"
                  disabled={act.isPending}
                  onClick={() => act.mutate({ id: i.id, action: 'approve' })}
                >
                  {t('reminders:items.approve')}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={act.isPending}
                  onClick={() => act.mutate({ id: i.id, action: 'dismiss' })}
                >
                  {t('reminders:items.dismiss')}
                </Button>
              </>
            ) : null}
          </TableCell>
        </TableRow>
      ))}
    </SimpleTable>
  );
  if (run.isPending) return <LoadingState />;
  if (run.isError) return <QueryErrorState error={run.error} />;
  return (
    <div className="flex flex-col gap-4">
      <Section
        title={t('reminders:items.approvalTitle')}
        description={t('reminders:items.approvalDescription')}
      >
        {rows(awaiting, true)}
      </Section>
      <BillingProblemAlert error={act.error} />
      <Section title={t('reminders:items.title')}>{rows(items, false)}</Section>
      <LoadMore query={run} />
    </div>
  );
}

function Exclusions() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const list = useDunningExclusions(marinaId, { active: true });
  const create = useCreateDunningExclusion(marinaId);
  const remove = useDeleteDunningExclusion(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [kind, setKind] = useState<DunningExclusion['kind']>('payment_plan');
  const [until, setUntil] = useState<string | null>(null);
  const [note, setNote] = useState('');
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <div className="flex flex-col gap-4">
      <Section title={t('reminders:exclusions.new')}>
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-64">
            <FieldLabel htmlFor="excl-customer">{t('reminders:fields.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="excl-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
          <EnumSelect
            id="excl-kind"
            label={t('reminders:exclusions.kind')}
            values={EXCLUSION_KINDS}
            labelOf={(k) => t(`billingEnums:exclusionKind.${k}`)}
            value={kind}
            onChange={(k) => k && setKind(k)}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="excl-until">{t('reminders:exclusions.until')}</FieldLabel>
            <DatePicker id="excl-until" value={until} onChange={setUntil} />
          </Field>
          <Field className="w-64">
            <FieldLabel htmlFor="excl-note">{t('reminders:exclusions.note')}</FieldLabel>
            <Input
              id="excl-note"
              maxLength={500}
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
          <Button
            disabled={!customer || create.isPending}
            onClick={() =>
              create.mutate(
                {
                  kind,
                  customer_id: customer?.id ?? '',
                  ...(until ? { until } : {}),
                  ...(note.trim() ? { note: note.trim() } : {}),
                },
                {
                  onSuccess: () => {
                    setCustomer(null);
                    setNote('');
                    setUntil(null);
                  },
                },
              )
            }
          >
            {t('reminders:exclusions.add')}
          </Button>
        </div>
        <BillingProblemAlert error={create.error ?? remove.error} />
      </Section>
      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <QueryErrorState error={list.error} />
      ) : (
        <SimpleTable
          head={[
            t('reminders:fields.customer'),
            t('reminders:fields.charge'),
            t('reminders:exclusions.kind'),
            t('reminders:exclusions.until'),
            t('reminders:exclusions.note'),
            '',
          ]}
          empty={t('reminders:exclusions.empty')}
          rows={items.length}
        >
          {items.map((e) => (
            <TableRow key={e.id} data-testid="exclusion-row">
              <TableCell>{e.customer?.display_name ?? ''}</TableCell>
              <TableCell>{e.charge_id ?? ''}</TableCell>
              <TableCell>{t(`billingEnums:exclusionKind.${e.kind}`)}</TableCell>
              <TableCell>
                <IsoDate date={e.until} />
              </TableCell>
              <TableCell>{e.note}</TableCell>
              <TableCell className="text-right">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(e.id)}
                >
                  {t('reminders:exclusions.remove')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <LoadMore query={list} />
    </div>
  );
}
