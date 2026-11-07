import { getRouteApi, Link, useNavigate } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useBerthGroups,
  useCommunication,
  useCommunicationAction,
  useCommunicationRecipients,
  useCommunications,
  useCreateCommunication,
  usePreviewCommunication,
  useTemplates,
  useUpdateCommunication,
  type Communication,
  type CommunicationPreview,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  LoadMore,
  M4ProblemAlert,
  PageFrame,
  Section,
  StepBar,
} from '../contracts-m4/components/kit';
import { CustomerField, type CustomerChoice } from '../desk/components/CustomerField';
import { localText } from '../pricing/lib/enums';
import { MapSelection } from './components/MapSelection';
import {
  COMM_STEPS,
  buildAudience,
  deliverable,
  draftFromAudience,
  toggle,
  type AudienceDraft,
  type AudienceMode,
  type CommStep,
} from './lib/audience';

const detailRoute = getRouteApi('/auth/m/$marinaId/communications/$commCampaignId');
const STATUSES: readonly Schemas['CommStatus'][] = [
  'draft',
  'scheduled',
  'sending',
  'sent',
  'cancelled',
];
const KINDS: readonly Schemas['ContractKind'][] = ['mooring', 'transit', 'charter'];
const TERMS: readonly Schemas['ContractTerm'][] = ['annual', 'seasonal', 'monthly', 'period'];

