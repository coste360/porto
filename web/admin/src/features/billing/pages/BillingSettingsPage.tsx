import { useState } from 'react';
import { toast } from 'sonner';

import {
  useAllItems,
  useBillingSettings,
  useCashRegisters,
  useCreateCashRegister,
  useUpdateBillingSettings,
  useUpdateCashRegister,
  type BillingSettings,
  type BillingSettingsInput,
  type ReminderLevel,
} from '@porto/shared/api';
import type { components } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { MoneyInput } from '@porto/shared/money';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ElementCombobox, PageHeader } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  BillingProblemAlert,
  EnumSelect,
  PageFrame,
  Section,
  SimpleTable,
} from '../components/kit';
import { ORIGIN_ITEM_KEYS, toSettingsInput } from '../lib/catalog';
import { itemName } from '../lib/drafts';

type OriginItemKey = components['schemas']['OriginItemKey'];

/** Billing settings (m5-plan §9.1) and cash registers. */
export function BillingSettingsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const settings = useBillingSettings(marinaId);
  return (
    <RequirePermission allowed={can('marina.configure')}>
      <PageFrame>
        <PageHeader
          title={t('billingSettings:title')}
          description={t('billingSettings:description')}
        />
        {settings.isPending ? (
          <LoadingState rows={6} />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SettingsForm key={settings.data.version} settings={settings.data} />
        )}
        <Registers />
      </PageFrame>
    </RequirePermission>
  );
}

function NumberField({
  id,
  label,
  value,
  min,
  max,
  onChange,
}: {
  id: string;
  label: string;
  value: number;
  min: number;
  max: number;
  onChange: (n: number) => void;
}) {
  return (
    <Field className="w-44">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={min}
        max={max}
        value={value}
        onChange={(e) => {
          const n = Number(e.target.value);
          if (Number.isInteger(n)) onChange(Math.max(min, Math.min(max, n)));
        }}
      />
    </Field>
  );
}

