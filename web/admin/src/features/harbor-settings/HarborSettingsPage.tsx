import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';
import { useFieldArray, useForm } from 'react-hook-form';
import { toast } from 'sonner';

import {
  useBerthGroups,
  useCreateBerthGroup,
  useDeleteBerthGroup,
  useHarborSettings,
  useUpdateBerthGroup,
  useUpdateHarborSettings,
  type BerthGroup,
  type HarborSettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { FieldGroup } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  ConfirmDialog,
  FormField,
  LengthInput,
  PageHeader,
  ProblemAlert,
} from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
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
import { toastProblem } from '../../lib/toast';
import { localName } from '../maps/lib';
import {
  GROUP_KINDS,
  groupSchema,
  settingsDefaults,
  settingsPatch,
  settingsSchema,
  type GroupForm,
  type SettingsIn,
  type SettingsOut,
} from './schema';

/** Harbour settings (m1-plan §5.1): berth groups, price-class rule, LOA tolerance, default grid. */
export function HarborSettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const settings = useHarborSettings(marinaId);
  return (
    <RequirePermission allowed={can('harbor.configure')}>
      <PageHeader title={t('maps:settings.title')} description={t('maps:settings.description')} />
      <div className="flex flex-col gap-6">
        {settings.isPending ? (
          <LoadingState />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SettingsForm key={settings.data.version} settings={settings.data} />
        )}
        <GroupsCard />
      </div>
    </RequirePermission>
  );
}

function SettingsForm({ settings }: { settings: HarborSettings }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateHarborSettings(marinaId);
  const form = useForm<SettingsIn, unknown, SettingsOut>({
    resolver: zodResolver(settingsSchema),
    defaultValues: settingsDefaults(settings),
  });
  const bands = useFieldArray({ control: form.control, name: 'bands' });
  const m = t('maps:units.m');
  const submit = form.handleSubmit(async (v) => {
    try {
      await update.mutateAsync({ version: settings.version, patch: settingsPatch(v) });
      toast.success(t('common:status.saved'));
    } catch (err) {
      toastProblem(t, err);
    }
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('maps:settings.rulesTitle')}</CardTitle>
        <CardDescription>{t('maps:settings.rulesHint')}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={(e) => void submit(e)} noValidate>
          <FieldGroup>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                control={form.control}
                name="loa_tolerance_cm"
                label={t('maps:settings.loaTolerance')}
                description={t('maps:settings.loaToleranceHint')}
                render={({ value, onChange, onBlur, id, ...rest }) => (
                  <LengthInput
                    id={id}
                    value={value}
                    onChange={onChange}
                    onBlur={onBlur}
                    unit={m}
                    locale={i18n.language}
                    aria-invalid={rest['aria-invalid']}
                  />
                )}
              />
              <FormField
                control={form.control}
                name="default_grid_cm"
                label={t('maps:settings.defaultGrid')}
                render={({ value, onChange, onBlur, id, ...rest }) => (
                  <LengthInput
                    id={id}
                    value={value}
                    onChange={onChange}
                    onBlur={onBlur}
                    unit={m}
                    locale={i18n.language}
                    aria-invalid={rest['aria-invalid']}
                  />
                )}
              />
            </div>
            <fieldset className="flex flex-col gap-3">
              <legend className="mb-2 text-sm font-medium">{t('maps:settings.bands')}</legend>
              {bands.fields.map((f, i) => (
                <div key={f.id} className="grid grid-cols-5 items-end gap-2">
                  <div className="col-span-2">
                    <FormField
                      control={form.control}
                      name={`bands.${i}.max_cm`}
                      label={t('maps:settings.bandMax')}
                      render={({ value, onChange, onBlur, id, ...rest }) => (
                        <LengthInput
                          id={id}
                          value={value}
                          onChange={onChange}
                          onBlur={onBlur}
                          unit={m}
                          locale={i18n.language}
                          aria-invalid={rest['aria-invalid']}
                        />
                      )}
                    />
                  </div>
                  <div className="col-span-2">
                    <FormField
                      control={form.control}
                      name={`bands.${i}.class`}
                      label={t('maps:settings.bandClass')}
                      render={(field) => <Input {...field} />}
                    />
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t('maps:settings.removeBand', { n: i + 1 })}
                    onClick={() => bands.remove(i)}
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              ))}
              <div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => bands.append({ max_cm: null, class: '' })}
                >
                  <PlusIcon />
                  {t('maps:settings.addBand')}
                </Button>
              </div>
            </fieldset>
            <FormField
              control={form.control}
              name="fallback"
              label={t('maps:settings.fallback')}
              description={t('maps:settings.fallbackHint')}
              render={(field) => <Input {...field} />}
            />
            {update.error ? <ProblemAlert error={update.error} /> : null}
            <div>
              <Button type="submit" disabled={update.isPending}>
                {update.isPending ? t('common:actions.saving') : t('common:actions.save')}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  );
}

