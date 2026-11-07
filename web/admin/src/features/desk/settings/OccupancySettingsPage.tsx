import { zodResolver } from '@hookform/resolvers/zod';
import { Trash2Icon } from 'lucide-react';
import { useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { toast } from 'sonner';

import {
  useClosures,
  useCreateClosure,
  useDeleteClosure,
  useOccupancySettings,
  useUpdateOccupancySettings,
  type OccupancySettings,
  type PlacementInput,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  DatePicker,
  FormField,
  LengthInput,
  PageHeader,
  PercentInput,
} from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { IntegerInput } from '../../pricing/components/IntegerInput';
import { useMarinaToday } from '../../registry/lib/today';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { ResourceField } from '../components/ResourceField';
import { isIsoDate } from '../lib/dates';
import {
  SETTINGS_FIELDS,
  occupancySettingsSchema,
  settingsBody,
  settingsDefaults,
  type OccupancySettingsIn,
  type OccupancySettingsOut,
} from './schema';

export function OccupancySettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const settings = useOccupancySettings(marinaId);
  return (
    <RequirePermission allowed={can('harbor.configure')}>
      <PageHeader title={t('desk:settings.title')} description={t('desk:settings.description')} />
      <div className="flex flex-col gap-6">
        {settings.isPending ? (
          <LoadingState />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SettingsForm key={settings.data.version} settings={settings.data} />
        )}
        <ClosuresCard />
      </div>
    </RequirePermission>
  );
}

