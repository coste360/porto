import { Link } from '@tanstack/react-router';
import { DownloadIcon, PlusIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import {
  useCharges,
  useChargesFile,
  type ChargeFilters,
  type ChargeOriginType,
  type ExportFormat,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { CustomerField, type CustomerChoice } from '../../desk/components/CustomerField';
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
} from '../components/kit';
import {
  CHARGE_VIEWS,
  invoicingVariant,
  paymentVariant,
  viewFilters,
  type ChargeView,
} from '../lib/charges';
import { ORIGIN_TYPES } from '../lib/catalog';
import { saveBlob } from '../lib/files';

/** Charges list (m5-plan §9.1): saved views, filters, CSV/XLSX. */
export function ChargesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [view, setView] = useState<ChargeView>('all');
  const [origin, setOrigin] = useState<ChargeOriginType | ''>('');
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [from, setFrom] = useState<string | null>(null);
  const [to, setTo] = useState<string | null>(null);
  const [q, setQ] = useState('');
  const filters: ChargeFilters = {
    ...viewFilters(view),
    ...(origin ? { origin_type: origin } : {}),
    ...(customer ? { customer_id: customer.id } : {}),
    ...(from ? { issued_from: from } : {}),
    ...(to ? { issued_to: to } : {}),
    ...(q.trim() ? { q: q.trim() } : {}),
  };
  const list = useCharges(marinaId, filters);
  const file = useChargesFile(marinaId);
  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data]);
  const download = (format: ExportFormat) =>
    file.mutate(
      { filters, format },
      { onSuccess: (b) => saveBlob(`addebiti-${today}.${format}`, b) },
    );
  return (
    <RequirePermission allowed={can('charges.manage') || can('payments.collect')}>
      <PageFrame>
        <PageHeader
          title={t('charges:list.title')}
          description={t('charges:list.description')}
          actions={
            <>
              <Button variant="outline" disabled={file.isPending} onClick={() => download('csv')}>
                <DownloadIcon />
                {t('charges:list.csv')}
              </Button>
              <Button variant="outline" disabled={file.isPending} onClick={() => download('xlsx')}>
                <DownloadIcon />
                {t('charges:list.xlsx')}
              </Button>
              {can('charges.manage') ? (
                <Button asChild>
                  <Link to="/m/$marinaId/charges/new" params={{ marinaId }}>
                    <PlusIcon />
                    {t('charges:list.new')}
                  </Link>
                </Button>
              ) : null}
            </>
          }
        />
        <ToggleGroup
          type="single"
          variant="outline"
          value={view}
          onValueChange={(v) => v && setView(v as ChargeView)}
          aria-label={t('charges:list.views')}
          className="flex-wrap"
        >
          {CHARGE_VIEWS.map((v) => (
            <ToggleGroupItem key={v} value={v}>
              {t(`charges:views.${v}`)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-48">
            <FieldLabel htmlFor="charges-q">{t('charges:list.search')}</FieldLabel>
            <Input id="charges-q" maxLength={32} value={q} onChange={(e) => setQ(e.target.value)} />
          </Field>
          <EnumSelect
            id="charges-origin"
            label={t('charges:fields.origin')}
            values={ORIGIN_TYPES}
            labelOf={(o) => t(`billingEnums:originType.${o}`)}
            value={origin}
            onChange={setOrigin}
            allLabel={t('charges:list.all')}
          />
          <Field className="w-64">
            <FieldLabel htmlFor="charges-customer">{t('charges:fields.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="charges-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="charges-from">{t('charges:list.issuedFrom')}</FieldLabel>
            <DatePicker id="charges-from" value={from} onChange={setFrom} />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="charges-to">{t('charges:list.issuedTo')}</FieldLabel>
            <DatePicker id="charges-to" value={to} onChange={setTo} />
          </Field>
        </div>
        <BillingProblemAlert error={file.error} />
        {list.isPending ? (
          <LoadingState rows={6} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('charges:fields.number'),
              t('charges:fields.customer'),
              t('charges:fields.origin'),
              t('charges:fields.issuedOn'),
              t('charges:fields.dueOn'),
              t('charges:fields.total'),
              t('charges:fields.residual'),
              t('charges:fields.status'),
            ]}
            empty={t('charges:list.empty')}
            rows={items.length}
          >
            {items.map((c) => (
              <TableRow key={c.id} data-testid="charge-row">
                <TableCell>
                  <Link
                    to="/m/$marinaId/charges/$chargeId"
                    params={{ marinaId, chargeId: c.id }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {c.number}
                  </Link>
                  {c.corrects_charge ? (
                    <Badge variant="outline" className="ml-2">
                      {t('charges:detail.correctionOf', { number: c.corrects_charge.number })}
                    </Badge>
                  ) : null}
                </TableCell>
                <TableCell>{c.customer?.display_name ?? t('charges:list.walkIn')}</TableCell>
                <TableCell>{t(`billingEnums:originType.${c.origin_type}`)}</TableCell>
                <TableCell>
                  <IsoDate date={c.issued_on} />
                </TableCell>
                <TableCell>
                  <IsoDate date={c.due_on} />
                </TableCell>
                <TableCell>
                  <Money cents={c.total_cents} />
                </TableCell>
                <TableCell>
                  <Money cents={c.residual_cents} />
                </TableCell>
                <TableCell className="flex flex-wrap gap-1">
                  <BillingBadge
                    group="paymentStatus"
                    value={c.payment_status}
                    variant={paymentVariant(c)}
                  />
                  <BillingBadge
                    group="invoicingStatus"
                    value={c.invoicing_status}
                    variant={invoicingVariant(c.invoicing_status)}
                  />
                  {c.overdue ? (
                    <Badge variant="destructive">{t('charges:list.overdue')}</Badge>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        <LoadMore query={list} />
      </PageFrame>
    </RequirePermission>
  );
}
