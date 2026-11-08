import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, TrashIcon } from 'lucide-react';
import { Controller, useFieldArray, useForm, type Control } from 'react-hook-form';
import { toast } from 'sonner';

import {
  useBerthGroups,
  useUpdateOnlineBookingSettings,
  type OnlineBookingSettings,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldSet,
  FieldLegend,
} from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { LengthInput } from '@porto/shared/ui/patterns/LengthInput';
import { PercentInput } from '@porto/shared/ui/patterns/PercentInput';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import { Switch } from '@porto/shared/ui/switch';
import { TabsContent } from '@porto/shared/ui/tabs';
import { Textarea } from '@porto/shared/ui/textarea';

import { BOOKING_DOCUMENT_TYPES } from '../lib/constants';
import { settingsSchema, toInput, toValues, type SettingsValues } from '../lib/settings';

const req = 'common:validation.required';

function NumberField({
  control,
  name,
  label,
  id,
}: {
  control: Control<SettingsValues>;
  name:
    | 'rules.min_nights'
    | 'rules.max_nights'
    | 'rules.min_advance_hours'
    | 'rules.max_advance_days'
    | 'max_live_holds'
    | 'approval.hold_hours';
  label: string;
  id: string;
}) {
  const { t } = useT();
  return (
    <Controller
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <Input
            id={id}
            type="number"
            inputMode="numeric"
            value={Number.isFinite(field.value) ? field.value : ''}
            onChange={(e) =>
              field.onChange(e.target.value === '' ? Number.NaN : Number(e.target.value))
            }
            onBlur={field.onBlur}
            aria-invalid={fieldState.invalid}
          />
          {fieldState.error?.message ? (
            <FieldError>{t(fieldState.error.message)}</FieldError>
          ) : null}
        </Field>
      )}
    />
  );
}

function SwitchField({
  control,
  name,
  label,
  id,
}: {
  control: Control<SettingsValues>;
  name:
    | 'required_fields.people_on_board'
    | 'required_fields.phone'
    | 'required_fields.documents_later'
    | 'late_payment_recovery'
    | 'allow_without_provider'
    | 'approval.always'
    | 'hide_header_in_embed'
    | 'show_feet';
  label: string;
  id: string;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Field orientation="horizontal">
          <Switch id={id} checked={field.value} onCheckedChange={field.onChange} />
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
        </Field>
      )}
    />
  );
}

/**
 * Rules, payment and cancellation, approval and arrival tabs of the settings page: one form saved with If-Match
 * (version 0 saves the defaults the first time). Rendered inside the page's Tabs.
 */
