import { Link, useNavigate } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateRight,
  useRights,
  type PlacementInput,
  type RightKind,
  type RightStatus,
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

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  IsoDate,
  LoadMore,
  M4ProblemAlert,
  PageFrame,
} from '../contracts-m4/components/kit';
import { ResourceField } from '../desk/components/ResourceField';
import { VesselField } from '../desk/components/VesselField';
import { NO_VESSEL, type VesselValue } from '../desk/lib/vessel';
import { useMarinaToday } from '../registry/lib/today';
import { HoldersEditor } from './components/HoldersEditor';
import { checkShares, holderRows, toHolderShares, type HolderDraft } from './lib/shares';
import { RIGHT_KINDS, RIGHT_STATUSES, rightVariant } from './lib/rights';

/** Berth rights of use (spec 14.1): list and the creation wizard. */
export function RightsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<RightStatus | ''>('active');
  const list = useRights(marinaId, status ? { status } : {});
  const [creating, setCreating] = useState(false);
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        <PageHeader
          title={t('rights:list.title')}
          description={t('rights:list.description')}
          actions={
            <Button onClick={() => setCreating(true)}>
              <PlusIcon />
              {t('rights:wizard.new')}
            </Button>
          }
        />
        <EnumSelect
          id="rights-status"
          label={t('rights:list.status')}
          group="rightStatus"
          values={RIGHT_STATUSES}
          value={status}
          onChange={setStatus}
          allLabel={t('contractsM4:common.all')}
        />
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('rights:list.berth')}</TableHead>
                  <TableHead>{t('rights:list.kind')}</TableHead>
                  <TableHead>{t('rights:list.holders')}</TableHead>
                  <TableHead>{t('rights:list.period')}</TableHead>
                  <TableHead>{t('rights:list.status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-muted-foreground h-24 text-center">
                      {t('rights:list.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((r) => (
                    <TableRow key={r.id} data-testid="right-row">
                      <TableCell>
                        <Link
                          to="/m/$marinaId/rights/$rightId"
                          params={{ marinaId, rightId: r.id }}
                          className="font-medium underline-offset-4 hover:underline"
                        >
                          {r.berth_code ?? r.allocations?.[0]?.resource_code ?? r.berth.resource_id}
                        </Link>
                      </TableCell>
                      <TableCell>{t(`docsEnums:rightKind.${r.kind}`)}</TableCell>
                      <TableCell>
                        {r.holders
                          .map((h) => `${h.customer.display_name ?? ''} (${h.share_millis}‰)`)
                          .join(', ')}
                      </TableCell>
                      <TableCell>
                        <IsoDate date={r.from_on} /> <IsoDate date={r.to_on} />
                      </TableCell>
                      <TableCell className="flex flex-wrap gap-1">
                        <Badge variant={rightVariant(r.status)}>
                          {t(`docsEnums:rightStatus.${r.status}`)}
                        </Badge>
                        {r.active_mandate_id ? (
                          <Badge variant="outline">{t('rights:list.mandate')}</Badge>
                        ) : null}
                        {r.unpaid_fees ? (
                          <Badge variant="destructive">{t('rights:list.unpaid')}</Badge>
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
        {creating ? <RightWizard onClose={() => setCreating(false)} /> : null}
      </PageFrame>
    </RequirePermission>
  );
}

function RightWizard({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const navigate = useNavigate();
  const create = useCreateRight(marinaId);
  const [berth, setBerth] = useState<PlacementInput | null>(null);
  const [kind, setKind] = useState<RightKind | ''>('long_term_use');
  const [fromOn, setFromOn] = useState<string | null>(today);
  const [toOn, setToOn] = useState<string | null>(null);
  const [deed, setDeed] = useState({
    deed: '',
    number: '',
    notary: '',
    date: null as string | null,
  });
  const [holders, setHolders] = useState<HolderDraft[]>([{ customer: null, text: '1000' }]);
  const [vessel, setVessel] = useState<VesselValue>(NO_VESSEL);
  const [touched, setTouched] = useState(false);
  const shares = checkShares(holderRows(holders));
  const ok = !!berth && !!kind && !!fromOn && shares.ok;
  const vesselId = vessel.kind === 'existing' ? vessel.id : null;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('rights:wizard.new')}</DialogTitle>
          <DialogDescription>{t('rights:wizard.description')}</DialogDescription>
        </DialogHeader>
        <ResourceField
          marinaId={marinaId}
          idPrefix="right-berth"
          value={berth}
          onChange={setBerth}
          types={['berth']}
          date={fromOn ?? today}
          from={fromOn ?? today}
          to={toOn ?? fromOn ?? today}
          {...(vessel.kind === 'existing' && vessel.dims ? { vessel: vessel.dims } : {})}
        />
        <div className="flex flex-wrap gap-3">
          <EnumSelect
            id="right-kind"
            label={t('rights:list.kind')}
            group="rightKind"
            values={RIGHT_KINDS}
            value={kind}
            onChange={setKind}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="right-from">{t('rights:wizard.fromOn')}</FieldLabel>
            <DatePicker id="right-from" value={fromOn} onChange={setFromOn} />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="right-to">{t('rights:wizard.toOn')}</FieldLabel>
            <DatePicker id="right-to" value={toOn} onChange={setToOn} />
          </Field>
        </div>
        <div className="grid gap-3 sm:grid-cols-4">
          {(['deed', 'number', 'notary'] as const).map((k) => (
            <Field key={k}>
              <FieldLabel htmlFor={`right-title-${k}`}>{t(`rights:title.${k}`)}</FieldLabel>
              <Input
                id={`right-title-${k}`}
                value={deed[k]}
                maxLength={200}
                onChange={(e) => setDeed({ ...deed, [k]: e.target.value })}
              />
            </Field>
          ))}
          <Field>
            <FieldLabel htmlFor="right-title-date">{t('rights:title.date')}</FieldLabel>
            <DatePicker
              id="right-title-date"
              value={deed.date}
              onChange={(date) => setDeed({ ...deed, date })}
            />
          </Field>
        </div>
        <HoldersEditor
          marinaId={marinaId}
          idPrefix="right"
          rows={holders}
          onChange={setHolders}
          showErrors={touched}
        />
        <VesselField
          marinaId={marinaId}
          idPrefix="right-vessel"
          value={vessel}
          onChange={setVessel}
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
              if (!ok || !berth || !kind || !fromOn) return;
              create.mutate(
                {
                  berth_id: berth.resource_id,
                  kind,
                  from_on: fromOn,
                  ...(toOn ? { to_on: toOn } : {}),
                  title: {
                    ...(deed.deed.trim() ? { deed: deed.deed.trim() } : {}),
                    ...(deed.number.trim() ? { number: deed.number.trim() } : {}),
                    ...(deed.notary.trim() ? { notary: deed.notary.trim() } : {}),
                    ...(deed.date ? { date: deed.date } : {}),
                  },
                  holders: toHolderShares(holderRows(holders)),
                  ...(vesselId ? { vessels: [{ vessel_id: vesselId, primary: true }] } : {}),
                },
                {
                  onSuccess: (r) => {
                    onClose();
                    void navigate({
                      to: '/m/$marinaId/rights/$rightId',
                      params: { marinaId, rightId: r.id },
                    });
                  },
                },
              );
            }}
          >
            {t('rights:wizard.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
