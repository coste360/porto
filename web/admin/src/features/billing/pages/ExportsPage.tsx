import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { DownloadIcon, PlusIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useChargeExport,
  useChargeExportFile,
  useChargeExports,
  useCreateChargeExport,
  useInvoiceReconciliations,
  usePreviewChargeExport,
  useReconcileInvoices,
  useReexportCharges,
  type ChargeExport,
  type ChargeOriginType,
  type InvoiceReconciliation,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
import { useMarinaToday } from '../../registry/lib/today';
import {
  BillingBadge,
  BillingProblemAlert,
  Detail,
  DetailGrid,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  Section,
  SimpleTable,
  Stat,
  StepBar,
  VatTable,
} from '../components/kit';
import {
  EXPORT_STEPS,
  canConfirm,
  formError,
  previousMonth,
  toFilter,
  vatTotalsMatch,
  type ExportForm,
  type ExportStep,
} from '../lib/exportWizard';
import { ORIGIN_TYPES } from '../lib/catalog';
import { openBlob, saveBlob } from '../lib/files';

/** Downloads of an export: CSV, Excel and the summary PDF. */
export function ExportDownloads({ exp }: { exp: Pick<ChargeExport, 'id' | 'number'> }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const file = useChargeExportFile(marinaId);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        {(['csv', 'xlsx', 'pdf'] as const).map((format) => (
          <Button
            key={format}
            variant="outline"
            disabled={file.isPending}
            onClick={() =>
              file.mutate(
                { exportId: exp.id, format },
                {
                  onSuccess: (b) =>
                    format === 'pdf' ? openBlob(b) : saveBlob(`${exp.number}.${format}`, b),
                },
              )
            }
          >
            <DownloadIcon />
            {t(`exports:download.${format}`)}
          </Button>
        ))}
      </div>
      <BillingProblemAlert error={file.error} />
    </div>
  );
}

