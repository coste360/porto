import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCancelRightTransfer,
  useCompleteRightTransfer,
  useCreateRightTransfer,
  useRequestTransferSignature,
  useRight,
  useRightTransfer,
  useRightTransfers,
  useRights,
  type RightTransfer,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
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
import { DatePicker, MoneyInput, PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
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
import { GeneratedDocuments, RequestSignatureDialog, SignaturePanel } from '../documents/public';
import { useMarinaToday } from '../registry/lib/today';
import { HoldersEditor } from './components/HoldersEditor';
import { defaultTransferDate } from './lib/rights';
import { checkShares, holderRows, toHolderShares, type HolderDraft } from './lib/shares';

const listRoute = getRouteApi('/auth/m/$marinaId/transfers');
const detailRoute = getRouteApi('/auth/m/$marinaId/transfers/$transferId');
const STATUSES: readonly Schemas['TransferStatus'][] = [
  'draft',
  'awaiting_signature',
  'completed',
  'cancelled',
];

function transferVariant(s: Schemas['TransferStatus']) {
  return s === 'completed' ? 'default' : s === 'cancelled' ? 'destructive' : 'secondary';
}

/** Transfers of rights (spec 14.2): list and the transfer wizard. */
export function TransfersPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const search = listRoute.useSearch();
  const [status, setStatus] = useState<Schemas['TransferStatus'] | ''>('');
  const list = useRightTransfers(marinaId, status ? { status } : {});
  const [creating, setCreating] = useState(!!search.right);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader
          title={t('rights:transfers.title')}
          description={t('rights:transfers.description')}
          actions={
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('rights:transfers.new')}
            </Button>
          }
        />
        <EnumSelect
          id="transfer-status"
          label={t('rights:list.status')}
          group="transferStatus"
          values={STATUSES}
          value={status}
          onChange={setStatus}
          allLabel={t('contractsM4:common.all')}
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
                  <TableHead>{t('rights:transfers.effective')}</TableHead>
                  <TableHead>{t('rights:transfers.to')}</TableHead>
                  <TableHead className="text-right">{t('rights:transfers.fee')}</TableHead>
                  <TableHead>{t('rights:list.status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-24 text-center">
                      {t('rights:transfers.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((x) => (
                    <TableRow key={x.id}>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/transfers/$transferId"
                          params={{ marinaId, transferId: x.id }}
                          className="underline-offset-4 hover:underline"
                        >
                          <IsoDate date={x.effective_on} />
                        </Link>
                      </TableCell>
                      <TableCell>
                        {x.to_holders.map((h) => h.customer.display_name ?? '').join(', ')}
                      </TableCell>
                      <TableCell className="text-right">
                        <Money cents={x.takeover_fee_cents} />
                      </TableCell>
                      <TableCell>
                        <Badge variant={transferVariant(x.status)}>
                          {t(`docsEnums:transferStatus.${x.status}`)}
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
        {creating ? (
          <TransferWizard rightId={search.right ?? ''} onClose={() => setCreating(false)} />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function TransferWizard({ rightId, onClose }: { rightId: string; onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const rights = useRights(marinaId, { status: 'active' });
  const create = useCreateRightTransfer(marinaId);
  const [right, setRight] = useState(rightId);
  const [picked, setEffective] = useState<string | null>(null); // null = the default for the right
  const [holders, setHolders] = useState<HolderDraft[]>([{ customer: null, text: '1000' }]);
  const [price, setPrice] = useState<number | null>(null);
  const [policy, setPolicy] = useState<'reassign' | 'prorata' | ''>('reassign');
  const [touched, setTouched] = useState(false);
  const selected = useRight(marinaId, right, !!right);
  const effective = picked ?? defaultTransferDate(today, selected.data?.from_on);
  const all = rights.data?.pages.flatMap((p) => p.items) ?? [];
  const ok = !!right && !!effective && !!policy && checkShares(holderRows(holders)).ok;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('rights:transfers.new')}</DialogTitle>
          <DialogDescription>{t('rights:transfers.wizardDescription')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-3">
          <EnumSelect
            id="transfer-right"
            label={t('rights:transfers.right')}
            values={all.map((r) => r.id)}
            labelOf={(id) => {
              const r = all.find((x) => x.id === id);
              return r
                ? `${r.allocations?.[0]?.resource_code ?? r.berth.resource_id} · ${r.holders.map((h) => h.customer.display_name ?? '').join(', ')}`
                : id;
            }}
            value={right}
            onChange={setRight}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="transfer-effective">{t('rights:transfers.effective')}</FieldLabel>
            <DatePicker id="transfer-effective" value={effective} onChange={setEffective} />
          </Field>
          <EnumSelect
            id="transfer-fees"
            label={t('rights:transfers.feesPolicy')}
            values={['reassign', 'prorata'] as const}
            labelOf={(v) => t(`rights:transfers.feesPolicies.${v}`)}
            value={policy}
            onChange={setPolicy}
          />
          <Field className="w-48">
            <FieldLabel htmlFor="transfer-price">{t('rights:transfers.price')}</FieldLabel>
            <MoneyInput
              id="transfer-price"
              locale={i18n.language}
              value={price}
              onChange={setPrice}
            />
          </Field>
        </div>
        {selected.data ? (
          <Section title={t('rights:transfers.from')}>
            <p className="text-sm">
              {selected.data.holders
                .map((h) => `${h.customer.display_name ?? ''} (${h.share_millis}‰)`)
                .join(', ')}
            </p>
            {selected.data.unpaid_fees ? (
              <p className="text-destructive text-sm" role="alert">
                {t('rights:transfers.requirementsWarning')}
              </p>
            ) : null}
          </Section>
        ) : null}
        <HoldersEditor
          marinaId={marinaId}
          idPrefix="transfer"
          rows={holders}
          onChange={setHolders}
          showErrors={touched}
        />
        {touched && !ok ? <FieldError>{t('rights:wizard.invalid')}</FieldError> : null}
        <M4ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={create.isPending}
            onClick={() => {
              setTouched(true);
              if (!ok || !effective || !policy) return;
              create.mutate(
                {
                  right_id: right,
                  effective_on: effective,
                  to_holders: toHolderShares(holderRows(holders)),
                  fees_policy: policy,
                  ...(price !== null ? { declared_price_cents: price } : {}),
                },
                {
                  onSuccess: (x) => {
                    onClose();
                    void navigate({
                      to: '/m/$marinaId/transfers/$transferId',
                      params: { marinaId, transferId: x.id },
                    });
                  },
                },
              );
            }}
          >
            {t('rights:transfers.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** One transfer: requirements, fee, signers, paper completion or cancel. */
export function TransferPage() {
  const { marinaId, can } = useCurrentMarina();
  const { transferId } = detailRoute.useParams();
  const transfer = useRightTransfer(marinaId, transferId);
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        {transfer.isPending ? (
          <LoadingState rows={6} />
        ) : transfer.isError ? (
          <QueryErrorState error={transfer.error} onRetry={() => void transfer.refetch()} />
        ) : (
          <TransferDetail transfer={transfer.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function TransferDetail({ transfer }: { transfer: RightTransfer }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const request = useRequestTransferSignature(marinaId);
  const complete = useCompleteRightTransfer(marinaId);
  const cancel = useCancelRightTransfer(marinaId);
  const [dialog, setDialog] = useState<'signature' | 'cancel' | null>(null);
  const v = { id: transfer.id, version: transfer.version };
  const open = transfer.status === 'draft' || transfer.status === 'awaiting_signature';
  return (
    <>
      <PageHeader
        title={t('rights:transfer.title')}
        actions={
          open ? (
            <>
              {transfer.status === 'draft' ? (
                <Button variant="outline" onClick={() => setDialog('signature')}>
                  {t('contractsM4:contract.requestSignature')}
                </Button>
              ) : null}
              {transfer.status === 'draft' ? (
                <Button
                  disabled={complete.isPending}
                  onClick={() => complete.mutate({ ...v, body: {} })}
                >
                  {t('rights:transfer.completePaper')}
                </Button>
              ) : null}
              <Button variant="outline" onClick={() => setDialog('cancel')}>
                {t('rights:transfer.cancel')}
              </Button>
            </>
          ) : null
        }
      />
      <M4ProblemAlert error={complete.error ?? cancel.error} />
      <div className="grid gap-4 lg:grid-cols-2">
        <Section title={t('rights:transfer.summary')}>
          <DetailGrid>
            <Detail label={t('rights:list.status')}>
              <Badge variant={transferVariant(transfer.status)}>
                {t(`docsEnums:transferStatus.${transfer.status}`)}
              </Badge>
            </Detail>
            <Detail label={t('rights:transfers.effective')}>
              <IsoDate date={transfer.effective_on} />
            </Detail>
            <Detail label={t('rights:transfers.fee')}>
              <Money cents={transfer.takeover_fee_cents} />
            </Detail>
            {transfer.declared_price_cents !== undefined ? (
              <Detail label={t('rights:transfers.price')}>
                <Money cents={transfer.declared_price_cents} />
              </Detail>
            ) : null}
            <Detail label={t('rights:transfers.feesPolicy')}>
              {t(`rights:transfers.feesPolicies.${transfer.fees_policy}`)}
            </Detail>
            <Detail label={t('rights:transfers.from')}>
              {(transfer.from_holders ?? [])
                .map((h) => `${h.customer.display_name ?? ''} (${h.share_millis}‰)`)
                .join(', ')}
            </Detail>
            <Detail label={t('rights:transfers.to')}>
              {transfer.to_holders
                .map((h) => `${h.customer.display_name ?? ''} (${h.share_millis}‰)`)
                .join(', ')}
            </Detail>
            {transfer.new_right_id ? (
              <Detail label={t('rights:transfer.newRight')}>
                <Link
                  to="/m/$marinaId/rights/$rightId"
                  params={{ marinaId, rightId: transfer.new_right_id }}
                  className="underline-offset-4 hover:underline"
                >
                  {t('rights:transfer.openRight')}
                </Link>
              </Detail>
            ) : null}
          </DetailGrid>
        </Section>
        <Section
          title={t('rights:transfer.requirements')}
          description={t(`rights:transfer.policy.${transfer.requirement_policy}`)}
        >
          {transfer.requirement_issues.length === 0 ? (
            <p className="text-sm">{t('rights:transfer.requirementsOk')}</p>
          ) : (
            <ul className="flex flex-col gap-1 text-sm" role="alert">
              {transfer.requirement_issues.map((i, n) => (
                <li key={n}>
                  {t(`rights:transfer.issues.${i.code}`)} <Money cents={i.amount_cents} />
                </li>
              ))}
            </ul>
          )}
        </Section>
      </div>
      {transfer.signature_request_id ? (
        <Section title={t('signature:list.title')}>
          <SignaturePanel requestId={transfer.signature_request_id} />
        </Section>
      ) : null}
      <Section title={t('templates:docs.title')}>
        <GeneratedDocuments subjectType="right_transfer" subjectId={transfer.id} />
      </Section>
      <RequestSignatureDialog
        open={dialog === 'signature'}
        pending={request.isPending}
        error={request.error}
        onClose={() => setDialog(null)}
        onSubmit={(body) => request.mutate({ ...v, body }, { onSuccess: () => setDialog(null) })}
      />
      <ReasonDialog
        open={dialog === 'cancel'}
        title={t('rights:transfer.cancel')}
        confirmLabel={t('rights:transfer.cancel')}
        destructive
        pending={cancel.isPending}
        error={cancel.error}
        onClose={() => setDialog(null)}
        onConfirm={(reason) =>
          cancel.mutate(
            { ...v, ...(reason ? { reason } : {}) },
            { onSuccess: () => setDialog(null) },
          )
        }
      />
    </>
  );
}
