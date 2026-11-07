import { useState, type ReactNode } from 'react';

import {
  useAcceptRenewalProposal,
  useContractInstallments,
  useDeclineRenewalProposal,
  useEntityAudit,
  useRenewContract,
  useRenewalProposal,
  useReplanInstallments,
  useRequestContractSignature,
  useResumeContract,
  useSendRenewalProposal,
  useSettleInstallment,
  useSuspendContract,
  type Contract,
  type Installment,
  type PlanChoice,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
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
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { GeneratedDocuments, RequestSignatureDialog, SignaturePanel } from '../../documents/public';
import { proposalActions, proposalVariant } from '../lib/campaign';
import { CONTRACT_TABS, type ContractTab } from '../lib/tabs';
import { AddendaPanel } from './AddendaPanel';
import {
  Detail,
  DetailGrid,
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  ReasonDialog,
  Section,
} from './kit';
import { PlanPreview } from './PlanPreview';
import { SettleDialog } from './SettleDialog';

/** M4 header actions of a mooring/charter contract: request signature, suspend and resume (§3.1). */
export function ContractM4Actions({ contract }: { contract: Contract }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [dialog, setDialog] = useState<'signature' | 'suspend' | null>(null);
  const request = useRequestContractSignature(marinaId);
  const suspend = useSuspendContract(marinaId);
  const resume = useResumeContract(marinaId);
  if (!can('contracts.write')) return null;
  return (
    <>
      {contract.status === 'draft' && !contract.signature_request_id ? (
        <Button variant="outline" onClick={() => setDialog('signature')}>
          {t('contractsM4:contract.requestSignature')}
        </Button>
      ) : null}
      {contract.status === 'active' ? (
        <Button variant="outline" onClick={() => setDialog('suspend')}>
          {t('contractsM4:contract.suspend')}
        </Button>
      ) : null}
      {contract.status === 'suspended' ? (
        <Button
          variant="outline"
          disabled={resume.isPending}
          onClick={() => resume.mutate({ id: contract.id, version: contract.version })}
        >
          {t('contractsM4:contract.resume')}
        </Button>
      ) : null}
      <RequestSignatureDialog
        open={dialog === 'signature'}
        pending={request.isPending}
        error={request.error}
        onClose={() => setDialog(null)}
        onSubmit={(body) =>
          request.mutate(
            { id: contract.id, version: contract.version, body },
            { onSuccess: () => setDialog(null) },
          )
        }
      />
      <ReasonDialog
        open={dialog === 'suspend'}
        title={t('contractsM4:contract.suspendTitle')}
        description={t('contractsM4:contract.suspendDescription')}
        confirmLabel={t('contractsM4:contract.suspend')}
        required
        destructive
        pending={suspend.isPending}
        error={suspend.error ?? resume.error}
        onClose={() => setDialog(null)}
        onConfirm={(reason) =>
          suspend.mutate(
            { id: contract.id, version: contract.version, reason },
            { onSuccess: () => setDialog(null) },
          )
        }
      />
    </>
  );
}

/** Badges of the M4 contract fields (suspension, signature, renewal, termination amount). */
export function ContractM4Badges({ contract }: { contract: Contract }) {
  const { t, i18n } = useT();
  return (
    <>
      {contract.suspended_at ? (
        <Badge variant="destructive">
          {t('contractsM4:contract.suspendedSince', {
            at: formatDateTime(contract.suspended_at, i18n.language),
          })}
        </Badge>
      ) : null}
      {contract.signed_at ? (
        <span>
          {t('contractsM4:contract.signedAt', {
            at: formatDateTime(contract.signed_at, i18n.language),
          })}
        </span>
      ) : null}
      {contract.termination_amount_cents !== undefined &&
      contract.termination_amount_cents !== 0 ? (
        <span>
          {t(
            contract.termination_amount_cents < 0
              ? 'contractsM4:contract.refund'
              : 'contractsM4:contract.penalty',
          )}{' '}
          <Money cents={Math.abs(contract.termination_amount_cents)} />
        </span>
      ) : null}
      {(contract.warnings ?? []).map((w) => (
        <Badge key={w} variant="outline">
          {t(`contractsM4:warnings.${w}`)}
        </Badge>
      ))}
    </>
  );
}

/** Tabs of the contract page (m4-plan §8.1): the M3 data cards go in `data`. */
export function ContractM4Tabs({
  contract,
  data,
  tab,
  onTab,
}: {
  contract: Contract;
  data: ReactNode;
  tab: ContractTab;
  onTab: (t: ContractTab) => void;
}) {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <Tabs value={tab} onValueChange={(v) => onTab(v as ContractTab)}>
      <TabsList className="flex-wrap">
        {CONTRACT_TABS.filter((x) => x !== 'history' || can('audit.read')).map((x) => (
          <TabsTrigger key={x} value={x}>
            {t(`contractsM4:tabs.${x}`)}
          </TabsTrigger>
        ))}
      </TabsList>
      <TabsContent value="data">{data}</TabsContent>
      <TabsContent value="installments">
        <InstallmentsTab contract={contract} />
      </TabsContent>
      <TabsContent value="documents">
        <GeneratedDocuments subjectType="contract" subjectId={contract.id} />
      </TabsContent>
      <TabsContent value="signature">
        {contract.signature_request_id ? (
          <SignaturePanel requestId={contract.signature_request_id} />
        ) : (
          <p className="text-muted-foreground text-sm">{t('contractsM4:contract.noSignature')}</p>
        )}
      </TabsContent>
      <TabsContent value="addenda">
        <AddendaPanel contract={contract} />
      </TabsContent>
      <TabsContent value="renewal">
        <RenewalTab contract={contract} />
      </TabsContent>
      <TabsContent value="history">
        {tab === 'history' ? <HistoryTab contractId={contract.id} /> : null}
      </TabsContent>
    </Tabs>
  );
}

