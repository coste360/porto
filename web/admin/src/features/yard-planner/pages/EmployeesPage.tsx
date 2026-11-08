import { CalendarDaysIcon, PlaneIcon, PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import {
  useAbsences,
  useCreateAbsence,
  useCreateEmployee,
  useDeleteAbsence,
  useEmployees,
  usePutEmployeeCalendar,
  useUpdateEmployee,
  type AbsenceKind,
  type Employee,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
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
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { MoneyInput, PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, PageFrame } from '../../contracts-m4/components/kit';
import { YardPlannerNav } from '../components/kit';
import { useQualificationIndex } from '../lib/qualifications';
import { hoursLabel, parseHours } from '../lib/planner';

const ABSENCE_KINDS: readonly AbsenceKind[] = ['vacation', 'sick', 'leave', 'other'];
const WEEKDAYS = [1, 2, 3, 4, 5, 6, 7] as const;

/** Yard employees (m10-plan §6.1): qualifications, hourly cost, weekly calendar and absences. */
export function EmployeesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const list = useEmployees(marinaId);
  const quals = useQualificationIndex(marinaId);
  const manage = can('yard.manage');
  const [editing, setEditing] = useState<Employee | 'new' | null>(null);
  const [calendar, setCalendar] = useState<Employee | null>(null);
  const [absences, setAbsences] = useState<Employee | null>(null);
  const items = list.data?.items ?? [];
  return (
    <RequirePermission allowed={can('yard.read') || manage}>
      <PageFrame>
        <PageHeader
          title={t('yardPlanner:employees.title')}
          description={t('yardPlanner:employees.description')}
          actions={
            manage ? (
              <Button onClick={() => setEditing('new')}>
                <PlusIcon />
                {t('yardPlanner:employees.new')}
              </Button>
            ) : null
          }
        />
        <YardPlannerNav marinaId={marinaId} current="employees" />
        {list.isPending ? (
          <LoadingState rows={4} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('yardPlanner:fields.code'),
              t('yardPlanner:fields.name'),
              t('yardPlanner:fields.department'),
              t('yardPlanner:fields.qualifications'),
              t('yardPlanner:fields.week'),
              t('yardPlanner:fields.hourlyCost'),
              '',
            ]}
            empty={t('yardPlanner:employees.empty')}
            rows={items.length}
          >
            {items.map((e) => (
              <TableRow key={e.id} data-testid="employee-row">
                <TableCell className="font-medium">{e.code}</TableCell>
                <TableCell>
                  {e.first_name} {e.last_name}
                  {!e.active ? (
                    <Badge variant="outline" className="ml-2">
                      {t('yardPlanner:employees.inactive')}
                    </Badge>
                  ) : null}
                </TableCell>
                <TableCell>{e.department}</TableCell>
                <TableCell className="flex flex-wrap gap-1">
                  {e.qualification_ids.map((q) => (
                    <Badge key={q} variant="secondary">
                      {quals.byId.get(q)?.code ?? '—'}
                    </Badge>
                  ))}
                </TableCell>
                <TableCell>
                  {t('yardPlanner:employees.weekHours', {
                    hours: hoursLabel(
                      Math.round((e.calendar.reduce((s, d) => s + d.minutes, 0) * 1000) / 60),
                    ),
                  })}
                </TableCell>
                <TableCell>{formatCents(e.hourly_cost_cents)}</TableCell>
                <TableCell className="text-right">
                  {manage ? (
                    <div className="flex justify-end gap-1">
                      <Button size="sm" variant="outline" onClick={() => setEditing(e)}>
                        {t('yardPlanner:employees.edit')}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={t('yardPlanner:employees.calendar')}
                        onClick={() => setCalendar(e)}
                      >
                        <CalendarDaysIcon />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={t('yardPlanner:employees.absences')}
                        onClick={() => setAbsences(e)}
                      >
                        <PlaneIcon />
                      </Button>
                    </div>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        {editing ? (
          <EmployeeDialog
            marinaId={marinaId}
            employee={editing === 'new' ? null : editing}
            onClose={() => setEditing(null)}
          />
        ) : null}
        {calendar ? (
          <CalendarDialog
            marinaId={marinaId}
            employee={calendar}
            onClose={() => setCalendar(null)}
          />
        ) : null}
        {absences ? (
          <AbsencesDialog
            marinaId={marinaId}
            employee={absences}
            onClose={() => setAbsences(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

function EmployeeDialog({
  marinaId,
  employee,
  onClose,
}: {
  marinaId: string;
  employee: Employee | null;
  onClose: () => void;
}) {
  const { t } = useT();
  const create = useCreateEmployee(marinaId);
  const update = useUpdateEmployee(marinaId);
  const quals = useQualificationIndex(marinaId);
  const [code, setCode] = useState(employee?.code ?? '');
  const [first, setFirst] = useState(employee?.first_name ?? '');
  const [last, setLast] = useState(employee?.last_name ?? '');
  const [department, setDepartment] = useState(employee?.department ?? '');
  const [cost, setCost] = useState<number | null>(employee?.hourly_cost_cents ?? 0);
  const [active, setActive] = useState(employee?.active ?? true);
  const [selected, setSelected] = useState<string[]>(employee?.qualification_ids ?? []);
  const save = () => {
    const body = {
      code: code.trim(),
      first_name: first.trim(),
      last_name: last.trim(),
      department: department.trim(),
      hourly_cost_cents: cost ?? 0,
      active,
      qualification_ids: selected,
    };
    if (employee)
      update.mutate({ id: employee.id, version: employee.version, body }, { onSuccess: onClose });
    else create.mutate(body, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {employee ? t('yardPlanner:employees.edit') : t('yardPlanner:employees.new')}
          </DialogTitle>
          <DialogDescription>{t('yardPlanner:employees.dialogHint')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="emp-code">{t('yardPlanner:fields.code')}</FieldLabel>
            <Input
              id="emp-code"
              value={code}
              maxLength={16}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="emp-department">{t('yardPlanner:fields.department')}</FieldLabel>
            <Input
              id="emp-department"
              value={department}
              maxLength={40}
              onChange={(e) => setDepartment(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="emp-first">{t('yardPlanner:fields.firstName')}</FieldLabel>
            <Input
              id="emp-first"
              value={first}
              maxLength={60}
              onChange={(e) => setFirst(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="emp-last">{t('yardPlanner:fields.lastName')}</FieldLabel>
            <Input
              id="emp-last"
              value={last}
              maxLength={60}
              onChange={(e) => setLast(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="emp-cost">{t('yardPlanner:fields.hourlyCost')}</FieldLabel>
            <MoneyInput id="emp-cost" value={cost} onChange={setCost} />
          </Field>
          <div className="flex items-center gap-2">
            <Switch id="emp-active" checked={active} onCheckedChange={setActive} />
            <Label htmlFor="emp-active">{t('yardPlanner:fields.active')}</Label>
          </div>
        </div>
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">{t('yardPlanner:fields.qualifications')}</legend>
          {quals.list.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              {t('yardPlanner:employees.noQualifications')}
            </p>
          ) : null}
          {quals.list.map((q) => (
            <div key={q.id} className="flex min-h-9 items-center gap-2">
              <Checkbox
                id={`emp-q-${q.id}`}
                checked={selected.includes(q.id)}
                onCheckedChange={(v) =>
                  setSelected((s) => (v === true ? [...s, q.id] : s.filter((x) => x !== q.id)))
                }
              />
              <Label htmlFor={`emp-q-${q.id}`}>
                {q.code}
                {q.lift_operator ? (
                  <Badge variant="outline" className="ml-2">
                    {t('yardPlanner:employees.liftOperator')}
                  </Badge>
                ) : null}
              </Label>
            </div>
          ))}
        </fieldset>
        <ProblemAlert error={create.error ?? update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardPlanner:actions.cancel')}
          </Button>
          <Button onClick={save} disabled={create.isPending || update.isPending}>
            {t('yardPlanner:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CalendarDialog({
  marinaId,
  employee,
  onClose,
}: {
  marinaId: string;
  employee: Employee;
  onClose: () => void;
}) {
  const { t } = useT();
  const put = usePutEmployeeCalendar(marinaId);
  const [hours, setHours] = useState<string[]>(() =>
    WEEKDAYS.map((wd) => {
      const m = employee.calendar.find((d) => d.weekday === wd)?.minutes ?? 0;
      return m ? hoursLabel(Math.round((m * 1000) / 60)) : '0';
    }),
  );
  const minutes = hours.map((h) => (h.trim() === '0' || h.trim() === '' ? 0 : parseHours(h)));
  const valid = minutes.every((m) => m !== null);
  const save = () =>
    put.mutate(
      {
        id: employee.id,
        version: employee.version,
        days: WEEKDAYS.map((wd, i) => ({
          weekday: wd,
          minutes: Math.round(((minutes[i] ?? 0) * 60) / 1000),
        })),
      },
      { onSuccess: onClose },
    );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('yardPlanner:employees.calendar')}</DialogTitle>
          <DialogDescription>{t('yardPlanner:employees.calendarHint')}</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {WEEKDAYS.map((wd, i) => (
            <Field key={wd}>
              <FieldLabel htmlFor={`cal-${wd}`}>{t(`yardPlanner:weekdays.${wd}`)}</FieldLabel>
              <Input
                id={`cal-${wd}`}
                inputMode="decimal"
                value={hours[i]}
                aria-invalid={minutes[i] === null}
                onChange={(e) => setHours((h) => h.map((x, j) => (j === i ? e.target.value : x)))}
              />
            </Field>
          ))}
        </div>
        <ProblemAlert error={put.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardPlanner:actions.cancel')}
          </Button>
          <Button onClick={save} disabled={!valid || put.isPending}>
            {t('yardPlanner:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function AbsencesDialog({
  marinaId,
  employee,
  onClose,
}: {
  marinaId: string;
  employee: Employee;
  onClose: () => void;
}) {
  const { t } = useT();
  const list = useAbsences(marinaId, employee.id);
  const create = useCreateAbsence(marinaId);
  const del = useDeleteAbsence(marinaId);
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [kind, setKind] = useState<AbsenceKind | ''>('vacation');
  const [partial, setPartial] = useState('');
  const add = () => {
    const ph = partial.trim() ? parseHours(partial) : null;
    create.mutate(
      {
        employeeId: employee.id,
        body: {
          from_on: from,
          to_on: to || from,
          kind: kind || 'other',
          ...(ph ? { minutes_per_day: Math.round((ph * 60) / 1000) } : {}),
        },
      },
      {
        onSuccess: () => {
          setFrom('');
          setTo('');
          setPartial('');
        },
      },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('yardPlanner:employees.absences')}</DialogTitle>
          <DialogDescription>
            {employee.code} {employee.first_name} {employee.last_name}
          </DialogDescription>
        </DialogHeader>
        <ul className="flex flex-col gap-2" data-testid="absence-list">
          {(list.data?.items ?? []).map((a) => (
            <li
              key={a.id}
              className="flex items-center justify-between gap-2 rounded-md border p-2 text-sm"
            >
              <span>
                {formatIsoDate(a.from_on)}
                {a.to_on !== a.from_on ? ` – ${formatIsoDate(a.to_on)}` : ''} ·{' '}
                {t(`yardPlanner:absenceKinds.${a.kind}`)}
                {a.minutes_per_day
                  ? ` · ${t('yardPlanner:employees.hoursPerDay', { hours: hoursLabel(Math.round((a.minutes_per_day * 1000) / 60)) })}`
                  : ''}
              </span>
              <Button
                size="sm"
                variant="ghost"
                aria-label={t('yardPlanner:actions.delete')}
                onClick={() => del.mutate({ employeeId: employee.id, absenceId: a.id })}
              >
                <Trash2Icon />
              </Button>
            </li>
          ))}
          {list.data && list.data.items.length === 0 ? (
            <li className="text-muted-foreground text-sm">
              {t('yardPlanner:employees.noAbsences')}
            </li>
          ) : null}
        </ul>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="abs-from">{t('yardPlanner:fields.from')}</FieldLabel>
            <Input
              id="abs-from"
              type="date"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="abs-to">{t('yardPlanner:fields.to')}</FieldLabel>
            <Input id="abs-to" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          </Field>
          <EnumSelect
            id="abs-kind"
            label={t('yardPlanner:fields.absenceKind')}
            values={ABSENCE_KINDS}
            value={kind}
            onChange={setKind}
            labelOf={(k) => t(`yardPlanner:absenceKinds.${k}`)}
          />
          <Field>
            <FieldLabel htmlFor="abs-partial">{t('yardPlanner:fields.partialHours')}</FieldLabel>
            <Input
              id="abs-partial"
              inputMode="decimal"
              value={partial}
              onChange={(e) => setPartial(e.target.value)}
            />
          </Field>
        </div>
        <ProblemAlert error={create.error ?? del.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('yardPlanner:actions.close')}
          </Button>
          <Button onClick={add} disabled={!from || create.isPending}>
            {t('yardPlanner:employees.addAbsence')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
