import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateRentalSkipper,
  usePutRentalSkipperHours,
  useRentalSettings,
  useRentalSkippers,
  useUpdateRentalSkipper,
  type RentalSkipper,
  type RentalSkipperHoursRow,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { IsoDate, M4ProblemAlert } from '../../contracts-m4/components/kit';
import { WEEKDAYS, defaultHours, hoursValid, slotLabel } from '../lib';
import { SlotSelect } from './kit';

/** Hours of a skipper as text: "Lun 08:00-19:00, …". */
function hoursText(rows: readonly RentalSkipperHoursRow[], t: (k: string) => string): string {
  return [...rows]
    .sort((a, b) => a.weekday - b.weekday || a.from_slot - b.from_slot)
    .map(
      (r) =>
        `${t(`rentalFleet:enums.weekdayShort.${r.weekday}`)} ${slotLabel(r.from_slot)}-${slotLabel(r.to_slot)}`,
    )
    .join(', ');
}

/** Bookable skippers (m11-plan §3.2): contact (shown to rental.manage only), licence expiry, weekly hours. */
export function SkippersTab({ marinaId }: { marinaId: string }) {
  const { t } = useT();
  const list = useRentalSkippers(marinaId);
  const [editing, setEditing] = useState<RentalSkipper | 'new' | null>(null);
  const [hours, setHours] = useState<RentalSkipper | null>(null);
  const items = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-3">
      <p className="text-muted-foreground text-sm">{t('rentalFleet:skippers.hint')}</p>
      <div className="flex justify-end">
        <Button onClick={() => setEditing('new')}>
          <PlusIcon />
          {t('rentalFleet:skippers.new')}
        </Button>
      </div>
      {list.isPending ? (
        <LoadingState rows={3} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('rentalFleet:fields.name'),
            t('rentalFleet:fields.phone'),
            t('rentalFleet:fields.licenceExpires'),
            t('rentalFleet:fields.hours'),
            '',
            '',
          ]}
          empty={t('rentalFleet:skippers.empty')}
          rows={items.length}
        >
          {items.map((k) => (
            <TableRow key={k.id} data-testid="skipper-row">
              <TableCell className="font-medium">
                {k.name}{' '}
                {!k.active ? (
                  <Badge variant="outline">{t('rentalFleet:skippers.inactive')}</Badge>
                ) : null}
              </TableCell>
              <TableCell>{k.phone ?? k.email ?? ''}</TableCell>
              <TableCell>
                <IsoDate date={k.licence_expires_on} />
              </TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {k.hours.length ? hoursText(k.hours, t) : t('rentalFleet:skippers.noHours')}
              </TableCell>
              <TableCell>
                <Button size="sm" variant="outline" onClick={() => setHours(k)}>
                  {t('rentalFleet:skippers.editHours')}
                </Button>
              </TableCell>
              <TableCell className="text-right">
                <Button size="sm" variant="outline" onClick={() => setEditing(k)}>
                  {t('common:actions.edit')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      {editing ? (
        <SkipperDialog
          marinaId={marinaId}
          skipper={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
      {hours ? (
        <HoursDialog marinaId={marinaId} skipper={hours} onClose={() => setHours(null)} />
      ) : null}
    </div>
  );
}

function SkipperDialog({
  marinaId,
  skipper,
  onClose,
}: {
  marinaId: string;
  skipper: RentalSkipper | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateRentalSkipper(marinaId);
  const update = useUpdateRentalSkipper(marinaId);
  const [name, setName] = useState(skipper?.name ?? '');
  const [phone, setPhone] = useState(skipper?.phone ?? '');
  const [email, setEmail] = useState(skipper?.email ?? '');
  const [expires, setExpires] = useState<string | null>(skipper?.licence_expires_on ?? null);
  const [active, setActive] = useState(skipper?.active ?? true);
  const save = () => {
    const body = { name: name.trim(), phone: phone.trim(), email: email.trim(), active };
    if (skipper)
      update.mutate(
        {
          id: skipper.id,
          version: skipper.version,
          body: { ...body, ...(expires ? { licence_expires_on: expires } : {}) },
        },
        { onSuccess: onClose },
      );
    else
      create.mutate(
        { ...body, ...(expires ? { licence_expires_on: expires } : {}) },
        { onSuccess: onClose },
      );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {skipper ? t('rentalFleet:skippers.edit') : t('rentalFleet:skippers.new')}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field className="sm:col-span-2">
            <FieldLabel htmlFor="sk-name">{t('rentalFleet:fields.name')}</FieldLabel>
            <Input
              id="sk-name"
              value={name}
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sk-phone">{t('rentalFleet:fields.phone')}</FieldLabel>
            <Input
              id="sk-phone"
              type="tel"
              value={phone}
              maxLength={32}
              onChange={(e) => setPhone(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sk-email">{t('rentalFleet:fields.email')}</FieldLabel>
            <Input
              id="sk-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="sk-expires">{t('rentalFleet:fields.licenceExpires')}</FieldLabel>
            <DatePicker id="sk-expires" value={expires} onChange={setExpires} />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="sk-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="sk-active">{t('rentalFleet:fields.active')}</Label>
          </div>
        </div>
        <M4ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button disabled={!name.trim() || create.isPending || update.isPending} onClick={save}>
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** The weekly calendar of a skipper (a soft rule; days off are blocks on the planner). */
function HoursDialog({
  marinaId,
  skipper,
  onClose,
}: {
  marinaId: string;
  skipper: RentalSkipper;
  onClose: () => void;
}) {
  const { t } = useT();
  const put = usePutRentalSkipperHours(marinaId);
  const settings = useRentalSettings(marinaId);
  const [rows, setRows] = useState<RentalSkipperHoursRow[]>(skipper.hours);
  const openFrom = settings.data?.open_from_slot ?? 16;
  const openTo = settings.data?.open_to_slot ?? 38;
  const set = (i: number, patch: Partial<RentalSkipperHoursRow>) =>
    setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const valid = hoursValid(rows);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('rentalFleet:skippers.hoursTitle', { name: skipper.name })}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-2" data-testid="hours-editor">
          {rows.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('rentalFleet:skippers.noHours')}</p>
          ) : null}
          {rows.map((r, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2">
              <Field className="w-auto min-w-36">
                <FieldLabel htmlFor={`h-${i}-day`}>{t('rentalFleet:fields.weekday')}</FieldLabel>
                <Select
                  value={String(r.weekday)}
                  onValueChange={(v) => set(i, { weekday: Number(v) })}
                >
                  <SelectTrigger id={`h-${i}-day`}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {WEEKDAYS.map((d) => (
                      <SelectItem key={d} value={String(d)}>
                        {t(`rentalFleet:enums.weekday.${d}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <SlotSelect
                id={`h-${i}-from`}
                label={t('rentalFleet:fields.from')}
                value={r.from_slot}
                onChange={(v) => set(i, { from_slot: v })}
                max={47}
              />
              <SlotSelect
                id={`h-${i}-to`}
                label={t('rentalFleet:fields.to')}
                value={r.to_slot}
                onChange={(v) => set(i, { to_slot: v })}
                min={1}
              />
              <Button
                size="icon"
                variant="ghost"
                aria-label={t('common:actions.remove')}
                onClick={() => setRows(rows.filter((_, j) => j !== i))}
              >
                <TrashIcon />
              </Button>
            </div>
          ))}
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                setRows([...rows, { weekday: 1, from_slot: openFrom, to_slot: openTo }])
              }
            >
              <PlusIcon />
              {t('common:actions.add')}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRows(defaultHours(openFrom, openTo))}
            >
              {t('rentalFleet:skippers.defaultHours')}
            </Button>
          </div>
          {!valid ? (
            <p className="text-destructive text-sm">{t('rentalFleet:skippers.hoursInvalid')}</p>
          ) : null}
        </div>
        <M4ProblemAlert error={put.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!valid || put.isPending}
            onClick={() =>
              put.mutate(
                { id: skipper.id, version: skipper.version, hours: rows },
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
