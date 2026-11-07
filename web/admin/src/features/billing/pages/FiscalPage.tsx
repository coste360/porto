import { DownloadIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useFiscalDailyTotals,
  useFiscalDocumentAction,
  useFiscalDocuments,
  useFiscalTotalsFile,
  type ExportFormat,
  type FiscalDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  IsoDate,
  LoadMore,
  Money,
  PageFrame,
  SimpleTable,
  VatRate,
} from '../components/kit';
import { saveBlob } from '../lib/files';

const STATUSES: readonly FiscalDocument['status'][] = ['pending', 'issued', 'failed', 'void'];

/** Corrispettivi and fiscal documents (m5-plan §5.2, §9.1). */
export function FiscalPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [from, setFrom] = useState<string | null>(`${today.slice(0, 8)}01`);
  const [to, setTo] = useState<string | null>(today);
  const [group, setGroup] = useState<'day' | 'month'>('day');
  const range = { ...(from ? { from } : {}), ...(to ? { to } : {}), group };
  const totals = useFiscalDailyTotals(marinaId, range);
  const file = useFiscalTotalsFile(marinaId);
  const download = (format: ExportFormat) =>
    file.mutate(
      { ...range, format },
      { onSuccess: (b) => saveBlob(`corrispettivi-${from ?? ''}-${to ?? ''}.${format}`, b) },
    );
  return (
    <RequirePermission allowed={can('charges.export')}>
      <PageFrame>
        <PageHeader title={t('fiscal:title')} description={t('fiscal:description')} />
        <Tabs defaultValue="totals">
          <TabsList>
            <TabsTrigger value="totals">{t('fiscal:tabs.totals')}</TabsTrigger>
            <TabsTrigger value="documents">{t('fiscal:tabs.documents')}</TabsTrigger>
          </TabsList>
          <TabsContent value="totals" className="flex flex-col gap-4">
            <div className="flex flex-wrap items-end gap-3">
              <Field className="w-auto">
                <FieldLabel htmlFor="fiscal-from">{t('fiscal:from')}</FieldLabel>
                <DatePicker id="fiscal-from" value={from} onChange={setFrom} />
              </Field>
              <Field className="w-auto">
                <FieldLabel htmlFor="fiscal-to">{t('fiscal:to')}</FieldLabel>
                <DatePicker id="fiscal-to" value={to} onChange={setTo} />
              </Field>
              <EnumSelect
                id="fiscal-group"
                label={t('fiscal:group')}
                values={['day', 'month'] as const}
                labelOf={(g) => t(`fiscal:groups.${g}`)}
                value={group}
                onChange={(g) => g && setGroup(g)}
              />
              <Button variant="outline" disabled={file.isPending} onClick={() => download('csv')}>
                <DownloadIcon />
                {t('fiscal:csv')}
              </Button>
              <Button variant="outline" disabled={file.isPending} onClick={() => download('xlsx')}>
                <DownloadIcon />
                {t('fiscal:xlsx')}
              </Button>
            </div>
            <BillingProblemAlert error={file.error} />
            {totals.isPending ? (
              <LoadingState />
            ) : totals.isError ? (
              <QueryErrorState error={totals.error} onRetry={() => void totals.refetch()} />
            ) : (
              <>
                <SimpleTable
                  head={[
                    t('fiscal:fields.period'),
                    t('fiscal:fields.rate'),
                    t('fiscal:fields.method'),
                    t('fiscal:fields.net'),
                    t('fiscal:fields.vat'),
                    t('fiscal:fields.total'),
                    t('fiscal:fields.documents'),
                  ]}
                  empty={t('fiscal:empty')}
                  rows={totals.data.rows.length}
                >
                  {totals.data.rows.map((r, i) => (
                    <TableRow key={i} data-testid="fiscal-row">
                      <TableCell>{r.period}</TableCell>
                      <TableCell>
                        <VatRate bp={r.vat_rate_bp} nature={r.vat_nature} />
                      </TableCell>
                      <TableCell>{t(`billingEnums:paymentMethod.${r.method}`)}</TableCell>
                      <TableCell>
                        <Money cents={r.net_cents} />
                      </TableCell>
                      <TableCell>
                        <Money cents={r.vat_cents} />
                      </TableCell>
                      <TableCell>
                        <Money cents={r.gross_cents} />
                      </TableCell>
                      <TableCell>{r.documents}</TableCell>
                    </TableRow>
                  ))}
                </SimpleTable>
                <p className="text-sm font-medium" data-testid="fiscal-total">
                  {t('fiscal:grandTotal')} <Money cents={totals.data.total_cents} />
                </p>
              </>
            )}
          </TabsContent>
          <TabsContent value="documents">
            <Documents />
          </TabsContent>
        </Tabs>
      </PageFrame>
    </RequirePermission>
  );
}

function Documents() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [status, setStatus] = useState<FiscalDocument['status'] | ''>('');
  const list = useFiscalDocuments(marinaId, status ? { status } : {});
  const act = useFiscalDocumentAction(marinaId);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <div className="flex flex-col gap-3">
      <EnumSelect
        id="fiscal-status"
        label={t('fiscal:fields.status')}
        values={STATUSES}
        labelOf={(s) => t(`billingEnums:fiscalStatus.${s}`)}
        value={status}
        onChange={setStatus}
        allLabel={t('fiscal:all')}
      />
      <BillingProblemAlert error={act.error} />
      {list.isPending ? (
        <LoadingState />
      ) : list.isError ? (
        <QueryErrorState error={list.error} />
      ) : (
        <SimpleTable
          head={[
            t('fiscal:fields.number'),
            t('fiscal:fields.businessDate'),
            t('fiscal:fields.driver'),
            t('fiscal:fields.total'),
            t('fiscal:fields.status'),
            '',
          ]}
          empty={t('fiscal:noDocuments')}
          rows={items.length}
        >
          {items.map((d) => (
            <TableRow key={d.id} data-testid="fiscal-document">
              <TableCell>{d.number}</TableCell>
              <TableCell>
                <IsoDate date={d.business_date} />
              </TableCell>
              <TableCell>{t(`billingEnums:fiscalDriver.${d.driver}`, d.driver)}</TableCell>
              <TableCell>
                <Money cents={d.total_cents} />
              </TableCell>
              <TableCell>
                <BillingBadge
                  group="fiscalStatus"
                  value={d.status}
                  variant={d.status === 'failed' ? 'destructive' : 'secondary'}
                />
              </TableCell>
              <TableCell className="flex justify-end gap-2">
                {d.status === 'failed' || d.status === 'pending' ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={act.isPending}
                    onClick={() => act.mutate({ id: d.id, action: 'retry' })}
                  >
                    {t('fiscal:retry')}
                  </Button>
                ) : null}
                {d.status === 'issued' ? (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={act.isPending}
                    onClick={() => act.mutate({ id: d.id, action: 'void' })}
                  >
                    {t('fiscal:void')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <LoadMore query={list} />
    </div>
  );
}