function InstallmentsTab({ contract }: { contract: Contract }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const plan = useContractInstallments(marinaId, contract.id);
  const replan = useReplanInstallments(marinaId);
  const settle = useSettleInstallment(marinaId);
  const [settling, setSettling] = useState<Installment | null>(null);
  if (plan.isPending) return <LoadingState rows={3} />;
  if (plan.isError)
    return <QueryErrorState error={plan.error} onRetry={() => void plan.refetch()} />;
  const p = plan.data;
  const early = p.alternatives?.early_payment;
  const canSettle = can('payments.collect');
  const replannable =
    can('contracts.write') && ['draft', 'awaiting_signature', 'active'].includes(contract.status);
  return (
    <div className="flex flex-col gap-4">
      <Section title={t('contractsM4:plan.title', { version: p.plan_version })}>
        <PlanPreview
          items={p.items}
          totalCents={p.total_cents}
          sumCents={p.sum_cents}
          caption={t('contractsM4:plan.title', { version: p.plan_version })}
          actions={(row) => {
            const inst = p.items.find((i) => i.seq === row.seq && i.due_on === row.due_on);
            return inst &&
              canSettle &&
              (inst.status === 'issued' || inst.status === 'scheduled') ? (
              <Button size="sm" variant="outline" onClick={() => setSettling(inst)}>
                {t('contractsM4:settle.open')}
              </Button>
            ) : null;
          }}
        />
      </Section>
      {early ? (
        <Section
          title={t('contractsM4:plan.earlyTitle')}
          description={t('contractsM4:plan.earlyDescription')}
          action={
            replannable ? (
              <Button
                variant="outline"
                disabled={replan.isPending}
                onClick={() =>
                  replan.mutate({
                    id: contract.id,
                    version: contract.version,
                    body: { plan: 'early_payment' },
                  })
                }
              >
                {t('contractsM4:plan.chooseEarly')}
              </Button>
            ) : undefined
          }
        >
          <p className="text-sm">
            {t('contractsM4:plan.discount')} <Money cents={early.discount_cents} />
          </p>
          <PlanPreview items={early.items} totalCents={early.total_cents} />
        </Section>
      ) : null}
      <M4ProblemAlert error={replan.error} />
      {settling ? (
        <SettleDialog
          open
          items={[settling]}
          totalCents={settling.amount_cents}
          pending={settle.isPending}
          error={settle.error}
          onClose={() => setSettling(null)}
          onSettle={(body) =>
            settle.mutate(
              {
                id: settling.id,
                version: settling.version,
                body: {
                  method: body.method,
                  ...(body.reference ? { reference: body.reference } : {}),
                  ...(body.settled_on ? { settled_on: body.settled_on } : {}),
                },
              },
              { onSuccess: () => setSettling(null) },
            )
          }
        />
      ) : null}
    </div>
  );
}

function RenewalTab({ contract }: { contract: Contract }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [open, setOpen] = useState(false);
  if (contract.renewal_proposal_id) {
    return <ProposalCard marinaId={marinaId} proposalId={contract.renewal_proposal_id} />;
  }
  return (
    <div className="flex flex-col gap-3">
      <p className="text-muted-foreground text-sm">{t('renewals:simple.none')}</p>
      {can('contracts.write') && contract.status === 'active' ? (
        <div>
          <Button onClick={() => setOpen(true)}>{t('renewals:simple.renew')}</Button>
        </div>
      ) : null}
      {open ? <RenewDialog contract={contract} onClose={() => setOpen(false)} /> : null}
    </div>
  );
}

