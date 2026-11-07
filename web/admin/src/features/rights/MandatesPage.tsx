import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { DownloadIcon, PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateMandate,
  useEndMandate,
  useMandate,
  useMandateStatement,
  useMandateStatementExport,
  useMandates,
  useRights,
  type Mandate,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, MoneyInput, PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  LoadMore,
  M4ProblemAlert,
  Money,
  PageFrame,
  ReasonDialog,
  Section,
} from '../contracts-m4/components/kit';
import { openBlob, saveBlob } from '../contracts-m4/lib/files';
import { useMarinaToday } from '../registry/lib/today';
import { mandateBody, type MandateForm } from './lib/mandate';

const detailRoute = getRouteApi('/auth/m/$marinaId/mandates/$mandateId');
const CHANNELS: readonly Schemas['MandateChannel'][] = ['transit', 'period'];

/** Management mandates (spec 14.3): list and the mandate form. */
export function MandatesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const list = useMandates(marinaId);
  const [creating, setCreating] = useState(false);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader
          title={t('rights:mandates.title')}
          description={t('rights:mandates.description')}
          actions={
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('rights:mandates.new')}
            </Button>
          }
        />
        {list.isPending ? (
          <LoadingState rows={4} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('rights:mandates.period')}</TableHead>
                  <TableHead>{t('rights:mandates.share')}</TableHead>
                  <TableHead className="text-right">{t('rights:mandates.revenue')}</TableHead>
                  <TableHead>{t('rights:list.status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-24 text-center">
                      {t('rights:mandates.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((m) => (
                    <TableRow key={m.id}>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/mandates/$mandateId"
                          params={{ marinaId, mandateId: m.id }}
                          className="underline-offset-4 hover:underline"
                        >
                          <IsoDate date={m.from_on} /> <IsoDate date={m.to_on} />
                        </Link>
                      </TableCell>
                      <TableCell>
                        {t('rights:mandates.shareValue', {
                          pct: (m.owner_share_bp / 100).toLocaleString(),
                        })}
                      </TableCell>
                      <TableCell className="text-right">
                        <Money cents={m.revenue_cents ?? 0} />
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {t(`docsEnums:mandateStatus.${m.status}`)}
                        </Badge>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        <LoadMore query={list} />
        {creating ? <MandateDialog onClose={() => setCreating(false)} /> : null}
      </PageFrame>
    </RequirePermission>
  );
}

function MandateDialog({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const rights = useRights(marinaId, { status: 'active' });
  const create = useCreateMandate(marinaId);
  const [f, setF] = useState<MandateForm>({
    rightId: '',
    from: today,
    to: null,
    shareBp: 7000,
    minCents: null,
    channels: ['transit'],
    settlement: 'credit',
  });
  const [touched, setTouched] = useState(false);
  const all = rights.data?.pages.flatMap((p) => p.items) ?? [];
  const mapped = mandateBody(f, today);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('rights:mandates.new')}</DialogTitle>
          <DialogDescription>{t('rights:mandates.formDescription')}</DialogDescription>
        </DialogHeader>
        <EnumSelect
          id="mandate-right"
          label={t('rights:transfers.right')}
          values={all.map((r) => r.id)}
          labelOf={(id) => {
            const r = all.find((x) => x.id === id);
            return r
              ? `${r.allocations?.[0]?.resource_code ?? r.berth.resource_id} · ${r.holders.map((h) => h.customer.display_name ?? '').join(', ')}`
              : id;
          }}
          value={f.rightId}
          onChange={(rightId) => setF({ ...f, rightId })}
        />
        <div className="flex flex-wrap gap-3">
          <Field className="w-auto">
            <FieldLabel htmlFor="mandate-from">{t('rights:mandates.from')}</FieldLabel>
            <DatePicker
              id="mandate-from"
              value={f.from}
              onChange={(from) => setF({ ...f, from })}
            />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="mandate-to">{t('rights:mandates.to')}</FieldLabel>
            <DatePicker id="mandate-to" value={f.to} onChange={(to) => setF({ ...f, to })} />
          </Field>
          <Field className="w-36">
            <FieldLabel htmlFor="mandate-share">{t('rights:mandates.share')}</FieldLabel>
            <PercentInput
              id="mandate-share"
              locale={i18n.language}
              value={f.shareBp}
              onChange={(shareBp) => setF({ ...f, shareBp })}
            />
          </Field>
          <Field className="w-44">
            <FieldLabel htmlFor="mandate-min">{t('rights:mandates.min')}</FieldLabel>
            <MoneyInput
              id="mandate-min"
              locale={i18n.language}
              value={f.minCents}
              onChange={(minCents) => setF({ ...f, minCents })}
            />
          </Field>
        </div>
        <div className="flex flex-wrap items-end gap-4">
          {CHANNELS.map((c) => (
            <Field key={c} orientation="horizontal" className="w-auto">
              <Checkbox
                id={`mandate-ch-${c}`}
                checked={f.channels.includes(c)}
                onCheckedChange={(v) =>
                  setF({
                    ...f,
                    channels: v === true ? [...f.channels, c] : f.channels.filter((x) => x !== c),
                  })
                }
              />
              <FieldLabel htmlFor={`mandate-ch-${c}`}>
                {t(`rights:mandates.channels.${c}`)}
              </FieldLabel>
            </Field>
          ))}
          <EnumSelect
            id="mandate-settlement"
            label={t('rights:mandates.settlement')}
            values={['credit', 'transfer'] as const}
            labelOf={(v) => t(`rights:mandates.settlements.${v}`)}
            value={f.settlement}
            onChange={(settlement) => setF({ ...f, settlement })}
          />
        </div>
        {touched && !mapped.ok ? (
          <FieldError>{t(`rights:mandates.errors.${mapped.error}`)}</FieldError>
        ) : null}
        <M4ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={create.isPending}
            onClick={() => {
              setTouched(true);
              if (mapped.ok)
                create.mutate(mapped.body, {
                  onSuccess: (m) => {
                    onClose();
                    void navigate({
                      to: '/m/$marinaId/mandates/$mandateId',
                      params: { marinaId, mandateId: m.id },
                    });
                  },
                });
            }}
          >
            {t('rights:mandates.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** One mandate: terms, revenue table, statement export, end early or cancel. */
export function MandatePage() {
  const { marinaId, can } = useCurrentMarina();
  const { mandateId } = detailRoute.useParams();
  const mandate = useMandate(marinaId, mandateId);
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        {mandate.isPending ? (
          <LoadingState rows={6} />
        ) : mandate.isError ? (
          <QueryErrorState error={mandate.error} onRetry={() => void mandate.refetch()} />
        ) : (
          <MandateDetail mandate={mandate.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function MandateDetail({ mandate }: { mandate: Mandate }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const statement = useMandateStatement(marinaId, mandate.id);
  const exp = useMandateStatementExport(marinaId);
  const end = useEndMandate(marinaId);
  const [dialog, setDialog] = useState<'end' | 'cancel' | null>(null);
  return (
    <>
      <PageHeader
        title={t('rights:mandate.title')}
        actions={
          <>
            <Button
              variant="outline"
              disabled={exp.isPending}
              onClick={() =>
                exp.mutate(
                  { mandateId: mandate.id, format: 'csv' },
                  { onSuccess: (b) => saveBlob(`mandato-${today}.csv`, b) },
                )
              }
            >
              <DownloadIcon />
              {t('rights:mandate.csv')}
            </Button>
            <Button
              variant="outline"
              disabled={exp.isPending}
              onClick={() =>
                exp.mutate({ mandateId: mandate.id, format: 'pdf' }, { onSuccess: openBlob })
              }
            >
              {t('rights:mandate.pdf')}
            </Button>
            {mandate.status === 'active' ? (
              <>
                <Button variant="outline" onClick={() => setDialog('end')}>
                  {t('rights:mandate.end')}
                </Button>
                <Button variant="outline" onClick={() => setDialog('cancel')}>
                  {t('rights:mandate.cancel')}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      <M4ProblemAlert error={exp.error ?? end.error} />
      <Section title={t('rights:mandate.terms')}>
        <DetailGrid>
          <Detail label={t('rights:mandates.period')}>
            <IsoDate date={mandate.from_on} /> <IsoDate date={mandate.to_on} />
          </Detail>
          <Detail label={t('rights:mandates.share')}>
            {t('rights:mandates.shareValue', {
              pct: (mandate.owner_share_bp / 100).toLocaleString(),
            })}
          </Detail>
          <Detail label={t('rights:mandates.min')}>
            <Money cents={mandate.min_guaranteed_cents} />
          </Detail>
          <Detail label={t('rights:list.status')}>
            {t(`docsEnums:mandateStatus.${mandate.status}`)}
          </Detail>
          <Detail label={t('rights:mandates.channelsLabel')}>
            {mandate.channels.map((c) => t(`rights:mandates.channels.${c}`)).join(', ')}
          </Detail>
          <Detail label={t('rights:mandates.settlement')}>
            {t(`rights:mandates.settlements.${mandate.settlement}`)}
          </Detail>
        </DetailGrid>
      </Section>
      <Section title={t('rights:mandate.revenues')}>
        {statement.isPending ? (
          <LoadingState rows={3} />
        ) : statement.isError ? (
          <QueryErrorState error={statement.error} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('rights:mandate.contract')}</TableHead>
                  <TableHead>{t('rights:mandate.nights')}</TableHead>
                  <TableHead className="text-right">{t('rights:mandates.revenue')}</TableHead>
                  <TableHead className="text-right">{t('rights:mandate.ownerShare')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {statement.data.lines.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-16 text-center">
                      {t('rights:mandate.noRevenue')}
                    </TableCell>
                  </TableRow>
                ) : (
                  statement.data.lines.map((l) => (
                    <TableRow key={l.allocation_id}>
                      <TableCell>{l.contract?.code ?? ''}</TableCell>
                      <TableCell>{l.nights}</TableCell>
                      <TableCell className="text-right">
                        <Money cents={l.revenue_cents} />
                      </TableCell>
                      <TableCell className="text-right">
                        <Money cents={l.owner_share_cents} />
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell colSpan={2}>{t('rights:mandate.total')}</TableCell>
                  <TableCell className="text-right">
                    <Money cents={statement.data.revenue_cents} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money cents={statement.data.owner_share_cents} />
                  </TableCell>
                </TableRow>
                {statement.data.guaranteed_top_up_cents > 0 ? (
                  <TableRow>
                    <TableCell colSpan={3}>{t('rights:mandate.topUp')}</TableCell>
                    <TableCell className="text-right">
                      <Money cents={statement.data.guaranteed_top_up_cents} />
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableFooter>
            </Table>
          </div>
        )}
      </Section>
      <ReasonDialog
        open={dialog !== null}
        title={dialog === 'cancel' ? t('rights:mandate.cancel') : t('rights:mandate.end')}
        description={
          dialog === 'cancel' ? t('rights:mandate.cancelHint') : t('rights:mandate.endHint')
        }
        confirmLabel={dialog === 'cancel' ? t('rights:mandate.cancel') : t('rights:mandate.end')}
        destructive
        pending={end.isPending}
        error={end.error}
        onClose={() => setDialog(null)}
        onConfirm={(reason) =>
          end.mutate(
            {
              id: mandate.id,
              version: mandate.version,
              on: today,
              cancel: dialog === 'cancel',
              ...(reason ? { reason } : {}),
            },
            { onSuccess: () => setDialog(null) },
          )
        }
      />
    </>
  );
}
