import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { DownloadIcon } from 'lucide-react';
import { useReducer, useState } from 'react';
import { toast } from 'sonner';

import {
  useAcceptRenewalProposal,
  useBerthGroups,
  useCampaignProposals,
  useCampaignProposalsCsv,
  useCloseRenewalCampaign,
  useCreateRenewalCampaign,
  useDeclineRenewalProposal,
  useDiscardRenewalCampaign,
  useIncludeRenewalProposal,
  useLaunchRenewalCampaign,
  usePreviewRenewalCampaign,
  usePriceLists,
  useRenewalCampaign,
  useSendRenewalProposal,
  useUpdateRenewalCampaign,
  type RenewalCampaign,
  type RenewalProposal,
  type RenewalProposalStatus,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldError, FieldLabel, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { localText } from '../pricing/lib/enums';
import { useMarinaToday } from '../registry/lib/today';
import {
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  PageFrame,
  ReasonDialog,
  StepBar,
} from './components/kit';
import { saveBlob } from './lib/files';
import {
  PROPOSAL_STATES,
  WIZARD_STEPS,
  campaignCounts,
  campaignInput,
  campaignPatch,
  canInclude,
  initialWizard,
  previewTotals,
  proposalActions,
  proposalVariant,
  stepError,
  wizardReducer,
  type WizardState,
} from './lib/campaign';

const route = getRouteApi('/auth/m/$marinaId/renewals/$campaignId');
const TERMS: readonly Schemas['ContractTerm'][] = ['annual', 'seasonal', 'monthly', 'period'];

function addDays(iso: string, days: number): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** A campaign: the wizard while it is a draft (or new), then the monitor. */
export function RenewalCampaignPage() {
  const { can, marinaId } = useCurrentMarina();
  const { campaignId } = route.useParams();
  const isNew = campaignId === 'new';
  const campaign = useRenewalCampaign(marinaId, campaignId, !isNew);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        {isNew ? (
          <CampaignWizard campaign={null} />
        ) : campaign.isPending ? (
          <LoadingState rows={6} />
        ) : campaign.isError ? (
          <QueryErrorState error={campaign.error} onRetry={() => void campaign.refetch()} />
        ) : campaign.data.status === 'draft' && can('contracts.write') ? (
          <CampaignWizard campaign={campaign.data} />
        ) : (
          <CampaignMonitor campaign={campaign.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

// ---- wizard: 1 filter → 2 preview → 3 message & deadline → 4 launch ----

function CampaignWizard({ campaign }: { campaign: RenewalCampaign | null }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const [s, dispatch] = useReducer(wizardReducer, campaign, (c) =>
    initialWizard(addDays(today, 30), c),
  );
  const [touched, setTouched] = useState(false);
  const create = useCreateRenewalCampaign(marinaId);
  const update = useUpdateRenewalCampaign(marinaId);
  const preview = usePreviewRenewalCampaign(marinaId);
  const launch = useLaunchRenewalCampaign(marinaId);
  const discard = useDiscardRenewalCampaign(marinaId);
  const error = stepError(s, today);
  const busy = create.isPending || update.isPending || preview.isPending || launch.isPending;

  const runPreview = async () => {
    let c = s.campaign;
    if (!c) {
      c = await create.mutateAsync(campaignInput(s));
      dispatch({ type: 'saved', campaign: c });
    } else {
      c = await update.mutateAsync({ id: c.id, version: c.version, patch: campaignPatch(s) });
      dispatch({ type: 'saved', campaign: c });
    }
    const p = await preview.mutateAsync({ id: c.id, version: c.version });
    dispatch({ type: 'previewed', preview: p });
  };

  const next = async () => {
    setTouched(true);
    if (s.step === 'filter') {
      if (error) return;
      try {
        await runPreview();
        setTouched(false);
      } catch {
        // shown by the alerts below
      }
      return;
    }
    if (s.step === 'preview' && s.stale) {
      await runPreview().catch(() => undefined);
      return;
    }
    if (error) return;
    if (s.step === 'message' && s.campaign) {
      const c = await update
        .mutateAsync({ id: s.campaign.id, version: s.campaign.version, patch: campaignPatch(s) })
        .catch(() => null);
      if (!c) return;
      dispatch({ type: 'saved', campaign: c });
    }
    setTouched(false);
    dispatch({ type: 'next' });
  };

  return (
    <>
      <PageHeader
        title={s.campaign ? s.campaign.name : t('renewals:wizard.title')}
        description={t('renewals:wizard.description')}
        actions={
          s.campaign ? (
            <Button
              variant="outline"
              disabled={discard.isPending}
              onClick={() =>
                s.campaign &&
                discard.mutate(
                  { id: s.campaign.id, version: s.campaign.version },
                  {
                    onSuccess: () =>
                      void navigate({ to: '/m/$marinaId/renewals', params: { marinaId } }),
                  },
                )
              }
            >
              {t('renewals:wizard.discard')}
            </Button>
          ) : null
        }
      />
      <StepBar
        steps={WIZARD_STEPS}
        current={s.step}
        label={t('renewals:wizard.steps')}
        labelOf={(x) => t(`renewals:wizard.step.${x}`)}
        onSelect={(step) => dispatch({ type: 'goto', step })}
      />
      {s.step === 'filter' ? <FilterStep s={s} dispatch={dispatch} /> : null}
      {s.step === 'preview' && s.preview ? <PreviewStep s={s} dispatch={dispatch} /> : null}
      {s.step === 'message' ? <MessageStep s={s} dispatch={dispatch} /> : null}
      {s.step === 'launch' ? <LaunchStep s={s} /> : null}
      {touched && error && error !== 'launched' ? (
        <FieldError>{t(`renewals:wizard.errors.${error}`)}</FieldError>
      ) : null}
      <M4ProblemAlert
        error={create.error ?? update.error ?? preview.error ?? launch.error ?? discard.error}
      />
      <div className="flex flex-wrap gap-2">
        {s.step !== 'filter' ? (
          <Button variant="outline" onClick={() => dispatch({ type: 'back' })}>
            {t('renewals:wizard.back')}
          </Button>
        ) : null}
        {s.step === 'launch' ? (
          <Button
            disabled={busy || !s.campaign}
            onClick={() =>
              s.campaign &&
              launch.mutate(
                { id: s.campaign.id, version: s.campaign.version },
                {
                  onSuccess: (c) => {
                    toast.success(t('renewals:wizard.launched'));
                    dispatch({ type: 'saved', campaign: c });
                    // The monitor replaces the wizard (a launched campaign is no longer a draft).
                    void navigate({
                      to: '/m/$marinaId/renewals/$campaignId',
                      params: { marinaId, campaignId: c.id },
                      replace: true,
                    });
                  },
                },
              )
            }
          >
            {t('renewals:wizard.launch')}
          </Button>
        ) : (
          <Button disabled={busy} onClick={() => void next()}>
            {s.step === 'filter' || s.stale
              ? t('renewals:wizard.preview')
              : t('renewals:wizard.next')}
          </Button>
        )}
      </div>
    </>
  );
}

type StepProps = { s: WizardState; dispatch: (a: Parameters<typeof wizardReducer>[1]) => void };

function FilterStep({ s, dispatch }: StepProps) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const groups = useBerthGroups(marinaId);
  const lists = usePriceLists(marinaId, { status: 'active' });
  const terms = s.filter.terms ?? [];
  const groupIds = s.filter.groups ?? [];
  const toggle = <T,>(list: readonly T[], v: T) =>
    list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
  return (
    <div className="flex flex-col gap-4">
      <Field>
        <FieldLabel htmlFor="campaign-name">{t('renewals:wizard.name')}</FieldLabel>
        <Input
          id="campaign-name"
          value={s.name}
          maxLength={120}
          onChange={(e) => dispatch({ type: 'name', value: e.target.value })}
        />
      </Field>
      <FieldSet>
        <FieldLegend variant="label">{t('renewals:wizard.terms')}</FieldLegend>
        <div className="flex flex-wrap gap-4">
          {TERMS.map((term) => (
            <Field key={term} orientation="horizontal" className="w-auto">
              <Checkbox
                id={`campaign-term-${term}`}
                checked={terms.includes(term)}
                onCheckedChange={() =>
                  dispatch({ type: 'filter', patch: { terms: toggle(terms, term) } })
                }
              />
              <FieldLabel htmlFor={`campaign-term-${term}`}>
                {t(`occupancyEnums:contractTerm.${term}`)}
              </FieldLabel>
            </Field>
          ))}
        </div>
      </FieldSet>
      {(groups.data ?? []).length > 0 ? (
        <FieldSet>
          <FieldLegend variant="label">{t('renewals:wizard.groups')}</FieldLegend>
          <div className="flex flex-wrap gap-4">
            {(groups.data ?? []).map((g) => (
              <Field key={g.id} orientation="horizontal" className="w-auto">
                <Checkbox
                  id={`campaign-group-${g.id}`}
                  checked={groupIds.includes(g.id)}
                  onCheckedChange={() =>
                    dispatch({ type: 'filter', patch: { groups: toggle(groupIds, g.id) } })
                  }
                />
                <FieldLabel htmlFor={`campaign-group-${g.id}`}>
                  {localText(g.name, i18n.language) || g.code}
                </FieldLabel>
              </Field>
            ))}
          </div>
        </FieldSet>
      ) : null}
      <div className="flex flex-wrap gap-3">
        <Field className="w-auto">
          <FieldLabel htmlFor="campaign-ends-from">{t('renewals:wizard.endsFrom')}</FieldLabel>
          <DatePicker
            id="campaign-ends-from"
            value={s.filter.ends_from ?? null}
            onChange={(v) => dispatch({ type: 'filter', patch: { ends_from: v ?? undefined } })}
          />
        </Field>
        <Field className="w-auto">
          <FieldLabel htmlFor="campaign-ends-to">{t('renewals:wizard.endsTo')}</FieldLabel>
          <DatePicker
            id="campaign-ends-to"
            value={s.filter.ends_to ?? null}
            onChange={(v) => dispatch({ type: 'filter', patch: { ends_to: v ?? undefined } })}
          />
        </Field>
        <EnumSelect
          id="campaign-price-list"
          label={t('renewals:wizard.priceList')}
          values={(lists.data?.items ?? []).map((l) => l.id)}
          labelOf={(id) => {
            const l = lists.data?.items.find((x) => x.id === id);
            return l ? `${l.code} · ${localText(l.name, i18n.language)}` : id;
          }}
          value={s.priceListId}
          onChange={(v) => dispatch({ type: 'priceList', value: v })}
          allLabel={t('renewals:wizard.priceListAuto')}
        />
      </div>
    </div>
  );
}

function PreviewStep({ s, dispatch }: StepProps) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const include = useIncludeRenewalProposal(marinaId);
  const [including, setIncluding] = useState<RenewalProposal | null>(null);
  const items = s.preview?.proposals.items ?? [];
  const totals = previewTotals(items);
  return (
    <div className="flex flex-col gap-3">
      {s.stale ? (
        <p className="text-sm" role="status">
          {t('renewals:wizard.stale')}
        </p>
      ) : null}
      <div className="grid gap-2 sm:grid-cols-4" data-testid="preview-totals">
        <Stat label={t('renewals:preview.included')} value={String(totals.included)} />
        <Stat label={t('renewals:preview.excluded')} value={String(totals.excluded)} />
        <Stat label={t('renewals:preview.oldTotal')} value={<Money cents={totals.oldCents} />} />
        <Stat label={t('renewals:preview.newTotal')} value={<Money cents={totals.newCents} />} />
      </div>
      <ProposalsTable
        items={items}
        renderAction={(p) =>
          canInclude(p) ? (
            <Button size="sm" variant="outline" onClick={() => setIncluding(p)}>
              {t('renewals:preview.include')}
            </Button>
          ) : null
        }
      />
      <ReasonDialog
        open={including !== null}
        title={t('renewals:preview.includeTitle')}
        description={t('renewals:preview.includeDescription')}
        confirmLabel={t('renewals:preview.include')}
        required
        pending={include.isPending}
        error={include.error}
        onClose={() => setIncluding(null)}
        onConfirm={(reason) =>
          including &&
          include.mutate(
            { id: including.id, version: including.version, reason },
            {
              onSuccess: (p) => {
                dispatch({ type: 'proposalChanged', proposal: p });
                setIncluding(null);
              },
            },
          )
        }
      />
    </div>
  );
}

function MessageStep({ s, dispatch }: StepProps) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-4">
      <Field className="w-auto">
        <FieldLabel htmlFor="campaign-deadline">{t('renewals:wizard.deadline')}</FieldLabel>
        <DatePicker
          id="campaign-deadline"
          value={s.deadlineOn || null}
          onChange={(v) => dispatch({ type: 'deadline', value: v ?? '' })}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="campaign-message">{t('renewals:wizard.message')}</FieldLabel>
        <Textarea
          id="campaign-message"
          rows={6}
          maxLength={4000}
          value={s.message}
          onChange={(e) => dispatch({ type: 'message', value: e.target.value })}
        />
      </Field>
    </div>
  );
}

function LaunchStep({ s }: { s: WizardState }) {
  const { t } = useT();
  const totals = previewTotals(s.preview?.proposals.items ?? []);
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p>{t('renewals:wizard.summary', { count: totals.included, excluded: totals.excluded })}</p>
      <p>
        {t('renewals:wizard.deadline')} <IsoDate date={s.deadlineOn} />
      </p>
      {s.campaign && s.campaign.status !== 'draft' ? (
        <p role="status">{t(`docsEnums:campaignStatus.${s.campaign.status}`)}</p>
      ) : null}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-md border p-3">
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="text-lg font-semibold">{value}</div>
    </div>
  );
}

