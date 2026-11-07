import { useState } from 'react';
import { toast } from 'sonner';

import {
  useContractSettings,
  useUpdateContractSettings,
  type ContractSettings,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldError, FieldLabel, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { MoneyInput, PageHeader, PercentInput } from '@porto/shared/ui/patterns';
import { Switch } from '@porto/shared/ui/switch';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { EnumSelect, M4ProblemAlert, PageFrame, Section } from './components/kit';
import { policyForm, policyFromForm, type PolicyForm } from './lib/policy';
import { OnlineFeatureNotice } from '../desktop/OnlineFeatureNotice';

const DRIVERS: readonly Schemas['SignatureDriver'][] = ['paper', 'otp', 'provider'];
const INSURANCE: readonly Schemas['InsurancePolicy'][] = ['warn', 'block'];
const REQUIRED: readonly Schemas['RequiredField'][] = [
  'address',
  'fiscal_code',
  'birth_date',
  'vessel_registration',
  'insurance_expiry',
  'insurance_document',
  'identity_document',
];
const MODES = ['single', 'interval', 'fixed_dates'] as const;
const ROUNDING = ['1', '100', '1000'] as const;
const DEPOSIT = ['none', 'bp', 'cents'] as const;

/** Contract settings (m4-plan §8.1): default policy, signature driver, insurance policy, bank details. */
export function ContractSettingsPage() {
  const { can, marinaId } = useCurrentMarina();
  const settings = useContractSettings(marinaId);
  return (
    <RequirePermission allowed={can('contracts.write')}>
      <PageFrame>
        {settings.isPending ? (
          <LoadingState rows={8} />
        ) : settings.isError ? (
          <QueryErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <SettingsForm key={settings.data.version} settings={settings.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function SettingsForm({ settings }: { settings: ContractSettings }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const save = useUpdateContractSettings(marinaId);
  const [p, setP] = useState<PolicyForm>(() => policyForm(settings.default_policy));
  const [s, setS] = useState(settings);
  const [invalid, setInvalid] = useState<string | null>(null);
  const num = (k: keyof ContractSettings) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setS({ ...s, [k]: /^\d{1,4}$/.test(e.target.value) ? Number(e.target.value) : 0 });
  const pf = <K extends keyof PolicyForm>(k: K, v: PolicyForm[K]) => setP({ ...p, [k]: v });
  const intField = (k: keyof PolicyForm, label: string) => (
    <Field className="w-40" data-invalid={invalid === k || undefined}>
      <FieldLabel htmlFor={`policy-${k}`}>{label}</FieldLabel>
      <Input
        id={`policy-${k}`}
        inputMode="numeric"
        value={String(p[k])}
        onChange={(e) => setP({ ...p, [k]: e.target.value })}
      />
    </Field>
  );
  const submit = () => {
    const r = policyFromForm(p, settings.default_policy);
    if (!r.ok) {
      setInvalid(r.field);
      return;
    }
    setInvalid(null);
    save.mutate(
      {
        version: settings.version,
        body: {
          default_policy: r.policy,
          signature_driver: s.signature_driver,
          provider_code: s.provider_code ?? null,
          marina_countersign: s.marina_countersign,
          require_paper_scan: s.require_paper_scan,
          signature_days: s.signature_days,
          signature_reminder_days: s.signature_reminder_days,
          renewal_deadline_days: s.renewal_deadline_days,
          arrears_days: s.arrears_days,
          insurance_policy: s.insurance_policy,
          required_fields: s.required_fields,
          bank_details: s.bank_details ?? {},
          office_email: s.office_email?.trim() || null,
        },
      },
      { onSuccess: () => toast.success(t('contractsM4:settings.saved')) },
    );
  };
  return (
    <>
      <PageHeader
        title={t('contractsM4:settings.title')}
        description={t('contractsM4:settings.description')}
        actions={
          <Button disabled={save.isPending} onClick={submit}>
            {t('common:actions.save')}
          </Button>
        }
      />
      <OnlineFeatureNotice feature="remoteSignature" />
      {invalid ? <FieldError>{t('contractsM4:settings.invalid')}</FieldError> : null}
      <M4ProblemAlert error={save.error} />
      <Section
        title={t('contractsM4:settings.payment')}
        description={t('contractsM4:settings.paymentHint')}
      >
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="policy-deposit-kind"
            label={t('contractsM4:settings.depositKind')}
            values={DEPOSIT}
            labelOf={(v) => t(`contractsM4:settings.deposit.${v}`)}
            value={p.depositKind}
            onChange={(v) => pf('depositKind', v || 'none')}
          />
          {p.depositKind === 'bp' ? (
            <Field className="w-40" data-invalid={invalid === 'depositBp' || undefined}>
              <FieldLabel htmlFor="policy-deposit-bp">
                {t('contractsM4:settings.depositBp')}
              </FieldLabel>
              <PercentInput
                id="policy-deposit-bp"
                locale={i18n.language}
                value={p.depositBp}
                onChange={(v) => pf('depositBp', v)}
              />
            </Field>
          ) : null}
          {p.depositKind === 'cents' ? (
            <Field className="w-40" data-invalid={invalid === 'depositCents' || undefined}>
              <FieldLabel htmlFor="policy-deposit-cents">
                {t('contractsM4:settings.depositCents')}
              </FieldLabel>
              <MoneyInput
                id="policy-deposit-cents"
                locale={i18n.language}
                value={p.depositCents}
                onChange={(v) => pf('depositCents', v)}
              />
            </Field>
          ) : null}
          {intField('depositDueDays', t('contractsM4:settings.depositDueDays'))}
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="policy-mode"
            label={t('contractsM4:settings.mode')}
            values={MODES}
            labelOf={(v) => t(`contractsM4:settings.modes.${v}`)}
            value={p.mode}
            onChange={(v) => pf('mode', v || 'single')}
          />
          {p.mode === 'interval' ? (
            <>
              {intField('everyMonths', t('contractsM4:settings.everyMonths'))}
              {intField('count', t('contractsM4:settings.count'))}
              {intField('firstOffsetMonths', t('contractsM4:settings.firstOffsetMonths'))}
            </>
          ) : null}
          {p.mode === 'single'
            ? intField('dueOffsetDays', t('contractsM4:settings.dueOffsetDays'))
            : null}
          {intField('issueDaysBefore', t('contractsM4:settings.issueDaysBefore'))}
          <EnumSelect
            id="policy-rounding"
            label={t('contractsM4:settings.rounding')}
            values={ROUNDING}
            labelOf={(v) => t(`contractsM4:settings.roundings.${v}`)}
            value={p.roundingUnit}
            onChange={(v) => pf('roundingUnit', v || '1')}
          />
        </div>
        <Field orientation="horizontal">
          <Switch
            id="policy-early"
            checked={p.earlyEnabled}
            onCheckedChange={(v) => pf('earlyEnabled', v)}
          />
          <FieldLabel htmlFor="policy-early">{t('contractsM4:settings.early')}</FieldLabel>
        </Field>
        {p.earlyEnabled ? (
          <div className="flex flex-wrap items-end gap-3">
            <Field className="w-40" data-invalid={invalid === 'earlyDiscountBp' || undefined}>
              <FieldLabel htmlFor="policy-early-bp">
                {t('contractsM4:settings.earlyDiscount')}
              </FieldLabel>
              <PercentInput
                id="policy-early-bp"
                locale={i18n.language}
                value={p.earlyDiscountBp}
                onChange={(v) => pf('earlyDiscountBp', v)}
              />
            </Field>
            {intField('earlyPayByDays', t('contractsM4:settings.earlyPayBy'))}
          </div>
        ) : null}
      </Section>
      <Section title={t('contractsM4:settings.renewalCancellation')}>
        <Field orientation="horizontal">
          <Switch id="policy-tacit" checked={p.tacit} onCheckedChange={(v) => pf('tacit', v)} />
          <FieldLabel htmlFor="policy-tacit">{t('contractsM4:settings.tacit')}</FieldLabel>
        </Field>
        <div className="flex flex-wrap items-end gap-3">
          {intField('noticeDays', t('contractsM4:settings.noticeDays'))}
          {intField('cancelNoticeDays', t('contractsM4:settings.cancelNoticeDays'))}
          <EnumSelect
            id="policy-refund"
            label={t('contractsM4:settings.refund')}
            values={['pro_rata', 'none'] as const}
            labelOf={(v) => t(`contractsM4:settings.refunds.${v}`)}
            value={p.refund}
            onChange={(v) => pf('refund', v || 'pro_rata')}
          />
          <Field className="w-40" data-invalid={invalid === 'penaltyBp' || undefined}>
            <FieldLabel htmlFor="policy-penalty">{t('contractsM4:settings.penalty')}</FieldLabel>
            <PercentInput
              id="policy-penalty"
              locale={i18n.language}
              value={p.penaltyBp}
              onChange={(v) => pf('penaltyBp', v)}
            />
          </Field>
        </div>
      </Section>
      <Section
        title={t('contractsM4:settings.signature')}
        description={t('contractsM4:settings.signatureHint')}
      >
        <div className="flex flex-wrap items-end gap-3">
          <EnumSelect
            id="settings-driver"
            label={t('contractsM4:settings.driver')}
            group="signatureDriver"
            values={DRIVERS}
            value={s.signature_driver}
            onChange={(v) => setS({ ...s, signature_driver: v || 'paper' })}
          />
          <Field className="w-40">
            <FieldLabel htmlFor="settings-sig-days">
              {t('contractsM4:settings.signatureDays')}
            </FieldLabel>
            <Input
              id="settings-sig-days"
              inputMode="numeric"
              value={String(s.signature_days)}
              onChange={num('signature_days')}
            />
          </Field>
          <Field className="w-40">
            <FieldLabel htmlFor="settings-sig-rem">
              {t('contractsM4:settings.reminderDays')}
            </FieldLabel>
            <Input
              id="settings-sig-rem"
              inputMode="numeric"
              value={String(s.signature_reminder_days)}
              onChange={num('signature_reminder_days')}
            />
          </Field>
        </div>
        <Field orientation="horizontal">
          <Switch
            id="settings-countersign"
            checked={s.marina_countersign}
            onCheckedChange={(v) => setS({ ...s, marina_countersign: v })}
          />
          <FieldLabel htmlFor="settings-countersign">
            {t('contractsM4:settings.countersign')}
          </FieldLabel>
        </Field>
        <Field orientation="horizontal">
          <Switch
            id="settings-scan"
            checked={s.require_paper_scan}
            onCheckedChange={(v) => setS({ ...s, require_paper_scan: v })}
          />
          <FieldLabel htmlFor="settings-scan">{t('contractsM4:settings.requireScan')}</FieldLabel>
        </Field>
      </Section>
      <Section title={t('contractsM4:settings.renewalsArrears')}>
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-48">
            <FieldLabel htmlFor="settings-renewal-days">
              {t('contractsM4:settings.renewalDeadlineDays')}
            </FieldLabel>
            <Input
              id="settings-renewal-days"
              inputMode="numeric"
              value={String(s.renewal_deadline_days)}
              onChange={num('renewal_deadline_days')}
            />
          </Field>
          <Field className="w-48">
            <FieldLabel htmlFor="settings-arrears">
              {t('contractsM4:settings.arrearsDays')}
            </FieldLabel>
            <Input
              id="settings-arrears"
              inputMode="numeric"
              value={String(s.arrears_days)}
              onChange={num('arrears_days')}
            />
          </Field>
          <EnumSelect
            id="settings-insurance"
            label={t('contractsM4:settings.insurance')}
            group="insurancePolicy"
            values={INSURANCE}
            value={s.insurance_policy}
            onChange={(v) => setS({ ...s, insurance_policy: v || 'warn' })}
          />
        </div>
        <FieldSet>
          <FieldLegend variant="label">{t('contractsM4:settings.requiredFields')}</FieldLegend>
          <div className="flex flex-wrap gap-4">
            {REQUIRED.map((f) => (
              <Field key={f} orientation="horizontal" className="w-auto">
                <Checkbox
                  id={`settings-req-${f}`}
                  checked={s.required_fields.includes(f)}
                  onCheckedChange={(c) =>
                    setS({
                      ...s,
                      required_fields:
                        c === true
                          ? [...s.required_fields, f]
                          : s.required_fields.filter((x) => x !== f),
                    })
                  }
                />
                <FieldLabel htmlFor={`settings-req-${f}`}>
                  {t(`docsEnums:requiredField.${f}`)}
                </FieldLabel>
              </Field>
            ))}
          </div>
        </FieldSet>
      </Section>
      <Section
        title={t('contractsM4:settings.bank')}
        description={t('contractsM4:settings.bankHint')}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          {(['iban', 'holder', 'bank', 'bic'] as const).map((k) => (
            <Field key={k}>
              <FieldLabel htmlFor={`settings-bank-${k}`}>
                {t(`docsEnums:fields.bank.${k}`)}
              </FieldLabel>
              <Input
                id={`settings-bank-${k}`}
                value={s.bank_details?.[k] ?? ''}
                maxLength={140}
                onChange={(e) =>
                  setS({ ...s, bank_details: { ...s.bank_details, [k]: e.target.value } })
                }
              />
            </Field>
          ))}
          <Field>
            <FieldLabel htmlFor="settings-office-email">
              {t('contractsM4:settings.officeEmail')}
            </FieldLabel>
            <Input
              id="settings-office-email"
              type="email"
              value={s.office_email ?? ''}
              onChange={(e) => setS({ ...s, office_email: e.target.value })}
            />
          </Field>
        </div>
      </Section>
    </>
  );
}