function RenewDialog({ contract, onClose }: { contract: Contract; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const renew = useRenewContract(marinaId);
  const [startsOn, setStartsOn] = useState<string | null>(contract.ends_on);
  const [deadline, setDeadline] = useState<string | null>(null);
  const [send, setSend] = useState(true);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('renewals:simple.title')}</DialogTitle>
          <DialogDescription>{t('renewals:simple.description')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="renew-start">{t('renewals:simple.startsOn')}</FieldLabel>
          <DatePicker id="renew-start" value={startsOn} onChange={setStartsOn} />
        </Field>
        <Field>
          <FieldLabel htmlFor="renew-deadline">{t('renewals:simple.deadline')}</FieldLabel>
          <DatePicker id="renew-deadline" value={deadline} onChange={setDeadline} />
        </Field>
        <Field orientation="horizontal">
          <Switch id="renew-send" checked={send} onCheckedChange={setSend} />
          <FieldLabel htmlFor="renew-send">{t('renewals:simple.send')}</FieldLabel>
        </Field>
        <M4ProblemAlert error={renew.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={renew.isPending}
            onClick={() =>
              renew.mutate(
                {
                  id: contract.id,
                  version: contract.version,
                  body: {
                    ...(startsOn ? { starts_on: startsOn } : {}),
                    ...(deadline ? { deadline_on: deadline } : {}),
                    send,
                  },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('renewals:simple.renew')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** One renewal proposal with the office actions (resend, accept on behalf, decline). */
export function ProposalCard({ marinaId, proposalId }: { marinaId: string; proposalId: string }) {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const proposal = useRenewalProposal(marinaId, proposalId);
  const send = useSendRenewalProposal(marinaId);
  const accept = useAcceptRenewalProposal(marinaId);
  const decline = useDeclineRenewalProposal(marinaId);
  const [plan, setPlan] = useState<PlanChoice | ''>('');
  const [declining, setDeclining] = useState(false);
  if (proposal.isPending) return <LoadingState rows={2} />;
  if (proposal.isError)
    return <QueryErrorState error={proposal.error} onRetry={() => void proposal.refetch()} />;
  const p = proposal.data;
  const acts = proposalActions(p);
  const write = can('contracts.write');
  const v = { id: p.id, version: p.version };
  return (
    <Section title={t('renewals:proposal.title')}>
      <DetailGrid>
        <Detail label={t('renewals:proposal.status')}>
          <Badge variant={proposalVariant(p.status)}>
            {t(`docsEnums:proposalStatus.${p.status}`)}
          </Badge>
        </Detail>
        <Detail label={t('renewals:proposal.oldTotal')}>
          <Money cents={p.old_total_cents} />
        </Detail>
        <Detail label={t('renewals:proposal.newTotal')}>
          <Money cents={p.new_total_cents} />
        </Detail>
        <Detail label={t('renewals:proposal.period')}>
          <IsoDate date={p.new_starts_on} /> <IsoDate date={p.new_ends_on} />
        </Detail>
        <Detail label={t('renewals:proposal.deadline')}>
          <IsoDate date={p.deadline_on} />
        </Detail>
        {p.exclusion_reason ? (
          <Detail label={t('renewals:proposal.exclusion')}>
            {t(`docsEnums:exclusionReason.${p.exclusion_reason}`)}
          </Detail>
        ) : null}
      </DetailGrid>
      <M4ProblemAlert error={send.error ?? accept.error ?? decline.error} />
      {write ? (
        <div className="flex flex-wrap items-end gap-2">
          {acts.resend ? (
            <Button variant="outline" disabled={send.isPending} onClick={() => send.mutate(v)}>
              {t('renewals:proposal.resend')}
            </Button>
          ) : null}
          {acts.accept ? (
            <>
              <EnumSelect
                id="proposal-plan"
                label={t('renewals:proposal.plan')}
                group="planChoice"
                values={['standard', 'early_payment'] as const}
                value={plan}
                onChange={setPlan}
              />
              <Button
                disabled={accept.isPending}
                onClick={() => accept.mutate({ ...v, ...(plan ? { plan } : {}) })}
              >
                {t('renewals:proposal.accept')}
              </Button>
            </>
          ) : null}
          {acts.decline ? (
            <Button variant="outline" onClick={() => setDeclining(true)}>
              {t('renewals:proposal.decline')}
            </Button>
          ) : null}
        </div>
      ) : null}
      <ReasonDialog
        open={declining}
        title={t('renewals:proposal.declineTitle')}
        confirmLabel={t('renewals:proposal.decline')}
        destructive
        pending={decline.isPending}
        error={decline.error}
        onClose={() => setDeclining(false)}
        onConfirm={(reason) =>
          decline.mutate(
            { ...v, ...(reason ? { reason } : {}) },
            { onSuccess: () => setDeclining(false) },
          )
        }
      />
    </Section>
  );
}

function HistoryTab({ contractId }: { contractId: string }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const audit = useEntityAudit(marinaId, 'contract', contractId);
  if (audit.isPending) return <LoadingState rows={3} />;
  if (audit.isError)
    return <QueryErrorState error={audit.error} onRetry={() => void audit.refetch()} />;
  return audit.data.items.length === 0 ? (
    <p className="text-muted-foreground text-sm">{t('contractsM4:history.empty')}</p>
  ) : (
    <ol className="flex flex-col gap-2">
      {audit.data.items.map((e) => (
        <li key={e.id} className="flex flex-wrap gap-3 rounded-md border p-2 text-sm">
          <span className="tabular-nums">{formatDateTime(e.created_at, i18n.language)}</span>
          <span className="font-medium">{e.action}</span>
        </li>
      ))}
    </ol>
  );
}