/** Mass communications (spec 28): campaigns by status. */
export function CommunicationsPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<Schemas['CommStatus'] | ''>('');
  const list = useCommunications(marinaId, status || undefined);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('communications.send')}>
      <PageFrame>
        <PageHeader
          title={t('communications:list.title')}
          description={t('communications:list.description')}
          actions={
            <Button asChild>
              <Link
                to="/m/$marinaId/communications/$commCampaignId"
                params={{ marinaId, commCampaignId: 'new' }}
              >
                <PlusIcon />
                {t('communications:list.new')}
              </Link>
            </Button>
          }
        />
        <EnumSelect
          id="comm-status"
          label={t('communications:list.status')}
          group="commStatus"
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
                  <TableHead>{t('communications:list.name')}</TableHead>
                  <TableHead>{t('communications:list.category')}</TableHead>
                  <TableHead>{t('communications:list.status')}</TableHead>
                  <TableHead>{t('communications:list.sentAt')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-24 text-center">
                      {t('communications:list.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/communications/$commCampaignId"
                          params={{ marinaId, commCampaignId: c.id }}
                          className="font-medium underline-offset-4 hover:underline"
                        >
                          {c.name}
                        </Link>
                      </TableCell>
                      <TableCell>{t(`docsEnums:commCategory.${c.category}`)}</TableCell>
                      <TableCell>
                        <Badge variant="secondary">{t(`docsEnums:commStatus.${c.status}`)}</Badge>
                      </TableCell>
                      <TableCell>
                        {c.sent_at ? formatDateTime(c.sent_at, i18n.language) : ''}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        <LoadMore query={list} />
      </PageFrame>
    </RequirePermission>
  );
}

/** One communication: the wizard while it is a draft (or new), then the recipients table. */
export function CommunicationPage() {
  const { marinaId, can } = useCurrentMarina();
  const { commCampaignId } = detailRoute.useParams();
  const isNew = commCampaignId === 'new';
  const comm = useCommunication(marinaId, commCampaignId, !isNew);
  return (
    <RequirePermission allowed={can('communications.send')}>
      <PageFrame>
        {isNew ? (
          <CommWizard comm={null} />
        ) : comm.isPending ? (
          <LoadingState rows={6} />
        ) : comm.isError ? (
          <QueryErrorState error={comm.error} onRetry={() => void comm.refetch()} />
        ) : comm.data.status === 'draft' ? (
          <CommWizard key={comm.data.version} comm={comm.data} />
        ) : (
          <CommMonitor comm={comm.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

type Content = {
  name: string;
  category: Schemas['CommCategory'];
  templateId: string;
  subject: string;
  body: string;
  documentIds: string;
};

function CommWizard({ comm }: { comm: Communication | null }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const navigate = useNavigate();
  const create = useCreateCommunication(marinaId);
  const update = useUpdateCommunication(marinaId);
  const preview = usePreviewCommunication(marinaId);
  const action = useCommunicationAction(marinaId);
  const [step, setStep] = useState<CommStep>(comm ? 'preview' : 'audience');
  const [audience, setAudience] = useState<AudienceDraft>(() => draftFromAudience(comm?.audience));
  const [customers, setCustomers] = useState<{ id: string; label: string }[]>(
    (comm?.audience.customer_ids ?? []).map((id) => ({ id, label: id })),
  );
  const [content, setContent] = useState<Content>({
    name: comm?.name ?? '',
    category: comm?.category ?? 'service',
    templateId: comm?.template_id ?? '',
    subject: comm?.subject ?? '',
    body: comm?.body_md ?? '',
    documentIds: (comm?.attachments ?? [])
      .map((a) => a.document_id ?? a.file_id ?? '')
      .filter(Boolean)
      .join(', '),
  });
  const [saved, setSaved] = useState<Communication | null>(comm);
  const [result, setResult] = useState<CommunicationPreview | null>(null);
  const [sampleCustomer, setSampleCustomer] = useState<CustomerChoice>(null);
  const [scheduleAt, setScheduleAt] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const built = buildAudience({ ...audience, customerIds: customers.map((c) => c.id) });

  const contentError = (): string | null => {
    if (!content.name.trim()) return 'name';
    if (!content.templateId && (!content.subject.trim() || !content.body.trim())) return 'content';
    return null;
  };
  const input = (): Schemas['CommunicationInput'] | null => {
    if (!built.ok) return null;
    const attachments = content.documentIds
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .map((document_id) => ({ document_id }));
    return {
      name: content.name.trim(),
      category: content.category,
      audience: built.audience,
      ...(content.templateId ? { template_id: content.templateId } : {}),
      ...(content.subject.trim() ? { subject: content.subject.trim() } : {}),
      ...(content.body.trim() ? { body_md: content.body } : {}),
      attachments,
    };
  };
  const persist = async (): Promise<Communication | null> => {
    const body = input();
    if (!body) return null;
    const c = saved
      ? await update.mutateAsync({
          id: saved.id,
          version: saved.version,
          patch: { ...body, template_id: body.template_id ?? null },
        })
      : await create.mutateAsync(body);
    setSaved(c);
    return c;
  };
  const next = async () => {
    setError(null);
    if (step === 'audience') {
      if (!built.ok) return setError(`audience.${built.error}`);
      return setStep('content');
    }
    if (step === 'content') {
      const e = contentError();
      if (e) return setError(`content.${e}`);
      return setStep('attachments');
    }
    if (step === 'attachments') {
      const c = await persist().catch(() => null);
      if (!c) return;
      const p = await preview.mutateAsync({ id: c.id }).catch(() => null);
      setResult(p);
      return setStep('preview');
    }
    if (step === 'preview') setStep('send');
  };
  return (
    <>
      <PageHeader
        title={saved?.name ?? t('communications:wizard.title')}
        description={t('communications:wizard.description')}
      />
      <StepBar
        steps={COMM_STEPS}
        current={step}
        label={t('communications:wizard.steps')}
        labelOf={(s) => t(`communications:wizard.step.${s}`)}
        onSelect={setStep}
      />
      {step === 'audience' ? (
        <AudienceStep
          draft={audience}
          onChange={setAudience}
          customers={customers}
          onCustomers={setCustomers}
        />
      ) : null}
      {step === 'content' ? <ContentStep content={content} onChange={setContent} /> : null}
      {step === 'attachments' ? (
        <Field>
          <FieldLabel htmlFor="comm-attachments">
            {t('communications:wizard.attachments')}
          </FieldLabel>
          <Input
            id="comm-attachments"
            value={content.documentIds}
            onChange={(e) => setContent({ ...content, documentIds: e.target.value })}
          />
          <p className="text-muted-foreground text-sm">
            {t('communications:wizard.attachmentsHint')}
          </p>
        </Field>
      ) : null}
      {step === 'preview' && saved ? (
        <div className="flex flex-col gap-3">
          {result ? <PreviewCounts preview={result} /> : null}
          <div className="flex flex-wrap items-end gap-2">
            <Field className="w-72">
              <FieldLabel htmlFor="comm-sample">
                {t('communications:wizard.sampleCustomer')}
              </FieldLabel>
              <CustomerField
                marinaId={marinaId}
                id="comm-sample"
                value={sampleCustomer}
                onChange={setSampleCustomer}
              />
            </Field>
            <Button
              variant="outline"
              disabled={preview.isPending}
              onClick={() =>
                preview.mutate(
                  { id: saved.id, ...(sampleCustomer ? { customerId: sampleCustomer.id } : {}) },
                  { onSuccess: setResult },
                )
              }
            >
              {t('communications:wizard.refresh')}
            </Button>
          </div>
          {result?.sample ? (
            <Section title={result.sample.subject}>
              <pre className="text-sm whitespace-pre-wrap" data-testid="comm-sample-text">
                {result.sample.text}
              </pre>
            </Section>
          ) : null}
        </div>
      ) : null}
      {step === 'send' && saved ? (
        <div className="flex flex-wrap items-end gap-3">
          <Button
            disabled={action.isPending}
            onClick={() =>
              action.mutate(
                { id: saved.id, version: saved.version, action: 'send' },
                {
                  onSuccess: (c) => {
                    toast.success(t('communications:wizard.sent'));
                    void navigate({
                      to: '/m/$marinaId/communications/$commCampaignId',
                      params: { marinaId, commCampaignId: c.id },
                      replace: true,
                    });
                  },
                },
              )
            }
          >
            {t('communications:wizard.sendNow')}
          </Button>
          <Field className="w-auto">
            <FieldLabel htmlFor="comm-schedule">{t('communications:wizard.scheduleOn')}</FieldLabel>
            <DatePicker id="comm-schedule" value={scheduleAt} onChange={setScheduleAt} />
          </Field>
          <Button
            variant="outline"
            disabled={!scheduleAt || action.isPending}
            onClick={() =>
              scheduleAt &&
              action.mutate(
                {
                  id: saved.id,
                  version: saved.version,
                  action: 'schedule',
                  scheduledAt: `${scheduleAt}T07:00:00Z`,
                },
                {
                  onSuccess: (c) => {
                    toast.success(t('communications:wizard.scheduled'));
                    void navigate({
                      to: '/m/$marinaId/communications/$commCampaignId',
                      params: { marinaId, commCampaignId: c.id },
                      replace: true,
                    });
                  },
                },
              )
            }
          >
            {t('communications:wizard.schedule')}
          </Button>
        </div>
      ) : null}
      {error ? <FieldError>{t(`communications:errors.${error}`)}</FieldError> : null}
      <M4ProblemAlert error={create.error ?? update.error ?? preview.error ?? action.error} />
      <div className="flex gap-2">
        {step !== 'audience' ? (
          <Button
            variant="outline"
            onClick={() => setStep(COMM_STEPS[COMM_STEPS.indexOf(step) - 1]!)}
          >
            {t('renewals:wizard.back')}
          </Button>
        ) : null}
        {step !== 'send' ? (
          <Button
            disabled={create.isPending || update.isPending || preview.isPending}
            onClick={() => void next()}
          >
            {t('renewals:wizard.next')}
          </Button>
        ) : null}
      </div>
    </>
  );
}

export function PreviewCounts({ preview }: { preview: CommunicationPreview }) {
  const { t } = useT();
  const r = preview.recipients;
  return (
    <div className="grid gap-2 sm:grid-cols-4" data-testid="comm-preview-counts">
      {(
        [
          ['count', r.count],
          ['deliverable', deliverable(r)],
          ['noConsent', r.skipped_no_consent],
          ['noEmail', r.skipped_no_email],
        ] as const
      ).map(([k, n]) => (
        <div key={k} className="rounded-md border p-3">
          <div className="text-muted-foreground text-xs">{t(`communications:preview.${k}`)}</div>
          <div className="text-lg font-semibold tabular-nums">{n}</div>
        </div>
      ))}
    </div>
  );
}

export function AudienceStep({
  draft,
  onChange,
  customers,
  onCustomers,
}: {
  draft: AudienceDraft;
  onChange: (d: AudienceDraft) => void;
  customers: { id: string; label: string }[];
  onCustomers: (c: { id: string; label: string }[]) => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const groups = useBerthGroups(marinaId);
  const [pick, setPick] = useState<CustomerChoice>(null);
  return (
    <div className="flex flex-col gap-4">
      <Tabs
        value={draft.mode}
        onValueChange={(m) => onChange({ ...draft, mode: m as AudienceMode })}
      >
        <TabsList aria-label={t('communications:audience.mode')}>
          {(['filter', 'customers', 'map'] as const).map((m) => (
            <TabsTrigger key={m} value={m}>
              {t(`communications:audience.modes.${m}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {/* The panel of the active mode: the trigger's aria-controls must point to a rendered panel. */}
        <TabsContent value={draft.mode} className="flex flex-col gap-4">
          {draft.mode === 'filter' ? (
            <div className="flex flex-col gap-3">
              <FieldSet>
                <FieldLegend variant="label">{t('communications:audience.kinds')}</FieldLegend>
                <div className="flex flex-wrap gap-4">
                  {KINDS.map((k) => (
                    <Field key={k} orientation="horizontal" className="w-auto">
                      <Checkbox
                        id={`aud-kind-${k}`}
                        checked={draft.contractKinds.includes(k)}
                        onCheckedChange={() =>
                          onChange({ ...draft, contractKinds: toggle(draft.contractKinds, k) })
                        }
                      />
                      <FieldLabel htmlFor={`aud-kind-${k}`}>
                        {t(`communications:audience.kind.${k}`)}
                      </FieldLabel>
                    </Field>
                  ))}
                  <Field orientation="horizontal" className="w-auto">
                    <Checkbox
                      id="aud-holders"
                      checked={draft.holders}
                      onCheckedChange={(c) => onChange({ ...draft, holders: c === true })}
                    />
                    <FieldLabel htmlFor="aud-holders">
                      {t('communications:audience.holders')}
                    </FieldLabel>
                  </Field>
                </div>
              </FieldSet>
              <FieldSet>
                <FieldLegend variant="label">{t('communications:audience.terms')}</FieldLegend>
                <div className="flex flex-wrap gap-4">
                  {TERMS.map((term) => (
                    <Field key={term} orientation="horizontal" className="w-auto">
                      <Checkbox
                        id={`aud-term-${term}`}
                        checked={draft.terms.includes(term)}
                        onCheckedChange={() =>
                          onChange({ ...draft, terms: toggle(draft.terms, term) })
                        }
                      />
                      <FieldLabel htmlFor={`aud-term-${term}`}>
                        {t(`occupancyEnums:contractTerm.${term}`)}
                      </FieldLabel>
                    </Field>
                  ))}
                </div>
              </FieldSet>
              {(groups.data ?? []).length > 0 ? (
                <FieldSet>
                  <FieldLegend variant="label">{t('communications:audience.groups')}</FieldLegend>
                  <div className="flex flex-wrap gap-4">
                    {(groups.data ?? []).map((g) => (
                      <Field key={g.id} orientation="horizontal" className="w-auto">
                        <Checkbox
                          id={`aud-group-${g.id}`}
                          checked={draft.groups.includes(g.id)}
                          onCheckedChange={() =>
                            onChange({ ...draft, groups: toggle(draft.groups, g.id) })
                          }
                        />
                        <FieldLabel htmlFor={`aud-group-${g.id}`}>
                          {localText(g.name, i18n.language) || g.code}
                        </FieldLabel>
                      </Field>
                    ))}
                  </div>
                </FieldSet>
              ) : null}
              <div className="flex flex-wrap gap-3">
                <Field className="w-auto">
                  <FieldLabel htmlFor="aud-from">
                    {t('communications:audience.presentFrom')}
                  </FieldLabel>
                  <DatePicker
                    id="aud-from"
                    value={draft.presentFrom}
                    onChange={(presentFrom) => onChange({ ...draft, presentFrom })}
                  />
                </Field>
                <Field className="w-auto">
                  <FieldLabel htmlFor="aud-to">{t('communications:audience.presentTo')}</FieldLabel>
                  <DatePicker
                    id="aud-to"
                    value={draft.presentTo}
                    onChange={(presentTo) => onChange({ ...draft, presentTo })}
                  />
                </Field>
                <Field className="w-48">
                  <FieldLabel htmlFor="aud-deadlines">
                    {t('communications:audience.deadlines')}
                  </FieldLabel>
                  <Input
                    id="aud-deadlines"
                    inputMode="numeric"
                    value={draft.deadlinesWithinDays}
                    onChange={(e) => onChange({ ...draft, deadlinesWithinDays: e.target.value })}
                  />
                </Field>
              </div>
            </div>
          ) : null}
          {draft.mode === 'customers' ? (
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-end gap-2">
                <Field className="w-72">
                  <FieldLabel htmlFor="aud-customer">
                    {t('communications:audience.customer')}
                  </FieldLabel>
                  <CustomerField
                    marinaId={marinaId}
                    id="aud-customer"
                    value={pick}
                    onChange={setPick}
                  />
                </Field>
                <Button
                  variant="outline"
                  disabled={!pick}
                  onClick={() => {
                    if (pick && !customers.some((c) => c.id === pick.id))
                      onCustomers([...customers, pick]);
                    setPick(null);
                  }}
                >
                  {t('communications:audience.add')}
                </Button>
              </div>
              <ul className="flex flex-wrap gap-2">
                {customers.map((c) => (
                  <li key={c.id}>
                    <Badge variant="secondary">
                      {c.label}
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-auto px-1"
                        aria-label={t('communications:audience.remove', { name: c.label })}
                        onClick={() => onCustomers(customers.filter((x) => x.id !== c.id))}
                      >
                        {t('communications:audience.removeShort')}
                      </Button>
                    </Badge>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {draft.mode === 'map' ? (
            <MapSelection
              mapId={draft.mapId}
              selected={draft.resourceIds}
              onMap={(mapId) => onChange({ ...draft, mapId })}
              onChange={(resourceIds, mapId) => onChange({ ...draft, resourceIds, mapId })}
            />
          ) : null}
        </TabsContent>
      </Tabs>
    </div>
  );
}

function ContentStep({ content, onChange }: { content: Content; onChange: (c: Content) => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const templates = useTemplates(marinaId, 'communication');
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-3">
        <Field className="w-72">
          <FieldLabel htmlFor="comm-name">{t('communications:list.name')}</FieldLabel>
          <Input
            id="comm-name"
            value={content.name}
            maxLength={120}
            onChange={(e) => onChange({ ...content, name: e.target.value })}
          />
        </Field>
        <EnumSelect
          id="comm-category"
          label={t('communications:list.category')}
          group="commCategory"
          values={['service', 'promotional'] as const}
          value={content.category}
          onChange={(category) => onChange({ ...content, category: category || 'service' })}
        />
        <EnumSelect
          id="comm-template"
          label={t('communications:wizard.template')}
          values={(templates.data ?? []).map((x) => x.id)}
          labelOf={(id) => templates.data?.find((x) => x.id === id)?.name ?? id}
          value={content.templateId}
          onChange={(templateId) => onChange({ ...content, templateId })}
          allLabel={t('communications:wizard.freeText')}
        />
      </div>
      {content.category === 'promotional' ? (
        <p className="text-muted-foreground text-sm">
          {t('communications:wizard.promotionalHint')}
        </p>
      ) : null}
      {!content.templateId ? (
        <>
          <Field>
            <FieldLabel htmlFor="comm-subject">{t('communications:wizard.subject')}</FieldLabel>
            <Input
              id="comm-subject"
              value={content.subject}
              maxLength={200}
              onChange={(e) => onChange({ ...content, subject: e.target.value })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="comm-body">{t('communications:wizard.body')}</FieldLabel>
            <Textarea
              id="comm-body"
              rows={10}
              className="font-mono text-sm"
              value={content.body}
              onChange={(e) => onChange({ ...content, body: e.target.value })}
            />
            <p className="text-muted-foreground text-sm">{t('communications:wizard.bodyHint')}</p>
          </Field>
        </>
      ) : null}
    </div>
  );
}

function CommMonitor({ comm }: { comm: Communication }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const recipients = useCommunicationRecipients(marinaId, comm.id);
  const action = useCommunicationAction(marinaId);
  return (
    <>
      <PageHeader
        title={comm.name}
        description={t(`docsEnums:commCategory.${comm.category}`)}
        actions={
          comm.status === 'scheduled' ? (
            <Button
              variant="outline"
              disabled={action.isPending}
              onClick={() =>
                action.mutate({ id: comm.id, version: comm.version, action: 'cancel' })
              }
            >
              {t('communications:monitor.cancel')}
            </Button>
          ) : null
        }
      />
      <div className="flex flex-wrap gap-2 text-sm">
        <Badge variant="secondary">{t(`docsEnums:commStatus.${comm.status}`)}</Badge>
        {comm.scheduled_at ? (
          <span>
            {t('communications:monitor.scheduledAt', {
              at: formatDateTime(comm.scheduled_at, i18n.language),
            })}
          </span>
        ) : null}
        {comm.sent_at ? (
          <span>
            {t('communications:monitor.sentAt', {
              at: formatDateTime(comm.sent_at, i18n.language),
            })}
          </span>
        ) : null}
      </div>
      <M4ProblemAlert error={action.error} />
      {recipients.isPending ? (
        <LoadingState rows={5} />
      ) : recipients.isError ? (
        <QueryErrorState error={recipients.error} onRetry={() => void recipients.refetch()} />
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('communications:monitor.customer')}</TableHead>
                <TableHead>{t('communications:monitor.status')}</TableHead>
                <TableHead>{t('communications:monitor.delivery')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {recipients.data.items.map((r) => (
                <TableRow key={r.customer.id} data-testid="comm-recipient">
                  <TableCell>{r.customer.display_name ?? ''}</TableCell>
                  <TableCell>{t(`docsEnums:recipientStatus.${r.status}`)}</TableCell>
                  <TableCell>
                    {r.delivery_status
                      ? t(`communications:monitor.deliveries.${r.delivery_status}`)
                      : ''}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </>
  );
}
