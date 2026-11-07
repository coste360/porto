import { Link } from '@tanstack/react-router';
import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState, type DragEvent } from 'react';

import {
  useCharterFleets,
  useCharters,
  useContract,
  useCreateCharter,
  useCreateCharterFleet,
  useRotateCharter,
  useUpdateCharterFleet,
  type CharterFleet,
  type Contract,
  type PlacementInput,
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
import { Input } from '@porto/shared/ui/input';
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
import { CustomerField, type CustomerChoice } from '../desk/components/CustomerField';
import { ResourceField } from '../desk/components/ResourceField';
import { VesselField } from '../desk/components/VesselField';
import { NO_VESSEL, type VesselValue } from '../desk/lib/vessel';
import { useMarinaToday } from '../registry/lib/today';
import { EnumSelect, IsoDate, M4ProblemAlert, Money, PageFrame, Section } from './components/kit';
import {
  charterBody,
  dropVessel,
  rotationAssignments,
  type CharterDraft,
  type Rotation,
} from './lib/charter';

const MODES: readonly Schemas['CharterPricingMode'][] = ['per_berth', 'per_vessel', 'flat'];
const WEEKDAYS = ['1', '2', '3', '4', '5', '6', '7'] as const;

/** Charter (spec 13.6): fleets of the charter companies, charter contracts and the rotation board. */
export function ChartersPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const fleets = useCharterFleets(marinaId);
  const charters = useCharters(marinaId);
  const [dialog, setDialog] = useState<'fleet' | 'charter' | null>(null);
  const [editing, setEditing] = useState<CharterFleet | null>(null);
  const [rotating, setRotating] = useState<string | null>(null);
  const write = can('contracts.write');
  return (
    <RequirePermission allowed={can('contracts.read')}>
      <PageFrame>
        <PageHeader
          title={t('charter:list.title')}
          description={t('charter:list.description')}
          actions={
            write ? (
              <>
                <Button variant="outline" onClick={() => setDialog('fleet')}>
                  <PlusIcon />
                  {t('charter:fleet.new')}
                </Button>
                <Button
                  onClick={() => setDialog('charter')}
                  disabled={(fleets.data ?? []).length === 0}
                >
                  <PlusIcon />
                  {t('charter:wizard.new')}
                </Button>
              </>
            ) : null
          }
        />
        <Section title={t('charter:fleet.title')}>
          {fleets.isPending ? (
            <LoadingState rows={2} />
          ) : fleets.isError ? (
            <QueryErrorState error={fleets.error} onRetry={() => void fleets.refetch()} />
          ) : fleets.data.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('charter:fleet.empty')}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {fleets.data.map((f) => (
                <li key={f.id} className="flex flex-wrap items-center gap-2 rounded-md border p-2">
                  <span className="font-medium">{f.name}</span>
                  <span className="text-muted-foreground text-sm">
                    {f.customer.display_name ?? ''}
                  </span>
                  {f.vessels.map((v) => (
                    <Badge key={v.vessel.id} variant={v.active ? 'secondary' : 'outline'}>
                      {v.vessel.name}
                    </Badge>
                  ))}
                  {write ? (
                    <Button size="sm" variant="ghost" onClick={() => setEditing(f)}>
                      {t('common:actions.edit')}
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </Section>
        <Section title={t('charter:list.contracts')}>
          {charters.isPending ? (
            <LoadingState rows={3} />
          ) : charters.isError ? (
            <QueryErrorState error={charters.error} onRetry={() => void charters.refetch()} />
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('charter:list.code')}</TableHead>
                    <TableHead>{t('charter:list.fleet')}</TableHead>
                    <TableHead>{t('charter:list.period')}</TableHead>
                    <TableHead className="text-right">{t('charter:list.total')}</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(charters.data.items ?? []).length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-muted-foreground h-24 text-center">
                        {t('charter:list.empty')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    charters.data.items.map((c) => (
                      <TableRow key={c.id}>
                        <TableCell>
                          <Link
                            to="/m/$marinaId/contracts/$contractId"
                            params={{ marinaId, contractId: c.id }}
                            className="underline-offset-4 hover:underline"
                          >
                            {c.code}
                          </Link>
                        </TableCell>
                        <TableCell>{c.customer?.display_name ?? ''}</TableCell>
                        <TableCell>
                          <IsoDate date={c.starts_on} /> <IsoDate date={c.ends_on} />
                        </TableCell>
                        <TableCell className="text-right">
                          <Money cents={c.total_cents} />
                        </TableCell>
                        <TableCell className="text-right">
                          {write && c.status === 'active' ? (
                            <Button size="sm" variant="outline" onClick={() => setRotating(c.id)}>
                              {t('charter:rotation.open')}
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
        </Section>
        {dialog === 'fleet' || editing ? (
          <FleetDialog
            fleet={editing}
            onClose={() => {
              setDialog(null);
              setEditing(null);
            }}
          />
        ) : null}
        {dialog === 'charter' ? (
          <CharterDialog fleets={fleets.data ?? []} today={today} onClose={() => setDialog(null)} />
        ) : null}
        {rotating ? (
          <RotationLoader
            contractId={rotating}
            fleets={fleets.data ?? []}
            onClose={() => setRotating(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function FleetDialog({ fleet, onClose }: { fleet: CharterFleet | null; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateCharterFleet(marinaId);
  const update = useUpdateCharterFleet(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(
    fleet ? { id: fleet.customer.id, label: fleet.customer.display_name ?? '' } : null,
  );
  const [name, setName] = useState(fleet?.name ?? '');
  const [vessels, setVessels] = useState<{ id: string; label: string }[]>(
    fleet?.vessels
      .filter((v) => v.active)
      .map((v) => ({ id: v.vessel.id, label: v.vessel.name })) ?? [],
  );
  const [picking, setPicking] = useState<VesselValue>(NO_VESSEL);
  const ok = !!customer && name.trim() !== '' && vessels.length > 0;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{fleet ? t('charter:fleet.edit') : t('charter:fleet.new')}</DialogTitle>
          <DialogDescription>{t('charter:fleet.description')}</DialogDescription>
        </DialogHeader>
        {!fleet ? (
          <Field>
            <FieldLabel htmlFor="fleet-customer">{t('charter:fleet.company')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="fleet-customer"
              value={customer}
              onChange={setCustomer}
            />
          </Field>
        ) : null}
        <Field>
          <FieldLabel htmlFor="fleet-name">{t('charter:fleet.name')}</FieldLabel>
          <Input
            id="fleet-name"
            value={name}
            maxLength={120}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <div className="flex flex-col gap-2">
          <VesselField
            marinaId={marinaId}
            idPrefix="fleet-vessel"
            value={picking}
            onChange={setPicking}
          />
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={picking.kind !== 'existing' || !picking.id}
              onClick={() => {
                if (
                  picking.kind === 'existing' &&
                  picking.id &&
                  !vessels.some((v) => v.id === picking.id)
                ) {
                  setVessels([...vessels, { id: picking.id, label: picking.label }]);
                }
                setPicking(NO_VESSEL);
              }}
            >
              <PlusIcon />
              {t('charter:fleet.addVessel')}
            </Button>
          </div>
          <ul className="flex flex-wrap gap-2">
            {vessels.map((v) => (
              <li key={v.id}>
                <Badge variant="secondary" className="gap-1">
                  {v.label}
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-5"
                    aria-label={t('charter:fleet.removeVessel', { name: v.label })}
                    onClick={() => setVessels(vessels.filter((x) => x.id !== v.id))}
                  >
                    <TrashIcon />
                  </Button>
                </Badge>
              </li>
            ))}
          </ul>
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!ok || create.isPending || update.isPending}
            onClick={() =>
              fleet
                ? update.mutate(
                    {
                      id: fleet.id,
                      version: fleet.version,
                      patch: { name: name.trim(), vessel_ids: vessels.map((v) => v.id) },
                    },
                    { onSuccess: onClose },
                  )
                : customer &&
                  create.mutate(
                    {
                      customer_id: customer.id,
                      name: name.trim(),
                      vessel_ids: vessels.map((v) => v.id),
                    },
                    { onSuccess: onClose },
                  )
            }
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CharterDialog({
  fleets,
  today,
  onClose,
}: {
  fleets: CharterFleet[];
  today: string;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateCharter(marinaId);
  const [d, setD] = useState<CharterDraft>({
    fleetId: fleets[0]?.id ?? '',
    startsOn: today,
    endsOn: null,
    mode: 'per_berth',
    flatCents: null,
    flatReason: '',
    changeoverWeekday: '6',
    placements: [{ target: null, vesselId: '' }],
    activate: false,
  });
  const [touched, setTouched] = useState(false);
  const fleet = fleets.find((f) => f.id === d.fleetId);
  const mapped = charterBody(d);
  const setPlacement = (
    i: number,
    p: Partial<{ target: PlacementInput | null; vesselId: string }>,
  ) => setD({ ...d, placements: d.placements.map((x, j) => (j === i ? { ...x, ...p } : x)) });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('charter:wizard.new')}</DialogTitle>
          <DialogDescription>{t('charter:wizard.description')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-3">
          <EnumSelect
            id="charter-fleet"
            label={t('charter:list.fleet')}
            values={fleets.map((f) => f.id)}
            labelOf={(id) => fleets.find((f) => f.id === id)?.name ?? id}
            value={d.fleetId}
            onChange={(fleetId) => setD({ ...d, fleetId })}
          />
          <Field className="w-auto">
            <FieldLabel htmlFor="charter-start">{t('charter:wizard.startsOn')}</FieldLabel>
            <DatePicker
              id="charter-start"
              value={d.startsOn}
              onChange={(startsOn) => setD({ ...d, startsOn })}
            />
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="charter-end">{t('charter:wizard.endsOn')}</FieldLabel>
            <DatePicker
              id="charter-end"
              value={d.endsOn}
              onChange={(endsOn) => setD({ ...d, endsOn })}
            />
          </Field>
          <EnumSelect
            id="charter-mode"
            label={t('charter:wizard.mode')}
            group="charterPricingMode"
            values={MODES}
            value={d.mode}
            onChange={(mode) => setD({ ...d, mode: mode || 'per_berth' })}
          />
          <EnumSelect
            id="charter-weekday"
            label={t('charter:wizard.changeover')}
            values={WEEKDAYS}
            labelOf={(w) => t(`charter:weekdays.${w}`)}
            value={d.changeoverWeekday}
            onChange={(w) => setD({ ...d, changeoverWeekday: w || '6' })}
          />
        </div>
        {d.mode === 'flat' ? (
          <div className="flex flex-wrap gap-3">
            <Field className="w-48">
              <FieldLabel htmlFor="charter-flat">{t('charter:wizard.flat')}</FieldLabel>
              <MoneyInput
                id="charter-flat"
                locale={i18n.language}
                value={d.flatCents}
                onChange={(flatCents) => setD({ ...d, flatCents })}
              />
            </Field>
            <Field className="flex-1">
              <FieldLabel htmlFor="charter-flat-reason">
                {t('contractsM4:common.reason')}
              </FieldLabel>
              <Input
                id="charter-flat-reason"
                value={d.flatReason}
                onChange={(e) => setD({ ...d, flatReason: e.target.value })}
              />
            </Field>
          </div>
        ) : null}
        <div className="flex flex-col gap-3">
          {d.placements.map((p, i) => (
            <div key={i} className="flex flex-col gap-2 rounded-md border p-2">
              <ResourceField
                marinaId={marinaId}
                idPrefix={`charter-res-${i}`}
                value={p.target}
                onChange={(target) => setPlacement(i, { target })}
                types={['berth', 'dynamic_area']}
                date={d.startsOn ?? today}
                from={d.startsOn ?? today}
                to={d.endsOn ?? d.startsOn ?? today}
              />
              <div className="flex items-end gap-2">
                <EnumSelect
                  id={`charter-vessel-${i}`}
                  label={t('charter:wizard.vessel')}
                  values={(fleet?.vessels ?? []).filter((v) => v.active).map((v) => v.vessel.id)}
                  labelOf={(id) =>
                    fleet?.vessels.find((v) => v.vessel.id === id)?.vessel.name ?? id
                  }
                  value={p.vesselId}
                  onChange={(vesselId) => setPlacement(i, { vesselId })}
                />
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('charter:wizard.removePlacement')}
                  onClick={() => setD({ ...d, placements: d.placements.filter((_, j) => j !== i) })}
                >
                  <TrashIcon />
                </Button>
              </div>
            </div>
          ))}
          <div>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                setD({ ...d, placements: [...d.placements, { target: null, vesselId: '' }] })
              }
            >
              <PlusIcon />
              {t('charter:wizard.addPlacement')}
            </Button>
          </div>
        </div>
        {touched && !mapped.ok ? (
          <FieldError>{t(`charter:wizard.errors.${mapped.error}`)}</FieldError>
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
              if (mapped.ok) create.mutate(mapped.body, { onSuccess: onClose });
            }}
          >
            {t('charter:wizard.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function RotationLoader({
  contractId,
  fleets,
  onClose,
}: {
  contractId: string;
  fleets: CharterFleet[];
  onClose: () => void;
}) {
  const { marinaId } = useCurrentMarina();
  const contract = useContract(marinaId, contractId);
  if (!contract.data) return null;
  return <RotationBoard contract={contract.data} fleets={fleets} onClose={onClose} />;
}

/**
 * Rotation board (spec 13.6): the contract's resource lines (rows) and the fleet vessels; drag a vessel onto a line
 * (or pick it in the select) to rotate from a night. Never repriced.
 */
function RotationBoard({
  contract,
  fleets,
  onClose,
}: {
  contract: Contract;
  fleets: CharterFleet[];
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const rotate = useRotateCharter(marinaId);
  const fleet = fleets.find((f) => f.id === contract.charter?.fleet_id);
  const vessels = (fleet?.vessels ?? []).filter((v) => v.active).map((v) => v.vessel);
  const lines = contract.lines.filter((l) => l.line_kind === 'resource');
  const current: Rotation = Object.fromEntries(
    lines.map((l) => {
      const alloc = contract.allocations.find(
        (a) =>
          (a.contract_line_id ?? '') === l.id && a.status === 'active' && a.last_night >= today,
      );
      return [l.id, alloc?.vessel_id ?? ''];
    }),
  );
  const [fromNight, setFromNight] = useState<string | null>(today);
  const [next, setNext] = useState<Rotation>(current);
  const assignments = rotationAssignments(current, next);
  const onDrop = (lineId: string) => (e: DragEvent) => {
    e.preventDefault();
    const vesselId = e.dataTransfer.getData('text/plain');
    if (vesselId) setNext(dropVessel(next, lineId, vesselId));
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('charter:rotation.title', { code: contract.code })}</DialogTitle>
          <DialogDescription>{t('charter:rotation.description')}</DialogDescription>
        </DialogHeader>
        <Field className="w-auto">
          <FieldLabel htmlFor="rotation-from">{t('charter:rotation.fromNight')}</FieldLabel>
          <DatePicker id="rotation-from" value={fromNight} onChange={setFromNight} />
        </Field>
        <ul className="flex flex-wrap gap-2" aria-label={t('charter:rotation.vessels')}>
          {vessels.map((v) => (
            <li
              key={v.id}
              draggable
              onDragStart={(e) => e.dataTransfer.setData('text/plain', v.id)}
              className="bg-secondary cursor-grab rounded-md px-2 py-1 text-sm"
            >
              {v.name}
            </li>
          ))}
        </ul>
        <div className="flex flex-col gap-2">
          {lines.map((l) => (
            <div
              key={l.id}
              onDragOver={(e) => e.preventDefault()}
              onDrop={onDrop(l.id)}
              className="flex flex-wrap items-end gap-3 rounded-md border p-2"
              data-testid="rotation-line"
            >
              <span className="min-w-20 font-medium">{l.resource_code ?? l.resource_id}</span>
              <EnumSelect
                id={`rotation-${l.id}`}
                label={t('charter:wizard.vessel')}
                values={vessels.map((v) => v.id)}
                labelOf={(id) => vessels.find((v) => v.id === id)?.name ?? id}
                value={next[l.id] ?? ''}
                onChange={(id) => id && setNext(dropVessel(next, l.id, id))}
              />
              {next[l.id] !== current[l.id] ? <Badge>{t('charter:rotation.changed')}</Badge> : null}
            </div>
          ))}
        </div>
        <M4ProblemAlert error={rotate.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!fromNight || assignments.length === 0 || rotate.isPending}
            onClick={() =>
              fromNight &&
              rotate.mutate(
                {
                  id: contract.id,
                  version: contract.version,
                  body: { from_night: fromNight, assignments },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('charter:rotation.apply')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
