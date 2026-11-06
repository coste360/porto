import { SettingsIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useDeadlinePolicies,
  useDeadlines,
  useReplaceDeadlinePolicies,
  type Deadline,
  type DeadlinePolicy,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { DEADLINE_KINDS, DEADLINE_STATUSES, DEADLINE_URGENCIES } from '../lib/enums';
import { parseLeadDays } from '../lib/format';
import { DeadlineTable } from './DeadlineTable';

const ALL = '__all__';
type Kind = Schemas['DeadlineKind'];
type Status = Schemas['DeadlineStatus'];
type Urgency = Schemas['DeadlineUrgency'];

export function DeadlinesPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const [status, setStatus] = useState<Status>('open');
  const [kind, setKind] = useState<Kind | ''>('');
  const [urgency, setUrgency] = useState<Urgency | ''>('');
  const [policiesOpen, setPoliciesOpen] = useState(false);

  return (
    <RequirePermission allowed={can('customers.read')}>
      <PageHeader
        title={t('deadlines:title')}
        description={t('deadlines:description')}
        actions={
          can('marina.configure') ? (
            <Button variant="outline" onClick={() => setPoliciesOpen(true)}>
              <SettingsIcon />
              {t('deadlines:policies.title')}
            </Button>
          ) : null
        }
      />
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end">
        <FilterSelect
          id="deadlines-status"
          label={t('deadlines:filters.status')}
          value={status}
          onChange={(v) => setStatus((v || 'open') as Status)}
          options={DEADLINE_STATUSES.map((s) => ({
            value: s,
            label: t(`deadlines:statuses.${s}`),
          }))}
        />
        <FilterSelect
          id="deadlines-kind"
          label={t('deadlines:filters.kind')}
          value={kind}
          allLabel={t('deadlines:filters.allKinds')}
          onChange={(v) => setKind(v as Kind | '')}
          options={DEADLINE_KINDS.map((k) => ({ value: k, label: t(`deadlines:kinds.${k}`) }))}
        />
        {status === 'open' ? (
          <FilterSelect
            id="deadlines-urgency"
            label={t('deadlines:filters.urgency')}
            value={urgency}
            allLabel={t('deadlines:filters.allUrgencies')}
            onChange={(v) => setUrgency(v as Urgency | '')}
            options={DEADLINE_URGENCIES.map((u) => ({
              value: u,
              label: t(`deadlines:urgency.${u}`),
            }))}
          />
        ) : null}
      </div>
      <DeadlineGroups status={status} kind={kind} urgency={status === 'open' ? urgency : ''} />
      {can('marina.configure') ? (
        <PoliciesDialog open={policiesOpen} onOpenChange={setPoliciesOpen} />
      ) : null}
    </RequirePermission>
  );
}

function FilterSelect({
  id,
  label,
  value,
  allLabel,
  options,
  onChange,
}: {
  id: string;
  label: string;
  value: string;
  allLabel?: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <Field className="sm:max-w-56">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value || ALL} onValueChange={(v) => onChange(v === ALL ? '' : v)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {allLabel ? <SelectItem value={ALL}>{allLabel}</SelectItem> : null}
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

/** Open deadlines grouped by urgency (overdue, due soon, later); closed ones as a single list. */
function DeadlineGroups({
  status,
  kind,
  urgency,
}: {
  status: Status;
  kind: Kind | '';
  urgency: Urgency | '';
}) {
  const { t } = useT();
  const { can, marinaId } = useCurrentMarina();
  const query = useDeadlines(marinaId, {
    status,
    ...(kind ? { kind } : {}),
    ...(urgency ? { urgency } : {}),
  });
  if (query.isPending) return <LoadingState rows={6} />;
  if (query.isError)
    return <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const items = query.data.pages.flatMap((p) => p.items);
  const canWrite = can('customers.write');
  const more = query.hasNextPage ? (
    <Button
      variant="outline"
      onClick={() => void query.fetchNextPage()}
      disabled={query.isFetchingNextPage}
    >
      {t('common:actions.loadMore')}
    </Button>
  ) : null;

  if (status !== 'open') {
    return (
      <div className="flex flex-col gap-3">
        <DeadlineTable deadlines={items} canWrite={canWrite} />
        {more}
      </div>
    );
  }
  const groups = DEADLINE_URGENCIES.filter((u) => !urgency || u === urgency).map((u) => ({
    urgency: u,
    items: items.filter((d: Deadline) => d.urgency === u),
  }));
  return (
    <div className="flex flex-col gap-6">
      {groups.map((g) => (
        <section
          key={g.urgency}
          aria-labelledby={`deadlines-${g.urgency}`}
          className="flex flex-col gap-2"
        >
          <h2 id={`deadlines-${g.urgency}`} className="text-lg font-semibold">
            {t(`deadlines:urgency.${g.urgency}`)} ({g.items.length})
          </h2>
          <DeadlineTable deadlines={g.items} canWrite={canWrite} />
        </section>
      ))}
      {more}
    </div>
  );
}

function PoliciesDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const policies = useDeadlinePolicies(marinaId);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('deadlines:policies.title')}</DialogTitle>
          <DialogDescription>{t('deadlines:policies.description')}</DialogDescription>
        </DialogHeader>
        {policies.isPending ? (
          <LoadingState />
        ) : policies.isError ? (
          <QueryErrorState error={policies.error} onRetry={() => void policies.refetch()} />
        ) : open ? (
          <PoliciesForm items={policies.data.items} onDone={() => onOpenChange(false)} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function PoliciesForm({ items, onDone }: { items: DeadlinePolicy[]; onDone: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const replace = useReplaceDeadlinePolicies(marinaId);
  const [texts, setTexts] = useState<Record<string, string>>(() =>
    Object.fromEntries(items.map((p) => [p.kind, p.lead_days.join(', ')])),
  );
  const parsed = items.map((p) => ({ kind: p.kind, days: parseLeadDays(texts[p.kind] ?? '') }));
  const invalid = parsed.some((p) => p.days === null);
  return (
    <form
      noValidate
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (invalid) return;
        replace.mutate(
          parsed.map((p) => ({ kind: p.kind, lead_days: p.days ?? [] })),
          {
            onSuccess: () => {
              toast.success(t('deadlines:policies.saved'));
              onDone();
            },
          },
        );
      }}
    >
      {replace.isError ? <ProblemAlert error={replace.error} /> : null}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{t('deadlines:policies.leadDays')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {parsed.map((p) => (
            <Field key={p.kind} data-invalid={p.days === null || undefined}>
              <FieldLabel htmlFor={`policy-${p.kind}`}>{t(`deadlines:kinds.${p.kind}`)}</FieldLabel>
              <Input
                id={`policy-${p.kind}`}
                value={texts[p.kind] ?? ''}
                aria-invalid={p.days === null || undefined}
                onChange={(e) => setTexts((prev) => ({ ...prev, [p.kind]: e.target.value }))}
              />
              {p.days === null ? <FieldError>{t('deadlines:policies.invalid')}</FieldError> : null}
            </Field>
          ))}
          <FieldDescription>{t('deadlines:policies.leadDaysHint')}</FieldDescription>
        </CardContent>
      </Card>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={invalid || replace.isPending}>
          {t('common:actions.save')}
        </Button>
      </DialogFooter>
    </form>
  );
}