function SettingsForm({ settings }: { settings: OccupancySettings }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateOccupancySettings(marinaId);
  const form = useForm<OccupancySettingsIn, unknown, OccupancySettingsOut>({
    resolver: zodResolver(occupancySettingsSchema),
    defaultValues: settingsDefaults(settings),
  });
  const submit = form.handleSubmit(async (v) => {
    try {
      await update.mutateAsync({ version: settings.version, body: settingsBody(v) });
      toast.success(t('common:status.saved'));
    } catch (err) {
      toastProblem(t, err);
    }
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:settings.rules')}</CardTitle>
        <CardDescription>{t('desk:settings.rulesHint')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={(e) => void submit(e)} noValidate data-testid="desk-occupancy-settings">
          <FieldGroup>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {SETTINGS_FIELDS.map((f) => (
                <FormField
                  key={f.name}
                  control={form.control}
                  name={f.name}
                  label={t(`desk:settings.fields.${f.name}`)}
                  render={({ value, onChange, onBlur, id, ...rest }) =>
                    f.kind === 'length' ? (
                      <LengthInput
                        id={id}
                        value={value as number | null}
                        onChange={onChange}
                        onBlur={onBlur}
                        locale={i18n.language}
                        aria-invalid={rest['aria-invalid']}
                      />
                    ) : f.kind === 'percent' ? (
                      <PercentInput
                        id={id}
                        value={value as number | null}
                        onChange={onChange}
                        onBlur={onBlur}
                        locale={i18n.language}
                        aria-invalid={rest['aria-invalid']}
                      />
                    ) : (
                      <IntegerInput
                        id={id}
                        value={value as number | null}
                        onChange={onChange}
                        onBlur={onBlur}
                        aria-invalid={rest['aria-invalid']}
                      />
                    )
                  }
                />
              ))}
              <FormField
                control={form.control}
                name="default_arrival_time"
                label={t('desk:settings.fields.default_arrival_time')}
                render={(field) => <Input type="time" {...field} />}
              />
              <FormField
                control={form.control}
                name="default_departure_time"
                label={t('desk:settings.fields.default_departure_time')}
                render={(field) => <Input type="time" {...field} />}
              />
              <FormField
                control={form.control}
                name="release_policy"
                label={t('desk:settings.fields.release_policy')}
                render={({ value, onChange, id }) => (
                  <Select value={value} onValueChange={onChange}>
                    <SelectTrigger id={id} className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {(['with_consent', 'never'] as const).map((p) => (
                        <SelectItem key={p} value={p}>
                          {t(`occupancyEnums:releasePolicy.${p}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
              <Controller
                control={form.control}
                name="reprice_on_move"
                render={({ field }) => (
                  <Field orientation="horizontal" className="sm:pt-6">
                    <Switch
                      id="settings-reprice"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                    <FieldLabel htmlFor="settings-reprice">
                      {t('desk:settings.fields.reprice_on_move')}
                    </FieldLabel>
                  </Field>
                )}
              />
            </div>
            <div>
              <Button type="submit" disabled={update.isPending}>
                {t('common:actions.save')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  );
}

const SCOPES = ['marina', 'resource'] as const satisfies readonly Schemas['ClosureScope'][];

/** Closures of new arrivals (10.8, D18): marina-wide or on one resource, with a reason. */
function ClosuresCard() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const closures = useClosures(marinaId, { from: today });
  const create = useCreateClosure(marinaId);
  const remove = useDeleteClosure(marinaId);
  const [scope, setScope] = useState<(typeof SCOPES)[number]>('marina');
  const [target, setTarget] = useState<PlacementInput | null>(null);
  const [from, setFrom] = useState<string | null>(today);
  const [to, setTo] = useState<string | null>(today);
  const [reason, setReason] = useState('');
  const valid =
    isIsoDate(from) &&
    isIsoDate(to) &&
    from <= to &&
    reason.trim() !== '' &&
    (scope === 'marina' || target !== null);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('desk:closures.title')}</CardTitle>
        <CardDescription>{t('desk:closures.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {closures.isPending ? (
          <LoadingState />
        ) : closures.isError ? (
          <QueryErrorState error={closures.error} />
        ) : closures.data.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('desk:closures.empty')}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('desk:closures.scope')}</TableHead>
                <TableHead>{t('desk:columns.period')}</TableHead>
                <TableHead>{t('desk:fields.reason')}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {closures.data.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>{t(`occupancyEnums:closureScope.${c.scope_type}`)}</TableCell>
                  <TableCell>{`${formatIsoDate(c.from_date)} – ${formatIsoDate(c.to_date)}`}</TableCell>
                  <TableCell>{c.reason}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t('desk:closures.delete')}
                      disabled={remove.isPending}
                      onClick={() => remove.mutate({ id: c.id, version: c.version })}
                    >
                      <Trash2Icon />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <DeskProblemAlert error={remove.error} />
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (!valid) return;
            create.mutate(
              {
                scope_type: scope,
                from_date: from,
                to_date: to,
                reason: reason.trim(),
                ...(scope === 'resource' && target
                  ? { scope_id: target.resource_id, resource_type: target.resource_type }
                  : {}),
              },
              { onSuccess: () => setReason('') },
            );
          }}
        >
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="closure-scope">{t('desk:closures.scope')}</FieldLabel>
              <Select value={scope} onValueChange={(v) => setScope(v as typeof scope)}>
                <SelectTrigger id="closure-scope" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SCOPES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {t(`occupancyEnums:closureScope.${s}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="closure-from">{t('desk:release.from')}</FieldLabel>
              <DatePicker id="closure-from" value={from} onChange={setFrom} />
            </Field>
            <Field>
              <FieldLabel htmlFor="closure-to">{t('desk:release.to')}</FieldLabel>
              <DatePicker id="closure-to" value={to} onChange={setTo} />
            </Field>
          </div>
          {scope === 'resource' ? (
            <ResourceField
              marinaId={marinaId}
              idPrefix="closure"
              value={target}
              onChange={setTarget}
              date={from ?? today}
              from={from ?? today}
              to={to ?? today}
            />
          ) : null}
          <Field>
            <FieldLabel htmlFor="closure-reason">{t('desk:fields.reason')}</FieldLabel>
            <Input
              id="closure-reason"
              value={reason}
              maxLength={200}
              onChange={(e) => setReason(e.target.value)}
            />
            {!valid && reason.trim() === '' ? (
              <FieldError>{t('common:validation.required')}</FieldError>
            ) : null}
          </Field>
          <DeskProblemAlert error={create.error} />
          <div>
            <Button type="submit" disabled={!valid || create.isPending}>
              {t('desk:closures.create')}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