/** History of exports and reconciliations (m5-plan §3.6, §9.1). */
export function ExportsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const list = useChargeExports(marinaId);
  const recs = useInvoiceReconciliations(marinaId);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const recItems = recs.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('charges.export')}>
      <PageFrame>
        <PageHeader
          title={t('exports:title')}
          description={t('exports:description')}
          actions={
            <>
              <Button asChild variant="outline">
                <Link to="/m/$marinaId/exports/reconcile" params={{ marinaId }}>
                  {t('exports:reconcile.open')}
                </Link>
              </Button>
              <Button asChild>
                <Link to="/m/$marinaId/exports/new" params={{ marinaId }}>
                  <PlusIcon />
                  {t('exports:new')}
                </Link>
              </Button>
            </>
          }
        />
        <Section title={t('exports:history')}>
          {list.isPending ? (
            <LoadingState />
          ) : list.isError ? (
            <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
          ) : (
            <SimpleTable
              head={[
                t('exports:fields.number'),
                t('exports:fields.kind'),
                t('exports:fields.period'),
                t('exports:fields.charges'),
                t('exports:fields.total'),
                t('exports:fields.delivery'),
                t('exports:fields.createdAt'),
              ]}
              empty={t('exports:empty')}
              rows={items.length}
            >
              {items.map((e) => (
                <TableRow key={e.id} data-testid="export-row">
                  <TableCell>
                    <Link
                      to="/m/$marinaId/exports/$exportId"
                      params={{ marinaId, exportId: e.id }}
                      className="font-medium underline-offset-4 hover:underline"
                    >
                      {e.number}
                    </Link>
                  </TableCell>
                  <TableCell>{t(`billingEnums:exportKind.${e.kind}`)}</TableCell>
                  <TableCell>
                    <IsoDate date={e.period_from} /> <IsoDate date={e.period_to} />
                  </TableCell>
                  <TableCell>{e.charge_count}</TableCell>
                  <TableCell>
                    <Money cents={e.total_cents} />
                  </TableCell>
                  <TableCell>
                    <BillingBadge group="deliveryStatus" value={e.delivery_status} />
                  </TableCell>
                  <TableCell>{formatDateTime(e.created_at, i18n.language)}</TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          )}
          <LoadMore query={list} />
        </Section>
        <Section title={t('exports:reconcile.history')}>
          <SimpleTable
            head={[
              t('exports:fields.createdAt'),
              t('exports:reconcile.source'),
              t('exports:reconcile.rows'),
              t('exports:reconcile.matched'),
              t('exports:reconcile.dryRun'),
            ]}
            empty={t('exports:reconcile.empty')}
            rows={recItems.length}
          >
            {recItems.map((r) => (
              <TableRow key={r.id}>
                <TableCell>{formatDateTime(r.created_at, i18n.language)}</TableCell>
                <TableCell>{t(`exports:reconcile.sources.${r.source}`)}</TableCell>
                <TableCell>{r.row_count}</TableCell>
                <TableCell>{r.matched_count}</TableCell>
                <TableCell>{r.dry_run ? t('exports:reconcile.yes') : ''}</TableCell>
              </TableRow>
            ))}
          </SimpleTable>
          <LoadMore query={recs} />
        </Section>
      </PageFrame>
    </RequirePermission>
  );
}

/** Export wizard: period and filters → preview totals per VAT and origin, warnings → confirm → downloads. */
export function ExportWizardPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [step, setStep] = useState<ExportStep>('filters');
  const [form, setForm] = useState<ExportForm>(() => ({
    ...previousMonth(today),
    originTypes: [],
  }));
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const preview = usePreviewChargeExport(marinaId);
  const create = useCreateChargeExport(marinaId);
  const [created, setCreated] = useState<ChargeExport | null>(null);
  const effective: ExportForm = { ...form, ...(customer ? { customerId: customer.id } : {}) };
  const error = formError(effective);
  const toggleOrigin = (o: ChargeOriginType) =>
    setForm((f) => ({
      ...f,
      originTypes: f.originTypes.includes(o)
        ? f.originTypes.filter((x) => x !== o)
        : [...f.originTypes, o],
    }));
  const p = preview.data;
  return (
    <RequirePermission allowed={can('charges.export')}>
      <PageFrame>
        <PageHeader
          title={t('exports:wizard.title')}
          description={t('exports:wizard.description')}
        />
        <StepBar
          steps={EXPORT_STEPS}
          current={step}
          label={t('exports:wizard.steps')}
          labelOf={(s) => t(`exports:wizard.stepNames.${s}`)}
          {...(step === 'preview' ? { onSelect: setStep } : {})}
        />
        {step === 'filters' ? (
          <Section title={t('exports:wizard.stepNames.filters')}>
            <div className="flex flex-wrap items-end gap-3">
              <Field className="w-auto">
                <FieldLabel htmlFor="exp-from">{t('exports:wizard.from')}</FieldLabel>
                <DatePicker
                  id="exp-from"
                  value={form.from}
                  onChange={(from) => setForm({ ...form, from })}
                />
              </Field>
              <Field className="w-auto">
                <FieldLabel htmlFor="exp-to">{t('exports:wizard.to')}</FieldLabel>
                <DatePicker
                  id="exp-to"
                  value={form.to}
                  onChange={(to) => setForm({ ...form, to })}
                />
              </Field>
              <Field className="w-64">
                <FieldLabel htmlFor="exp-customer">{t('exports:wizard.customer')}</FieldLabel>
                <CustomerField
                  marinaId={marinaId}
                  id="exp-customer"
                  value={customer}
                  onChange={setCustomer}
                />
              </Field>
            </div>
            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">{t('exports:wizard.origins')}</legend>
              <div className="flex flex-wrap gap-3">
                {ORIGIN_TYPES.map((o) => (
                  <label key={o} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={form.originTypes.includes(o)}
                      onCheckedChange={() => toggleOrigin(o)}
                    />
                    {t(`billingEnums:originType.${o}`)}
                  </label>
                ))}
              </div>
              <p className="text-muted-foreground text-xs">{t('exports:wizard.originsHint')}</p>
            </fieldset>
            {error ? <p className="text-destructive text-sm">{t(error)}</p> : null}
            <BillingProblemAlert error={preview.error} />
            <div>
              <Button
                disabled={!!error || preview.isPending}
                onClick={() =>
                  preview.mutate(toFilter(effective), { onSuccess: () => setStep('preview') })
                }
              >
                {t('exports:wizard.preview')}
              </Button>
            </div>
          </Section>
        ) : null}
        {step === 'preview' && p ? (
          <>
            <section className="grid gap-2 sm:grid-cols-2" data-testid="export-preview">
              <Stat label={t('exports:wizard.count')}>{p.count}</Stat>
              <Stat label={t('exports:fields.total')}>
                <Money cents={p.total_cents} />
              </Stat>
            </section>
            {!vatTotalsMatch(p) ? (
              <Alert variant="destructive">
                <AlertTitle>{t('exports:wizard.mismatch')}</AlertTitle>
              </Alert>
            ) : null}
            {p.warnings.length ? (
              <Alert data-testid="export-warnings">
                <AlertTitle>{t('exports:wizard.warningsTitle')}</AlertTitle>
                <AlertDescription>
                  <ul className="flex flex-col gap-1">
                    {p.warnings.map((w, i) => (
                      <li key={i}>
                        {t(`exports:wizard.warnings.${w.code}`, { count: w.charge_ids.length })}{' '}
                        {w.customer_id ? (
                          <Link
                            to="/m/$marinaId/customers/$customerId"
                            params={{ marinaId, customerId: w.customer_id }}
                            className="underline underline-offset-4"
                          >
                            {t('exports:wizard.openCustomer')}
                          </Link>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            ) : null}
            <Section title={t('exports:wizard.byVat')}>
              <VatTable buckets={p.totals_by_vat} />
            </Section>
            <Section title={t('exports:wizard.byOrigin')}>
              <SimpleTable
                head={[
                  t('charges:fields.origin'),
                  t('exports:wizard.count'),
                  t('exports:fields.total'),
                ]}
                empty={t('exports:wizard.nothing')}
                rows={p.totals_by_origin.length}
              >
                {p.totals_by_origin.map((o) => (
                  <TableRow key={o.origin_type}>
                    <TableCell>{t(`billingEnums:originType.${o.origin_type}`)}</TableCell>
                    <TableCell>{o.count}</TableCell>
                    <TableCell>
                      <Money cents={o.total_cents} />
                    </TableCell>
                  </TableRow>
                ))}
              </SimpleTable>
            </Section>
            <BillingProblemAlert error={create.error} />
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setStep('filters')}>
                {t('exports:wizard.back')}
              </Button>
              <Button
                disabled={!canConfirm(p) || create.isPending}
                onClick={() =>
                  create.mutate(toFilter(effective), {
                    onSuccess: (e) => {
                      setCreated(e);
                      setStep('done');
                    },
                  })
                }
              >
                {t('exports:wizard.confirm')}
              </Button>
            </div>
          </>
        ) : null}
        {step === 'done' && created ? (
          <Section title={t('exports:wizard.doneTitle', { number: created.number })}>
            <p role="status" className="text-sm" data-testid="export-done">
              {t('exports:wizard.done', { count: created.charge_count })}{' '}
              <Money cents={created.total_cents} />
            </p>
            <ExportDownloads exp={created} />
            <div>
              <Button asChild variant="outline">
                <Link
                  to="/m/$marinaId/exports/$exportId"
                  params={{ marinaId, exportId: created.id }}
                >
                  {t('exports:wizard.openExport')}
                </Link>
              </Button>
            </div>
          </Section>
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

const exportRoute = getRouteApi('/auth/m/$marinaId/exports/$exportId');

/** Export detail: totals, items, downloads and re-export. */
export function ExportPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const { exportId } = exportRoute.useParams();
  const detail = useChargeExport(marinaId, exportId);
  const reexport = useReexportCharges(marinaId);
  const navigate = useNavigate();
  const exp = detail.data?.pages[0]?.export;
  const items = detail.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('charges.export')}>
      <PageFrame>
        {detail.isPending ? (
          <LoadingState rows={6} />
        ) : detail.isError || !exp ? (
          <QueryErrorState error={detail.error} onRetry={() => void detail.refetch()} />
        ) : (
          <>
            <PageHeader
              title={t('exports:detail.title', { number: exp.number })}
              actions={
                <Button
                  variant="outline"
                  disabled={reexport.isPending}
                  onClick={() =>
                    reexport.mutate(
                      { export_id: exp.id },
                      {
                        onSuccess: (e) => {
                          toast.success(t('exports:detail.reexported', { number: e.number }));
                          void navigate({
                            to: '/m/$marinaId/exports/$exportId',
                            params: { marinaId, exportId: e.id },
                          });
                        },
                      },
                    )
                  }
                >
                  {t('exports:detail.reexport')}
                </Button>
              }
            />
            <BillingProblemAlert error={reexport.error} />
            <Section title={t('exports:detail.summary')}>
              <DetailGrid>
                <Detail label={t('exports:fields.kind')}>
                  {t(`billingEnums:exportKind.${exp.kind}`)}
                </Detail>
                <Detail label={t('exports:fields.period')}>
                  <IsoDate date={exp.period_from} /> <IsoDate date={exp.period_to} />
                </Detail>
                <Detail label={t('exports:fields.charges')}>{exp.charge_count}</Detail>
                <Detail label={t('exports:fields.lines')}>{exp.line_count}</Detail>
                <Detail label={t('exports:fields.total')}>
                  <Money cents={exp.total_cents} />
                </Detail>
                <Detail label={t('exports:fields.delivery')}>
                  <BillingBadge group="deliveryStatus" value={exp.delivery_status} />
                </Detail>
                <Detail label={t('exports:fields.createdAt')}>
                  {formatDateTime(exp.created_at, i18n.language)}
                </Detail>
              </DetailGrid>
              <ExportDownloads exp={exp} />
            </Section>
            <Section title={t('exports:wizard.byVat')}>
              <VatTable buckets={exp.totals_by_vat ?? []} />
            </Section>
            <Section title={t('exports:detail.items')}>
              <SimpleTable
                head={[
                  t('charges:fields.number'),
                  t('charges:fields.customer'),
                  t('exports:fields.first'),
                  t('exports:fields.total'),
                ]}
                empty={t('exports:detail.noItems')}
                rows={items.length}
              >
                {items.map((i) => (
                  <TableRow key={i.charge_id} data-testid="export-item">
                    <TableCell>
                      <Link
                        to="/m/$marinaId/charges/$chargeId"
                        params={{ marinaId, chargeId: i.charge_id }}
                        className="underline-offset-4 hover:underline"
                      >
                        {i.number}
                      </Link>
                    </TableCell>
                    <TableCell>{i.customer?.display_name ?? ''}</TableCell>
                    <TableCell>{i.first ? t('exports:reconcile.yes') : ''}</TableCell>
                    <TableCell>
                      <Money cents={i.total_cents} />
                    </TableCell>
                  </TableRow>
                ))}
              </SimpleTable>
              <LoadMore query={detail} />
            </Section>
          </>
        )}
      </PageFrame>
    </RequirePermission>
  );
}

/** Reconciliation of invoice numbers (m5-plan §3.6): upload the CSV of the invoicing software, dry run, apply. */
export function ReconcilePage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const reconcile = useReconcileInvoices(marinaId);
  const [csv, setCsv] = useState('');
  const [report, setReport] = useState<InvoiceReconciliation | null>(null);
  const run = (dry: boolean) =>
    reconcile.mutate({ csv, dry_run: dry }, { onSuccess: (r) => setReport(r) });
  return (
    <RequirePermission allowed={can('charges.export')}>
      <PageFrame>
        <PageHeader
          title={t('exports:reconcile.title')}
          description={t('exports:reconcile.description')}
        />
        <Section title={t('exports:reconcile.file')}>
          <Field>
            <FieldLabel htmlFor="rec-file">{t('exports:reconcile.upload')}</FieldLabel>
            <Input
              id="rec-file"
              type="file"
              accept=".csv,text/csv"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) void f.text().then((txt) => setCsv(txt));
                setReport(null);
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="rec-csv">{t('exports:reconcile.paste')}</FieldLabel>
            <Textarea
              id="rec-csv"
              rows={6}
              value={csv}
              onChange={(e) => {
                setCsv(e.target.value);
                setReport(null);
              }}
            />
          </Field>
          <p className="text-muted-foreground text-xs">{t('exports:reconcile.columns')}</p>
          <BillingProblemAlert error={reconcile.error} />
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={!csv.trim() || reconcile.isPending}
              onClick={() => run(true)}
            >
              {t('exports:reconcile.dryRunAction')}
            </Button>
            <Button
              disabled={!csv.trim() || !report?.dry_run || reconcile.isPending}
              onClick={() => run(false)}
            >
              {t('exports:reconcile.apply')}
            </Button>
          </div>
        </Section>
        {report ? (
          <Section
            title={
              report.dry_run ? t('exports:reconcile.reportDry') : t('exports:reconcile.reportDone')
            }
          >
            <p role="status" className="text-sm" data-testid="reconcile-summary">
              {t('exports:reconcile.summary', {
                rows: report.row_count,
                matched: report.matched_count,
              })}
            </p>
            <SimpleTable
              head={[
                t('exports:reconcile.row'),
                t('charges:fields.number'),
                t('exports:reconcile.outcome'),
              ]}
              empty=""
              rows={report.report.length}
            >
              {report.report.map((r) => (
                <TableRow key={r.row} data-testid="reconcile-row">
                  <TableCell>{r.row}</TableCell>
                  <TableCell>{r.charge_number ?? ''}</TableCell>
                  <TableCell>
                    <BillingBadge
                      group="reconciliationOutcome"
                      value={r.outcome}
                      variant={r.outcome === 'ok' ? 'default' : 'destructive'}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          </Section>
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}