/** Proposals with old/new price and the exclusion reason (preview and monitor). */
export function ProposalsTable({
  items,
  renderAction,
}: {
  items: readonly RenewalProposal[];
  renderAction?: (p: RenewalProposal) => React.ReactNode;
}) {
  const { t } = useT();
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('renewals:preview.contract')}</TableHead>
            <TableHead>{t('renewals:preview.customer')}</TableHead>
            <TableHead>{t('renewals:preview.vessel')}</TableHead>
            <TableHead className="text-right">{t('renewals:preview.oldPrice')}</TableHead>
            <TableHead className="text-right">{t('renewals:preview.newPrice')}</TableHead>
            <TableHead>{t('renewals:preview.status')}</TableHead>
            {renderAction ? <TableHead /> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.length === 0 ? (
            <TableRow>
              <TableCell colSpan={7} className="text-muted-foreground h-24 text-center">
                {t('renewals:preview.empty')}
              </TableCell>
            </TableRow>
          ) : (
            items.map((p) => (
              <TableRow key={p.id} data-testid="proposal-row" data-status={p.status}>
                <TableCell>{p.source_contract.code}</TableCell>
                <TableCell>{p.customer?.display_name ?? ''}</TableCell>
                <TableCell>{p.vessel?.name ?? ''}</TableCell>
                <TableCell className="text-right">
                  <Money cents={p.old_total_cents} />
                </TableCell>
                <TableCell className="text-right">
                  <Money cents={p.new_total_cents} />
                </TableCell>
                <TableCell className="flex flex-wrap gap-1">
                  <Badge variant={proposalVariant(p.status)}>
                    {t(`docsEnums:proposalStatus.${p.status}`)}
                  </Badge>
                  {p.exclusion_reason ? (
                    <Badge variant="outline">
                      {t(`docsEnums:exclusionReason.${p.exclusion_reason}`)}
                    </Badge>
                  ) : null}
                </TableCell>
                {renderAction ? (
                  <TableCell className="text-right">{renderAction(p)}</TableCell>
                ) : null}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}

// ---- monitor ----

function CampaignMonitor({ campaign }: { campaign: RenewalCampaign }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const [status, setStatus] = useState<RenewalProposalStatus | ''>('');
  const proposals = useCampaignProposals(marinaId, campaign.id, status || undefined);
  const csv = useCampaignProposalsCsv(marinaId);
  const close = useCloseRenewalCampaign(marinaId);
  const send = useSendRenewalProposal(marinaId);
  const accept = useAcceptRenewalProposal(marinaId);
  const decline = useDeclineRenewalProposal(marinaId);
  const [declining, setDeclining] = useState<RenewalProposal | null>(null);
  const counts = campaignCounts(campaign.counts as Record<string, unknown>);
  const items = proposals.data?.pages.flatMap((p) => p.items) ?? [];
  const write = can('contracts.write');
  return (
    <>
      <PageHeader
        title={campaign.name}
        description={t('renewals:monitor.description')}
        actions={
          <>
            <Button
              variant="outline"
              disabled={csv.isPending}
              onClick={() =>
                csv.mutate(campaign.id, { onSuccess: (b) => saveBlob(`rinnovi-${today}.csv`, b) })
              }
            >
              <DownloadIcon />
              {t('renewals:monitor.csv')}
            </Button>
            {write && campaign.status === 'open' ? (
              <Button
                variant="outline"
                disabled={close.isPending}
                onClick={() => close.mutate({ id: campaign.id, version: campaign.version })}
              >
                {t('renewals:monitor.close')}
              </Button>
            ) : null}
          </>
        }
      />
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant="secondary">{t(`docsEnums:campaignStatus.${campaign.status}`)}</Badge>
        <span>
          {t('renewals:list.deadline')} <IsoDate date={campaign.deadline_on} />
        </span>
      </div>
      <section
        aria-label={t('renewals:monitor.counts')}
        className="grid gap-2 sm:grid-cols-3 lg:grid-cols-5"
        aria-live="polite"
      >
        {PROPOSAL_STATES.filter((st) => st !== 'previewed').map((st) => (
          <button
            key={st}
            type="button"
            className="hover:bg-muted rounded-md border p-3 text-left"
            aria-pressed={status === st}
            data-testid={`count-${st}`}
            onClick={() => setStatus(status === st ? '' : st)}
          >
            <div className="text-muted-foreground text-xs">
              {t(`docsEnums:proposalStatus.${st}`)}
            </div>
            <div className="text-lg font-semibold tabular-nums">{counts[st] ?? 0}</div>
          </button>
        ))}
      </section>
      <M4ProblemAlert error={close.error ?? send.error ?? accept.error ?? decline.error} />
      {proposals.isPending ? (
        <LoadingState rows={6} />
      ) : proposals.isError ? (
        <QueryErrorState error={proposals.error} onRetry={() => void proposals.refetch()} />
      ) : (
        <ProposalsTable
          items={items}
          renderAction={
            write
              ? (p) => {
                  const a = proposalActions(p);
                  const v = { id: p.id, version: p.version };
                  return (
                    <div className="flex justify-end gap-1">
                      {a.resend ? (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={send.isPending}
                          onClick={() => send.mutate(v)}
                        >
                          {t('renewals:proposal.resend')}
                        </Button>
                      ) : null}
                      {a.accept ? (
                        <Button
                          size="sm"
                          disabled={accept.isPending}
                          onClick={() => accept.mutate(v)}
                        >
                          {t('renewals:proposal.acceptOnBehalf')}
                        </Button>
                      ) : null}
                      {a.decline ? (
                        <Button size="sm" variant="outline" onClick={() => setDeclining(p)}>
                          {t('renewals:proposal.decline')}
                        </Button>
                      ) : null}
                    </div>
                  );
                }
              : undefined
          }
        />
      )}
      <ReasonDialog
        open={declining !== null}
        title={t('renewals:proposal.declineTitle')}
        confirmLabel={t('renewals:proposal.decline')}
        destructive
        pending={decline.isPending}
        error={decline.error}
        onClose={() => setDeclining(null)}
        onConfirm={(reason) =>
          declining &&
          decline.mutate(
            { id: declining.id, version: declining.version, ...(reason ? { reason } : {}) },
            { onSuccess: () => setDeclining(null) },
          )
        }
      />
    </>
  );
}
