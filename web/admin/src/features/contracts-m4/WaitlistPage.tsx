import { useNavigate } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateWaitlistEntry,
  useQuoteFromWaitlist,
  useUpdateWaitlistEntry,
  useWaitlist,
  type PlacementInput,
  type Schemas,
  type WaitlistEntry,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
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
import { Input } from '@porto/shared/ui/input';
import { DatePicker, LengthInput, PageHeader } from '@porto/shared/ui/patterns';
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
import { CustomerField, type CustomerChoice } from '../desk/components/CustomerField';
import { ResourceField } from '../desk/components/ResourceField';
import { useMarinaToday } from '../registry/lib/today';
import { EnumSelect, IsoDate, LoadMore, M4ProblemAlert, PageFrame } from './components/kit';
import { addYear, waitlistQuoteInput } from './lib/waitlist';

const STATUSES: readonly Schemas['WaitlistStatus'][] = [
  'waiting',
  'offered',
  'converted',
  'withdrawn',
];
const TERMS: readonly Schemas['ContractTerm'][] = ['annual', 'seasonal', 'monthly', 'period'];

/** Waiting list (spec 13.9, D26): entries by priority, quote from an entry, withdraw. */
export function WaitlistPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const [status, setStatus] = useState<Schemas['WaitlistStatus'] | ''>('waiting');
  const list = useWaitlist(marinaId, status ? { status } : {});
  const update = useUpdateWaitlistEntry(marinaId);
  const [creating, setCreating] = useState(false);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const write = can('contracts.write');
  const [quoting, setQuoting] = useState<WaitlistEntry | null>(null);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        <PageHeader
          title={t('contractsM4:waitlist.title')}
          description={t('contractsM4:waitlist.description')}
          actions={
            write ? (
              <Button onClick={() => setCreating(true)}>
                <PlusIcon />
                {t('contractsM4:waitlist.new')}
              </Button>
            ) : null
          }
        />
        <EnumSelect
          id="waitlist-status"
          label={t('contractsM4:waitlist.status')}
          group="waitlistStatus"
          values={STATUSES}
          value={status}
          onChange={setStatus}
          allLabel={t('contractsM4:common.all')}
        />
        <M4ProblemAlert error={update.error} />
        {list.isPending ? (
          <LoadingState rows={4} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('contractsM4:waitlist.priority')}</TableHead>
                  <TableHead>{t('contractsM4:waitlist.customer')}</TableHead>
                  <TableHead>{t('contractsM4:waitlist.vessel')}</TableHead>
                  <TableHead>{t('contractsM4:waitlist.term')}</TableHead>
                  <TableHead>{t('contractsM4:waitlist.from')}</TableHead>
                  <TableHead>{t('contractsM4:waitlist.status')}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={7} className="text-muted-foreground h-24 text-center">
                      {t('contractsM4:waitlist.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((e) => (
                    <TableRow key={e.id}>
                      <TableCell>{e.priority}</TableCell>
                      <TableCell>{e.customer.display_name ?? ''}</TableCell>
                      <TableCell>
                        {e.vessel ? `${e.vessel.name} · ` : ''}
                        {`${formatCm(e.loa_cm, i18n.language)} × ${formatCm(e.beam_cm, i18n.language)}`}
                      </TableCell>
                      <TableCell>{t(`occupancyEnums:contractTerm.${e.term}`)}</TableCell>
                      <TableCell>
                        <IsoDate date={e.requested_from} />
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {t(`docsEnums:waitlistStatus.${e.status}`)}
                        </Badge>
                      </TableCell>
                      <TableCell className="flex justify-end gap-1">
                        {write && e.status === 'waiting' && e.vessel ? (
                          <Button size="sm" onClick={() => setQuoting(e)}>
                            {t('contractsM4:waitlist.quote')}
                          </Button>
                        ) : null}
                        {write && e.status === 'waiting' ? (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={update.isPending}
                            onClick={() =>
                              update.mutate({
                                id: e.id,
                                version: e.version,
                                patch: { status: 'withdrawn' },
                              })
                            }
                          >
                            {t('contractsM4:waitlist.withdraw')}
                          </Button>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
        <LoadMore query={list} />
        {creating ? <NewEntryDialog today={today} onClose={() => setCreating(false)} /> : null}
        {quoting ? (
          <QuoteFromEntryDialog
            entry={quoting}
            today={today}
            onClose={() => setQuoting(null)}
            onCreated={(quoteId) =>
              void navigate({ to: '/m/$marinaId/quotes/$quoteId', params: { marinaId, quoteId } })
            }
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function QuoteFromEntryDialog({
  entry,
  today,
  onClose,
  onCreated,
}: {
  entry: WaitlistEntry;
  today: string;
  onClose: () => void;
  onCreated: (quoteId: string) => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const quote = useQuoteFromWaitlist(marinaId);
  const [target, setTarget] = useState<PlacementInput | null>(null);
  const body = target ? waitlistQuoteInput(entry, target, today) : null;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('contractsM4:waitlist.quoteTitle')}</DialogTitle>
          <DialogDescription>{t('contractsM4:waitlist.quoteDescription')}</DialogDescription>
        </DialogHeader>
        <ResourceField
          marinaId={marinaId}
          idPrefix="wl-target"
          value={target}
          onChange={setTarget}
          types={['berth']}
          {...(entry.vessel
            ? {
                vessel: {
                  loa_cm: entry.vessel.loa_cm,
                  beam_cm: entry.vessel.beam_cm,
                  hull_type: entry.vessel.hull_type,
                },
              }
            : {})}
          date={entry.requested_from > today ? entry.requested_from : today}
          from={entry.requested_from > today ? entry.requested_from : today}
          to={addYear(entry.requested_from > today ? entry.requested_from : today)}
        />
        <M4ProblemAlert error={quote.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!body || quote.isPending}
            onClick={() =>
              body && quote.mutate({ id: entry.id, body }, { onSuccess: (q) => onCreated(q.id) })
            }
          >
            {t('contractsM4:waitlist.quote')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function NewEntryDialog({ today, onClose }: { today: string; onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateWaitlistEntry(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(null);
  const [loa, setLoa] = useState<number | null>(null);
  const [beam, setBeam] = useState<number | null>(null);
  const [term, setTerm] = useState<Schemas['ContractTerm'] | ''>('annual');
  const [from, setFrom] = useState<string | null>(today);
  const [priority, setPriority] = useState('0');
  const [notes, setNotes] = useState('');
  const [touched, setTouched] = useState(false);
  const ok = !!customer && !!loa && !!beam && !!term && !!from && /^\d{1,4}$/.test(priority);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('contractsM4:waitlist.new')}</DialogTitle>
          <DialogDescription>{t('contractsM4:waitlist.newDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="wl-customer">{t('contractsM4:waitlist.customer')}</FieldLabel>
          <CustomerField
            marinaId={marinaId}
            id="wl-customer"
            value={customer}
            onChange={setCustomer}
          />
        </Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="wl-loa">{t('contractsM4:waitlist.loa')}</FieldLabel>
            <LengthInput id="wl-loa" value={loa} onChange={setLoa} locale={i18n.language} />
          </Field>
          <Field>
            <FieldLabel htmlFor="wl-beam">{t('contractsM4:waitlist.beam')}</FieldLabel>
            <LengthInput id="wl-beam" value={beam} onChange={setBeam} locale={i18n.language} />
          </Field>
        </div>
        <div className="flex flex-wrap gap-3">
          <EnumSelect
            id="wl-term"
            label={t('contractsM4:waitlist.term')}
            values={TERMS}
            labelOf={(x) => t(`occupancyEnums:contractTerm.${x}`)}
            value={term}
            onChange={setTerm}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="wl-from">{t('contractsM4:waitlist.from')}</FieldLabel>
            <DatePicker id="wl-from" value={from} onChange={setFrom} />
          </Field>
          <Field className="w-28">
            <FieldLabel htmlFor="wl-priority">{t('contractsM4:waitlist.priority')}</FieldLabel>
            <Input
              id="wl-priority"
              inputMode="numeric"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
            />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="wl-notes">{t('contractsM4:waitlist.notes')}</FieldLabel>
          <Input
            id="wl-notes"
            value={notes}
            maxLength={500}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        {touched && !ok ? <FieldError>{t('contractsM4:waitlist.invalid')}</FieldError> : null}
        <M4ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={create.isPending}
            onClick={() => {
              setTouched(true);
              if (!ok || !customer || !loa || !beam || !term || !from) return;
              create.mutate(
                {
                  customer_id: customer.id,
                  loa_cm: loa,
                  beam_cm: beam,
                  term,
                  requested_from: from,
                  priority: Number(priority),
                  ...(notes.trim() ? { notes: notes.trim() } : {}),
                },
                { onSuccess: onClose },
              );
            }}
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