function SettingsForm({ settings }: { settings: BillingSettings }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateBillingSettings(marinaId);
  const items = useAllItems(marinaId);
  const [f, setF] = useState<BillingSettingsInput>(() => toSettingsInput(settings));
  const options = (items.data ?? [])
    .filter((i) => i.active)
    .map((i) => ({ value: i.id, label: `${i.code} · ${itemName(i, i18n.language)}` }));
  const originItem = (key: OriginItemKey) =>
    f.origin_items.find((o) => o.origin_key === key)?.item_id ?? null;
  const setOriginItem = (key: OriginItemKey, itemId: string | null) =>
    setF({
      ...f,
      origin_items: [
        ...f.origin_items.filter((o) => o.origin_key !== key),
        ...(itemId ? [{ origin_key: key, item_id: itemId }] : []),
      ],
    });
  const setLevel = (level: ReminderLevel, patch: { days?: number; enabled?: boolean }) =>
    setF({
      ...f,
      reminder_policy: {
        levels: f.reminder_policy.levels.map((l) => (l.level === level ? { ...l, ...patch } : l)),
      },
    });
  return (
    <>
      <Section title={t('billingSettings:charges.title')}>
        <div className="flex flex-wrap items-end gap-3">
          <NumberField
            id="bs-due"
            label={t('billingSettings:charges.dueDays')}
            value={f.default_due_days}
            min={0}
            max={365}
            onChange={(default_due_days) => setF({ ...f, default_due_days })}
          />
          <NumberField
            id="bs-export"
            label={t('billingSettings:charges.exportReminderDays')}
            value={f.export_reminder_days}
            min={1}
            max={365}
            onChange={(export_reminder_days) => setF({ ...f, export_reminder_days })}
          />
          <NumberField
            id="bs-sepa"
            label={t('billingSettings:charges.sepaLeadDays')}
            value={f.sepa_lead_days}
            min={0}
            max={30}
            onChange={(sepa_lead_days) => setF({ ...f, sepa_lead_days })}
          />
        </div>
        <label className="flex items-center gap-2 text-sm">
          <Switch
            checked={f.auto_payment_request}
            onCheckedChange={(auto_payment_request) => setF({ ...f, auto_payment_request })}
          />
          {t('billingSettings:charges.autoRequest')}
        </label>
      </Section>
      <Section
        title={t('billingSettings:originItems.title')}
        description={t('billingSettings:originItems.description')}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          {ORIGIN_ITEM_KEYS.map((key) => (
            <Field key={key}>
              <FieldLabel htmlFor={`bs-origin-${key}`}>
                {t(`billingEnums:originItemKey.${key}`)}
              </FieldLabel>
              <ElementCombobox
                id={`bs-origin-${key}`}
                options={options}
                value={originItem(key)}
                onChange={(v) => setOriginItem(key, v)}
                placeholder={t('billingSettings:originItems.none')}
                searchPlaceholder={t('charges:lines.itemSearch')}
                emptyText={t('charges:lines.noItems')}
              />
            </Field>
          ))}
        </div>
      </Section>
      <Section
        title={t('billingSettings:reminders.title')}
        description={t('billingSettings:reminders.description')}
      >
        <NumberField
          id="bs-hour"
          label={t('billingSettings:reminders.hour')}
          value={f.reminder_hour}
          min={0}
          max={23}
          onChange={(reminder_hour) => setF({ ...f, reminder_hour })}
        />
        <SimpleTable
          head={[
            t('billingSettings:reminders.level'),
            t('billingSettings:reminders.days'),
            t('billingSettings:reminders.enabled'),
          ]}
          empty=""
          rows={f.reminder_policy.levels.length}
        >
          {f.reminder_policy.levels.map((l) => (
            <TableRow key={l.level} data-testid="reminder-level">
              <TableCell>{t(`billingEnums:reminderLevel.${l.level}`)}</TableCell>
              <TableCell className="w-32">
                <Input
                  type="number"
                  min={-60}
                  max={365}
                  aria-label={t('billingSettings:reminders.daysOf', {
                    level: t(`billingEnums:reminderLevel.${l.level}`),
                  })}
                  value={l.days}
                  onChange={(e) => {
                    const n = Number(e.target.value);
                    if (Number.isInteger(n))
                      setLevel(l.level, { days: Math.max(-60, Math.min(365, n)) });
                  }}
                />
              </TableCell>
              <TableCell>
                <Switch
                  aria-label={t('billingSettings:reminders.enabledOf', {
                    level: t(`billingEnums:reminderLevel.${l.level}`),
                  })}
                  checked={l.enabled}
                  onCheckedChange={(enabled) => setLevel(l.level, { enabled })}
                />
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
        <p className="text-muted-foreground text-xs">{t('billingSettings:reminders.finalHint')}</p>
      </Section>
      <Section
        title={t('billingSettings:arrears.title')}
        description={t('billingSettings:arrears.description')}
      >
        <label className="flex items-center gap-2 text-sm">
          <Switch
            checked={f.arrears_policy.enabled}
            onCheckedChange={(enabled) =>
              setF({ ...f, arrears_policy: { ...f.arrears_policy, enabled } })
            }
          />
          {t('billingSettings:arrears.enabled')}
        </label>
        <div className="flex flex-wrap items-end gap-3">
          <NumberField
            id="bs-arrears-days"
            label={t('billingSettings:arrears.days')}
            value={f.arrears_policy.days_overdue}
            min={1}
            max={365}
            onChange={(days_overdue) =>
              setF({ ...f, arrears_policy: { ...f.arrears_policy, days_overdue } })
            }
          />
          <Field className="w-44">
            <FieldLabel htmlFor="bs-arrears-min">{t('billingSettings:arrears.min')}</FieldLabel>
            <MoneyInput
              id="bs-arrears-min"
              locale={i18n.language}
              value={f.arrears_policy.min_amount_cents}
              onValueChange={(v) =>
                setF({
                  ...f,
                  arrears_policy: { ...f.arrears_policy, min_amount_cents: Math.max(0, v ?? 0) },
                })
              }
            />
          </Field>
        </div>
      </Section>
      <Section title={t('billingSettings:fiscal.title')}>
        <EnumSelect
          id="bs-fiscal"
          label={t('billingSettings:fiscal.driver')}
          values={settings.fiscal_drivers}
          labelOf={(d) => t(`billingEnums:fiscalDriver.${d}`, d)}
          value={f.fiscal_driver}
          onChange={(fiscal_driver) => fiscal_driver && setF({ ...f, fiscal_driver })}
        />
        <Field>
          <FieldLabel htmlFor="bs-footer">{t('billingSettings:fiscal.footer')}</FieldLabel>
          <Textarea
            id="bs-footer"
            maxLength={1000}
            value={f.receipt_footer}
            onChange={(e) => setF({ ...f, receipt_footer: e.target.value })}
          />
        </Field>
      </Section>
      <BillingProblemAlert error={update.error} />
      <div className="flex justify-end">
        <Button
          disabled={update.isPending}
          onClick={() =>
            update.mutate(
              { version: settings.version, body: f },
              { onSuccess: () => toast.success(t('billingSettings:saved')) },
            )
          }
        >
          {t('common:actions.save')}
        </Button>
      </div>
    </>
  );
}

function Registers() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const list = useCashRegisters(marinaId);
  const create = useCreateCashRegister(marinaId);
  const update = useUpdateCashRegister(marinaId);
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const items = list.data?.items ?? [];
  const validCode = /^[a-z0-9_-]{1,32}$/.test(code);
  return (
    <Section title={t('billingSettings:registers.title')}>
      {list.isPending ? (
        <LoadingState />
      ) : (
        <SimpleTable
          head={[
            t('billingSettings:registers.code'),
            t('billingSettings:registers.name'),
            t('billingSettings:registers.active'),
            t('billingSettings:registers.default'),
          ]}
          empty={t('billingSettings:registers.empty')}
          rows={items.length}
        >
          {items.map((r) => (
            <TableRow key={r.id} data-testid="register-row">
              <TableCell>{r.code}</TableCell>
              <TableCell>{r.name}</TableCell>
              <TableCell>
                <Switch
                  aria-label={t('billingSettings:registers.activeOf', { name: r.name })}
                  checked={r.active}
                  disabled={update.isPending}
                  onCheckedChange={(active) =>
                    update.mutate({ id: r.id, version: r.version, patch: { active } })
                  }
                />
              </TableCell>
              <TableCell>
                {r.is_default ? (
                  t('billingSettings:registers.isDefault')
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={update.isPending}
                    onClick={() =>
                      update.mutate({ id: r.id, version: r.version, patch: { is_default: true } })
                    }
                  >
                    {t('billingSettings:registers.makeDefault')}
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
      <div className="flex flex-wrap items-end gap-3">
        <Field className="w-40">
          <FieldLabel htmlFor="reg-code">{t('billingSettings:registers.code')}</FieldLabel>
          <Input
            id="reg-code"
            maxLength={32}
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        </Field>
        <Field className="w-64">
          <FieldLabel htmlFor="reg-name">{t('billingSettings:registers.name')}</FieldLabel>
          <Input
            id="reg-name"
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Button
          variant="outline"
          disabled={!validCode || !name.trim() || create.isPending}
          onClick={() =>
            create.mutate(
              { code, name: name.trim() },
              {
                onSuccess: () => {
                  setCode('');
                  setName('');
                },
              },
            )
          }
        >
          {t('billingSettings:registers.add')}
        </Button>
      </div>
      <BillingProblemAlert error={create.error ?? update.error ?? list.error} />
    </Section>
  );
}