export function SettingsForm({
  marinaId,
  settings,
}: {
  marinaId: string;
  settings: OnlineBookingSettings;
}) {
  const { t, i18n } = useT();
  const save = useUpdateOnlineBookingSettings(marinaId);
  const groups = useBerthGroups(marinaId);
  const form = useForm<SettingsValues>({
    resolver: zodResolver(settingsSchema),
    values: toValues(settings),
  });
  const tiers = useFieldArray({ control: form.control, name: 'cancellation_tiers' });
  const c = form.control;
  const submit = form.handleSubmit((v) =>
    save.mutate(
      { version: settings.version, body: toInput(v) },
      { onSuccess: () => toast.success(t('online:settings.saved')) },
    ),
  );
  const footer = (
    <div className="flex flex-col gap-3">
      <ProblemAlert error={save.error} />
      <div>
        <Button type="submit" disabled={save.isPending} data-testid="online-settings-save">
          {t('online:settings.save')}
        </Button>
      </div>
    </div>
  );
  const tiersError = form.formState.errors.cancellation_tiers;
  return (
    <form onSubmit={submit} noValidate>
      <TabsContent value="rules">
        <Card>
          <CardContent className="flex flex-col gap-6 pt-6">
            <FieldGroup className="grid gap-4 sm:grid-cols-2">
              <NumberField
                control={c}
                name="rules.min_nights"
                id="r-min"
                label={t('online:settings.rules.minNights')}
              />
              <NumberField
                control={c}
                name="rules.max_nights"
                id="r-max"
                label={t('online:settings.rules.maxNights')}
              />
              <NumberField
                control={c}
                name="rules.min_advance_hours"
                id="r-adv-h"
                label={t('online:settings.rules.minAdvanceHours')}
              />
              <NumberField
                control={c}
                name="rules.max_advance_days"
                id="r-adv-d"
                label={t('online:settings.rules.maxAdvanceDays')}
              />
              <Field>
                <FieldLabel htmlFor="r-from">{t('online:settings.rules.arrivalFrom')}</FieldLabel>
                <Input id="r-from" type="time" {...form.register('rules.arrival_from')} />
              </Field>
              <Field>
                <FieldLabel htmlFor="r-to">{t('online:settings.rules.arrivalTo')}</FieldLabel>
                <Input id="r-to" type="time" {...form.register('rules.arrival_to')} />
              </Field>
              <NumberField
                control={c}
                name="max_live_holds"
                id="r-holds"
                label={t('online:settings.rules.maxLiveHolds')}
              />
            </FieldGroup>
            <Controller
              control={c}
              name="rules.closed_weekdays"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">
                    {t('online:settings.rules.closedWeekdays')}
                  </FieldLegend>
                  <div className="flex flex-wrap gap-4">
                    {[1, 2, 3, 4, 5, 6, 0].map((d) => (
                      <Field key={d} orientation="horizontal" className="w-auto">
                        <Checkbox
                          id={`wd-${d}`}
                          checked={field.value.includes(d)}
                          onCheckedChange={(on) =>
                            field.onChange(
                              on ? [...field.value, d] : field.value.filter((x) => x !== d),
                            )
                          }
                        />
                        <FieldLabel htmlFor={`wd-${d}`}>
                          {t(`online:settings.weekdays.${d}`)}
                        </FieldLabel>
                      </Field>
                    ))}
                  </div>
                </FieldSet>
              )}
            />
            <Controller
              control={c}
              name="reveal_berth"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">
                    {t('online:settings.rules.revealBerth')}
                  </FieldLegend>
                  <RadioGroup value={field.value} onValueChange={field.onChange}>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="after_confirmation" id="rb-after" />
                      <FieldLabel htmlFor="rb-after">
                        {t('online:settings.rules.revealAfter')}
                      </FieldLabel>
                    </Field>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="never" id="rb-never" />
                      <FieldLabel htmlFor="rb-never">
                        {t('online:settings.rules.revealNever')}
                      </FieldLabel>
                    </Field>
                  </RadioGroup>
                </FieldSet>
              )}
            />
            <FieldGroup>
              <SwitchField
                control={c}
                name="required_fields.people_on_board"
                id="rf-people"
                label={t('online:settings.rules.people')}
              />
              <SwitchField
                control={c}
                name="required_fields.phone"
                id="rf-phone"
                label={t('online:settings.rules.phone')}
              />
              <SwitchField
                control={c}
                name="show_feet"
                id="rf-feet"
                label={t('online:settings.rules.showFeet')}
              />
            </FieldGroup>
            <Controller
              control={c}
              name="required_fields.documents"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">{t('online:settings.rules.documents')}</FieldLegend>
                  {BOOKING_DOCUMENT_TYPES.map((d) => (
                    <Field key={d} orientation="horizontal">
                      <Checkbox
                        id={`doc-${d}`}
                        checked={field.value.includes(d)}
                        onCheckedChange={(on) =>
                          field.onChange(
                            on ? [...field.value, d] : field.value.filter((x) => x !== d),
                          )
                        }
                      />
                      <FieldLabel htmlFor={`doc-${d}`}>{t(`online:documentTypes.${d}`)}</FieldLabel>
                    </Field>
                  ))}
                </FieldSet>
              )}
            />
            <SwitchField
              control={c}
              name="required_fields.documents_later"
              id="rf-later"
              label={t('online:settings.rules.documentsLater')}
            />
            {footer}
          </CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="payment">
        <Card>
          <CardContent className="flex flex-col gap-6 pt-6">
            <Controller
              control={c}
              name="payment_mode"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">{t('online:settings.payment.mode')}</FieldLegend>
                  <RadioGroup value={field.value} onValueChange={field.onChange}>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="full" id="pm-full" />
                      <FieldLabel htmlFor="pm-full">{t('online:settings.payment.full')}</FieldLabel>
                    </Field>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="deposit" id="pm-deposit" />
                      <FieldLabel htmlFor="pm-deposit">
                        {t('online:settings.payment.deposit')}
                      </FieldLabel>
                    </Field>
                  </RadioGroup>
                </FieldSet>
              )}
            />
            <Controller
              control={c}
              name="deposit_bp"
              render={({ field, fieldState }) => (
                <Field data-invalid={fieldState.invalid} className="sm:max-w-48">
                  <FieldLabel htmlFor="pm-bp">{t('online:settings.payment.depositBp')}</FieldLabel>
                  <PercentInput
                    id="pm-bp"
                    value={field.value}
                    onChange={(bp) => field.onChange(bp ?? Number.NaN)}
                    locale={i18n.language}
                  />
                  {fieldState.error?.message ? (
                    <FieldError>{t(fieldState.error.message)}</FieldError>
                  ) : null}
                </Field>
              )}
            />
            <FieldSet>
              <FieldLegend variant="label">{t('online:settings.payment.tiers')}</FieldLegend>
              <div className="flex flex-col gap-3" data-testid="online-tiers">
                {tiers.fields.map((f, i) => (
                  <div key={f.id} className="flex flex-col gap-2 sm:flex-row sm:items-end">
                    <Controller
                      control={c}
                      name={`cancellation_tiers.${i}.min_days_before`}
                      render={({ field, fieldState }) => (
                        <Field data-invalid={fieldState.invalid}>
                          <FieldLabel htmlFor={`tier-d-${i}`}>
                            {t('online:settings.payment.tierDays')}
                          </FieldLabel>
                          <Input
                            id={`tier-d-${i}`}
                            type="number"
                            inputMode="numeric"
                            value={Number.isFinite(field.value) ? field.value : ''}
                            onChange={(e) =>
                              field.onChange(
                                e.target.value === '' ? Number.NaN : Number(e.target.value),
                              )
                            }
                          />
                        </Field>
                      )}
                    />
                    <Controller
                      control={c}
                      name={`cancellation_tiers.${i}.refund_bp`}
                      render={({ field, fieldState }) => (
                        <Field data-invalid={fieldState.invalid}>
                          <FieldLabel htmlFor={`tier-r-${i}`}>
                            {t('online:settings.payment.tierRefund')}
                          </FieldLabel>
                          <PercentInput
                            id={`tier-r-${i}`}
                            value={field.value}
                            onChange={(bp) => field.onChange(bp ?? Number.NaN)}
                            locale={i18n.language}
                          />
                        </Field>
                      )}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => tiers.remove(i)}
                      disabled={tiers.fields.length <= 1}
                      aria-label={t('online:settings.payment.removeTier')}
                    >
                      <TrashIcon />
                    </Button>
                  </div>
                ))}
                {tiersError ? (
                  <FieldError>
                    {t(tiersError.message ?? tiersError.root?.message ?? req)}
                  </FieldError>
                ) : null}
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => tiers.append({ min_days_before: 0, refund_bp: 0 })}
                    disabled={tiers.fields.length >= 10}
                    data-testid="online-add-tier"
                  >
                    <PlusIcon />
                    {t('online:settings.payment.addTier')}
                  </Button>
                </div>
              </div>
            </FieldSet>
            <Controller
              control={c}
              name="refund_as"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">{t('online:settings.payment.refundAs')}</FieldLegend>
                  <RadioGroup value={field.value} onValueChange={field.onChange}>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="refund" id="ra-refund" />
                      <FieldLabel htmlFor="ra-refund">
                        {t('online:settings.payment.refund')}
                      </FieldLabel>
                    </Field>
                    <Field orientation="horizontal">
                      <RadioGroupItem value="credit" id="ra-credit" />
                      <FieldLabel htmlFor="ra-credit">
                        {t('online:settings.payment.credit')}
                      </FieldLabel>
                    </Field>
                  </RadioGroup>
                </FieldSet>
              )}
            />
            <FieldGroup>
              <SwitchField
                control={c}
                name="late_payment_recovery"
                id="pm-late"
                label={t('online:settings.payment.latePayment')}
              />
              <SwitchField
                control={c}
                name="allow_without_provider"
                id="pm-noprov"
                label={t('online:settings.payment.withoutProvider')}
              />
            </FieldGroup>
            {footer}
          </CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="approval">
        <Card>
          <CardContent className="flex flex-col gap-6 pt-6">
            <SwitchField
              control={c}
              name="approval.always"
              id="ap-always"
              label={t('online:settings.approval.always')}
            />
            <Controller
              control={c}
              name="approval.loa_over_cm"
              render={({ field }) => (
                <Field className="sm:max-w-56">
                  <FieldLabel htmlFor="ap-loa">{t('online:settings.approval.loaOver')}</FieldLabel>
                  <LengthInput
                    id="ap-loa"
                    value={field.value}
                    onChange={field.onChange}
                    locale={i18n.language}
                  />
                </Field>
              )}
            />
            <NumberField
              control={c}
              name="approval.hold_hours"
              id="ap-hours"
              label={t('online:settings.approval.holdHours')}
            />
            <Controller
              control={c}
              name="approval.group_ids"
              render={({ field }) => (
                <FieldSet>
                  <FieldLegend variant="label">{t('online:settings.approval.groups')}</FieldLegend>
                  {(groups.data ?? []).map((g) => (
                    <Field key={g.id} orientation="horizontal">
                      <Checkbox
                        id={`ap-g-${g.id}`}
                        checked={field.value.includes(g.id)}
                        onCheckedChange={(on) =>
                          field.onChange(
                            on ? [...field.value, g.id] : field.value.filter((x) => x !== g.id),
                          )
                        }
                      />
                      <FieldLabel htmlFor={`ap-g-${g.id}`}>
                        {g.code} ·{' '}
                        {i18n.language.startsWith('en') && g.name.en ? g.name.en : g.name.it}
                      </FieldLabel>
                    </Field>
                  ))}
                </FieldSet>
              )}
            />
            {footer}
          </CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="arrival">
        <Card>
          <CardContent className="flex flex-col gap-6 pt-6">
            <FieldGroup className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="ar-vhf">{t('online:settings.arrival.vhf')}</FieldLabel>
                <Input id="ar-vhf" {...form.register('arrival_info.vhf_channel')} />
              </Field>
              <Field>
                <FieldLabel htmlFor="ar-phone">{t('online:settings.arrival.phone')}</FieldLabel>
                <Input id="ar-phone" type="tel" {...form.register('arrival_info.phone')} />
              </Field>
              <Field
                className="sm:col-span-2"
                data-invalid={Boolean(form.formState.errors.arrival_info?.rules_url)}
              >
                <FieldLabel htmlFor="ar-url">{t('online:settings.arrival.rulesUrl')}</FieldLabel>
                <Input id="ar-url" type="url" {...form.register('arrival_info.rules_url')} />
              </Field>
              <Field className="sm:col-span-2">
                <FieldLabel htmlFor="ar-it">{t('online:settings.arrival.textIt')}</FieldLabel>
                <Textarea id="ar-it" rows={4} {...form.register('arrival_info.text_it')} />
              </Field>
              <Field className="sm:col-span-2">
                <FieldLabel htmlFor="ar-en">{t('online:settings.arrival.textEn')}</FieldLabel>
                <Textarea id="ar-en" rows={4} {...form.register('arrival_info.text_en')} />
              </Field>
              <Field>
                <FieldLabel htmlFor="ar-theme">{t('online:settings.arrival.theme')}</FieldLabel>
                <Input id="ar-theme" {...form.register('theme_preset')} />
              </Field>
            </FieldGroup>
            <SwitchField
              control={c}
              name="hide_header_in_embed"
              id="ar-hide"
              label={t('online:settings.arrival.hideHeader')}
            />
            {footer}
          </CardContent>
        </Card>
      </TabsContent>
    </form>
  );
}