function GroupsCard() {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const groups = useBerthGroups(marinaId);
  const remove = useDeleteBerthGroup(marinaId);
  const [editing, setEditing] = useState<BerthGroup | 'new' | null>(null);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('maps:groups.title')}</CardTitle>
        <CardDescription>{t('maps:groups.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div>
          <Button onClick={() => setEditing('new')}>
            <PlusIcon />
            {t('maps:groups.create')}
          </Button>
        </div>
        {groups.isPending ? (
          <LoadingState />
        ) : groups.isError ? (
          <QueryErrorState error={groups.error} onRetry={() => void groups.refetch()} />
        ) : groups.data.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('maps:groups.empty')}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('maps:groups.code')}</TableHead>
                <TableHead>{t('maps:groups.name')}</TableHead>
                <TableHead>{t('maps:groups.kind')}</TableHead>
                <TableHead>{t('maps:groups.members')}</TableHead>
                <TableHead>
                  <span className="sr-only">{t('common:actions.actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...groups.data]
                .sort((a, b) => a.sort_order - b.sort_order || a.code.localeCompare(b.code))
                .map((g) => (
                  <TableRow key={g.id}>
                    <TableCell className="font-medium">{g.code}</TableCell>
                    <TableCell>{localName(g.name, i18n.language)}</TableCell>
                    <TableCell>
                      <Badge variant="secondary">{t(`maps:groups.kinds.${g.kind}`)}</Badge>
                    </TableCell>
                    <TableCell>
                      {t('maps:groups.memberCount', {
                        count:
                          g.member_counts.berth +
                          g.member_counts.dynamic_area +
                          g.member_counts.land_resource,
                      })}
                    </TableCell>
                    <TableCell className="flex justify-end gap-2">
                      <Button variant="outline" size="sm" onClick={() => setEditing(g)}>
                        {t('common:actions.edit')}
                      </Button>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="sm">
                            {t('common:actions.delete')}
                          </Button>
                        }
                        title={t('maps:groups.deleteTitle', { code: g.code })}
                        description={t('maps:groups.deleteHint')}
                        confirmLabel={t('common:actions.delete')}
                        cancelLabel={t('common:actions.cancel')}
                        destructive
                        onConfirm={() =>
                          remove.mutate(g, { onError: (err) => toastProblem(t, err) })
                        }
                      />
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      <Dialog open={editing !== null} onOpenChange={(open) => (open ? null : setEditing(null))}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing === 'new' ? t('maps:groups.create') : t('maps:groups.edit')}
            </DialogTitle>
          </DialogHeader>
          {editing ? (
            <GroupFormBody
              group={editing === 'new' ? null : editing}
              onDone={() => setEditing(null)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function GroupFormBody({ group, onDone }: { group: BerthGroup | null; onDone: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateBerthGroup(marinaId);
  const update = useUpdateBerthGroup(marinaId);
  const form = useForm<GroupForm>({
    resolver: zodResolver(groupSchema),
    defaultValues: {
      code: group?.code ?? '',
      name_it: group?.name.it ?? '',
      name_en: group?.name.en ?? '',
      kind: group?.kind ?? 'custom',
    },
  });
  const error = create.error ?? update.error;
  const submit = form.handleSubmit(async (v) => {
    const name = v.name_en ? { it: v.name_it, en: v.name_en } : { it: v.name_it };
    if (group) await update.mutateAsync({ group, patch: { code: v.code, name, kind: v.kind } });
    else await create.mutateAsync({ code: v.code, name, kind: v.kind });
    onDone();
  });
  return (
    <form onSubmit={(e) => void submit(e).catch(() => undefined)} noValidate>
      <FieldGroup>
        <FormField
          control={form.control}
          name="code"
          label={t('maps:groups.code')}
          render={(f) => <Input {...f} />}
        />
        <FormField
          control={form.control}
          name="name_it"
          label={t('maps:create.nameIt')}
          render={(f) => <Input {...f} />}
        />
        <FormField
          control={form.control}
          name="name_en"
          label={t('maps:create.nameEn')}
          render={(f) => <Input {...f} />}
        />
        <FormField
          control={form.control}
          name="kind"
          label={t('maps:groups.kind')}
          render={(field) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger id={field.id}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {GROUP_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`maps:groups.kinds.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
        {error ? <ProblemAlert error={error} /> : null}
      </FieldGroup>
      <DialogFooter className="mt-6">
        <Button type="button" variant="outline" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={create.isPending || update.isPending}>
          {t('common:actions.save')}
        </Button>
      </DialogFooter>
    </form>
  );
}
