import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, TrashIcon } from 'lucide-react';
import { Controller, useFieldArray, useForm, useWatch, type Control } from 'react-hook-form';
import { toast } from 'sonner';

import {
  useBerthCategories,
  useMaps,
  useSaveMarinaProfile,
  type MarinaProfile,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { LengthInput } from '@porto/shared/ui/patterns/LengthInput';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { ScrollArea, ScrollBar } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Switch } from '@porto/shared/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { localName } from '../../maps/lib';
import {
  POI_KINDS,
  POWER_AMPS,
  profileSchema,
  SEABEDS,
  SERVICE_FLAGS,
  toInput,
  toValues,
  WINDS,
  type ProfileValues,
} from '../lib/profileForm';
import { HoursEditor } from './HoursEditor';
import { I18nField } from './I18nField';
import { PhotosManager } from './PhotosManager';

const TABS = ['general', 'contacts', 'hours', 'harbour', 'services', 'photos', 'sections'] as const;

type TextKey =
  | 'street'
  | 'postal_code'
  | 'city'
  | 'province'
  | 'country'
  | 'phone'
  | 'email'
  | 'website'
  | 'lat'
  | 'lon';

function TextInput({
  control,
  name,
  label,
  hint,
  type = 'text',
}: {
  control: Control<ProfileValues>;
  name: TextKey;
  label: string;
  hint?: string;
  type?: string;
}) {
  const { t } = useT();
  const id = `profile-${name}`;
  return (
    <Controller
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <Input id={id} type={type} aria-invalid={fieldState.invalid} {...field} />
          {hint ? <FieldDescription>{hint}</FieldDescription> : null}
          {fieldState.error?.message ? (
            <FieldError>{t(fieldState.error.message)}</FieldError>
          ) : null}
        </Field>
      )}
    />
  );
}

type CmKey = 'max_loa_cm' | 'depth_entrance_cm' | 'depth_quay_cm' | 'depth_min_cm' | 'depth_max_cm';

function MetresInput({
  control,
  name,
  label,
}: {
  control: Control<ProfileValues>;
  name: CmKey;
  label: string;
}) {
  const { t, i18n } = useT();
  const id = `profile-${name}`;
  return (
    <Controller
      control={control}
      name={name}
      render={({ field, fieldState }) => (
        <Field data-invalid={fieldState.invalid}>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <LengthInput
            id={id}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            locale={i18n.language}
            unit={t('profile:unit.m')}
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

type IntKey =
  | 'berths_total_override'
  | 'transit_berths_override'
  | 'services.travel_lift_kg'
  | 'services.crane_kg';

function OptionalInt({
  control,
  name,
  label,
  hint,
}: {
  control: Control<ProfileValues>;
  name: IntKey;
  label: string;
  hint?: string;
}) {
  const id = `profile-${name.replace('.', '-')}`;
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Field>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <Input
            id={id}
            type="number"
            inputMode="numeric"
            min={0}
            value={field.value ?? ''}
            onChange={(e) => {
              const n = Number.parseInt(e.target.value, 10);
              field.onChange(e.target.value === '' || !Number.isFinite(n) ? null : n);
            }}
            onBlur={field.onBlur}
          />
          {hint ? <FieldDescription>{hint}</FieldDescription> : null}
        </Field>
      )}
    />
  );
}

/** Toggles a value in a list (checkbox groups). */
function toggle<T>(list: T[], value: T, on: boolean): T[] {
  return on ? (list.includes(value) ? list : [...list, value]) : list.filter((x) => x !== value);
}

/**
 * The profile draft form (m12-plan §12.1, §16.2): one form in tabs, saved with If-Match (version 0 the first
 * time). Lengths are typed in metres and kept in cm; the position is typed in decimal degrees and kept as e7.
 */
export function ProfileForm({ marinaId, profile }: { marinaId: string; profile: MarinaProfile }) {
  const { t, i18n } = useT();
  const save = useSaveMarinaProfile(marinaId);
  const maps = useMaps(marinaId);
  const categories = (useBerthCategories(marinaId).data?.items ?? []).filter(
    (c) => !c.archived && (c.applies_to ?? []).includes('berth'),
  );
  const form = useForm<ProfileValues>({
    resolver: zodResolver(profileSchema),
    values: toValues(profile),
  });
  const c = form.control;
  const vhf = useWatch({ control: c, name: 'vhf' });
  const poi = useFieldArray({ control: c, name: 'poi' });
  const showOccupancy = useWatch({ control: c, name: 'show_occupancy' });
  const submit = form.handleSubmit((v) =>
    save.mutate(
      { version: profile.version, body: toInput(v) },
      { onSuccess: () => toast.success(t('profile:saved')) },
    ),
  );
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4" data-testid="profile-form">
      <Tabs defaultValue="general">
        <ScrollArea className="w-full">
          <TabsList>
            {TABS.map((k) => (
              <TabsTrigger key={k} value={k}>
                {t(`profile:tabs.${k}`)}
              </TabsTrigger>
            ))}
          </TabsList>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>

        <TabsContent value="general">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <I18nField
                control={c}
                name="description"
                id="profile-description"
                label={t('profile:fields.description')}
                max={1000}
              />
              <Controller
                control={c}
                name="locales"
                render={({ field }) => (
                  <Field orientation="horizontal">
                    <Switch
                      id="profile-locale-en"
                      checked={field.value.includes('en')}
                      onCheckedChange={(on) => field.onChange(on ? ['it', 'en'] : ['it'])}
                    />
                    <FieldLabel htmlFor="profile-locale-en">
                      {t('profile:fields.publishEnglish')}
                    </FieldLabel>
                  </Field>
                )}
              />
              <FieldSet>
                <FieldLegend>{t('profile:fields.position')}</FieldLegend>
                <FieldDescription>{t('profile:fields.positionHint')}</FieldDescription>
                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <TextInput control={c} name="lat" label={t('profile:fields.lat')} />
                  <TextInput control={c} name="lon" label={t('profile:fields.lon')} />
                </FieldGroup>
              </FieldSet>
              <FieldSet>
                <FieldLegend>{t('profile:fields.address')}</FieldLegend>
                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <TextInput control={c} name="street" label={t('profile:fields.street')} />
                  <TextInput
                    control={c}
                    name="postal_code"
                    label={t('profile:fields.postalCode')}
                  />
                  <TextInput control={c} name="city" label={t('profile:fields.city')} />
                  <TextInput control={c} name="province" label={t('profile:fields.province')} />
                  <TextInput control={c} name="country" label={t('profile:fields.country')} />
                </FieldGroup>
              </FieldSet>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="contacts">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <TextInput control={c} name="phone" type="tel" label={t('profile:fields.phone')} />
                <TextInput
                  control={c}
                  name="email"
                  type="email"
                  label={t('profile:fields.email')}
                />
                <TextInput
                  control={c}
                  name="website"
                  type="url"
                  label={t('profile:fields.website')}
                  hint={t('profile:fields.websiteHint')}
                />
              </FieldGroup>
              <FieldSet>
                <FieldLegend>{t('profile:fields.vhf')}</FieldLegend>
                <div className="flex flex-wrap items-end gap-2">
                  {vhf.map((ch, i) => (
                    <Field key={`${ch}-${i}`} className="w-28">
                      <FieldLabel htmlFor={`profile-vhf-${i}`} className="sr-only">
                        {t('profile:fields.vhf')}
                      </FieldLabel>
                      <Input
                        id={`profile-vhf-${i}`}
                        type="number"
                        min={1}
                        max={88}
                        value={ch}
                        onChange={(e) => {
                          const next = [...vhf];
                          next[i] = Number.parseInt(e.target.value, 10) || 0;
                          form.setValue('vhf', next, { shouldDirty: true });
                        }}
                      />
                    </Field>
                  ))}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={vhf.length >= 4}
                    onClick={() => form.setValue('vhf', [...vhf, 16], { shouldDirty: true })}
                  >
                    <PlusIcon />
                    {t('profile:fields.addVhf')}
                  </Button>
                  {vhf.length > 0 ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t('profile:fields.removeVhf')}
                      onClick={() => form.setValue('vhf', vhf.slice(0, -1), { shouldDirty: true })}
                    >
                      <TrashIcon />
                    </Button>
                  ) : null}
                </div>
                {form.formState.errors.vhf ? (
                  <FieldError>{t('profile:fields.vhfInvalid')}</FieldError>
                ) : null}
              </FieldSet>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="hours">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <HoursEditor
                control={c}
                name="office_hours"
                legend={t('publicProfile:officeHours')}
              />
              <HoursEditor control={c} name="dock_hours" legend={t('publicProfile:dockHours')} />
              <I18nField
                control={c}
                name="hours_note"
                id="profile-hours-note"
                label={t('profile:fields.hoursNote')}
                max={300}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="harbour">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <OptionalInt
                  control={c}
                  name="berths_total_override"
                  label={t('profile:fields.berthsTotal')}
                  hint={t('profile:fields.computed', { n: profile.berths_total })}
                />
                <OptionalInt
                  control={c}
                  name="transit_berths_override"
                  label={t('profile:fields.transitBerths')}
                  hint={t('profile:fields.computed', { n: profile.transit_berths })}
                />
                <MetresInput control={c} name="max_loa_cm" label={t('publicProfile:maxLoa')} />
                <MetresInput
                  control={c}
                  name="depth_entrance_cm"
                  label={t('publicProfile:depthEntrance')}
                />
                <MetresInput
                  control={c}
                  name="depth_quay_cm"
                  label={t('publicProfile:depthQuay')}
                />
                <MetresInput control={c} name="depth_min_cm" label={t('profile:fields.depthMin')} />
                <MetresInput control={c} name="depth_max_cm" label={t('profile:fields.depthMax')} />
                <Controller
                  control={c}
                  name="seabed"
                  render={({ field }) => (
                    <Field>
                      <FieldLabel htmlFor="profile-seabed">
                        {t('publicProfile:seabedLabel')}
                      </FieldLabel>
                      <Select
                        value={field.value || 'none'}
                        onValueChange={(v) => field.onChange(v === 'none' ? '' : v)}
                      >
                        <SelectTrigger id="profile-seabed">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="none">{t('profile:fields.notSet')}</SelectItem>
                          {SEABEDS.map((s) => (
                            <SelectItem key={s} value={s}>
                              {t(`publicProfile:seabed.${s}`)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </Field>
                  )}
                />
              </FieldGroup>
              <Controller
                control={c}
                name="shelter"
                render={({ field }) => (
                  <FieldSet>
                    <FieldLegend>{t('publicProfile:shelter')}</FieldLegend>
                    <div className="flex flex-wrap gap-4">
                      {WINDS.map((w) => (
                        <Field key={w} orientation="horizontal" className="w-auto">
                          <Checkbox
                            id={`profile-wind-${w}`}
                            checked={field.value.includes(w)}
                            onCheckedChange={(on) =>
                              field.onChange(toggle(field.value, w, on === true))
                            }
                          />
                          <FieldLabel htmlFor={`profile-wind-${w}`}>
                            {t(`profile:wind.${w}`)}
                          </FieldLabel>
                        </Field>
                      ))}
                    </div>
                  </FieldSet>
                )}
              />
              <I18nField
                control={c}
                name="access_notes"
                id="profile-access"
                label={t('publicProfile:access')}
                max={2000}
              />
              <I18nField
                control={c}
                name="lights"
                id="profile-lights"
                label={t('publicProfile:lights')}
                max={1000}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="services">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <Controller
                control={c}
                name="services.flags"
                render={({ field }) => (
                  <FieldSet>
                    <FieldLegend>{t('publicProfile:services.title')}</FieldLegend>
                    <div className="grid gap-3 sm:grid-cols-3">
                      {SERVICE_FLAGS.map((f) => (
                        <Field key={f} orientation="horizontal">
                          <Checkbox
                            id={`profile-svc-${f}`}
                            checked={field.value.includes(f)}
                            onCheckedChange={(on) =>
                              field.onChange(toggle(field.value, f, on === true))
                            }
                          />
                          <FieldLabel htmlFor={`profile-svc-${f}`}>
                            {t(`publicProfile:services.${f}`)}
                          </FieldLabel>
                        </Field>
                      ))}
                    </div>
                  </FieldSet>
                )}
              />
              <Controller
                control={c}
                name="services.power_amps"
                render={({ field }) => (
                  <FieldSet>
                    <FieldLegend>{t('publicProfile:services.power')}</FieldLegend>
                    <div className="flex flex-wrap gap-4">
                      {POWER_AMPS.map((a) => (
                        <Field key={a} orientation="horizontal" className="w-auto">
                          <Checkbox
                            id={`profile-amps-${a}`}
                            checked={field.value.includes(a)}
                            onCheckedChange={(on) =>
                              field.onChange(toggle(field.value, a, on === true))
                            }
                          />
                          <FieldLabel htmlFor={`profile-amps-${a}`}>
                            {t('profile:fields.amps', { amps: a })}
                          </FieldLabel>
                        </Field>
                      ))}
                    </div>
                  </FieldSet>
                )}
              />
              <FieldSet>
                <FieldLegend>{t('publicProfile:services.fuel')}</FieldLegend>
                <div className="flex flex-wrap gap-4">
                  {(['petrol', 'diesel'] as const).map((k) => (
                    <Controller
                      key={k}
                      control={c}
                      name={`services.${k}`}
                      render={({ field }) => (
                        <Field orientation="horizontal" className="w-auto">
                          <Checkbox
                            id={`profile-fuel-${k}`}
                            checked={field.value}
                            onCheckedChange={(on) => field.onChange(on === true)}
                          />
                          <FieldLabel htmlFor={`profile-fuel-${k}`}>
                            {t(
                              k === 'petrol'
                                ? 'publicProfile:fuelPetrol'
                                : 'publicProfile:fuelDiesel',
                            )}
                          </FieldLabel>
                        </Field>
                      )}
                    />
                  ))}
                </div>
                <I18nField
                  control={c}
                  name="services.fuel_hours"
                  id="profile-fuel-hours"
                  label={t('profile:fields.fuelHours')}
                  max={300}
                  multiline={false}
                />
              </FieldSet>
              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <OptionalInt
                  control={c}
                  name="services.travel_lift_kg"
                  label={t('profile:fields.travelLiftKg')}
                />
                <OptionalInt
                  control={c}
                  name="services.crane_kg"
                  label={t('profile:fields.craneKg')}
                />
              </FieldGroup>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="photos">
          <Card>
            <CardContent className="flex flex-col gap-8 pt-6">
              <PhotosManager
                marinaId={marinaId}
                control={c}
                name="photos"
                legend={t('publicProfile:photos')}
                max={12}
              />
              <PhotosManager
                marinaId={marinaId}
                control={c}
                name="access_images"
                legend={t('profile:fields.accessImages')}
                max={4}
              />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="sections">
          <Card>
            <CardContent className="flex flex-col gap-6 pt-6">
              <Controller
                control={c}
                name="show_map"
                render={({ field }) => (
                  <Field orientation="horizontal">
                    <Switch
                      id="profile-show-map"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                    <FieldLabel htmlFor="profile-show-map">{t('profile:sections.map')}</FieldLabel>
                  </Field>
                )}
              />
              <Controller
                control={c}
                name="map_id"
                render={({ field }) => (
                  <Field>
                    <FieldLabel htmlFor="profile-map">{t('profile:sections.mapPick')}</FieldLabel>
                    <Select
                      value={field.value || 'first'}
                      onValueChange={(v) => field.onChange(v === 'first' ? '' : v)}
                    >
                      <SelectTrigger id="profile-map">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="first">{t('profile:sections.firstMap')}</SelectItem>
                        {(maps.data ?? []).map((m) => (
                          <SelectItem key={m.id} value={m.id}>
                            {localName(m.name, i18n.language)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                )}
              />
              <FieldSet>
                <FieldLegend>{t('profile:sections.poi')}</FieldLegend>
                <FieldDescription>{t('profile:sections.poiHint')}</FieldDescription>
                <ul className="flex flex-col gap-2">
                  {poi.fields.map((row, i) => (
                    <li key={row.id} className="flex flex-wrap items-end gap-2">
                      <Controller
                        control={c}
                        name={`poi.${i}.kind`}
                        render={({ field }) => (
                          <Select value={field.value} onValueChange={field.onChange}>
                            <SelectTrigger
                              className="w-44"
                              aria-label={t('profile:sections.poiKind')}
                            >
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {POI_KINDS.map((k) => (
                                <SelectItem key={k} value={k}>
                                  {t(`profile:poi.${k}`)}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        )}
                      />
                      {(['x_cm', 'y_cm'] as const).map((axis) => (
                        <Controller
                          key={axis}
                          control={c}
                          name={`poi.${i}.${axis}`}
                          render={({ field }) => (
                            <div className="w-36">
                              <LengthInput
                                value={field.value}
                                onChange={(v) => field.onChange(v ?? 0)}
                                locale={i18n.language}
                                unit={t('profile:unit.m')}
                                aria-label={t(
                                  axis === 'x_cm'
                                    ? 'profile:sections.poiX'
                                    : 'profile:sections.poiY',
                                )}
                              />
                            </div>
                          )}
                        />
                      ))}
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={t('profile:sections.poiRemove')}
                        onClick={() => poi.remove(i)}
                      >
                        <TrashIcon />
                      </Button>
                    </li>
                  ))}
                </ul>
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={poi.fields.length >= 16}
                    onClick={() => poi.append({ kind: 'entrance', x_cm: 0, y_cm: 0 })}
                  >
                    <PlusIcon />
                    {t('profile:sections.poiAdd')}
                  </Button>
                </div>
              </FieldSet>
              <Controller
                control={c}
                name="show_prices"
                render={({ field }) => (
                  <Field orientation="horizontal">
                    <Switch
                      id="profile-show-prices"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                    <FieldLabel htmlFor="profile-show-prices">
                      {t('profile:sections.prices')}
                    </FieldLabel>
                  </Field>
                )}
              />
              {!profile.prices_available ? (
                <Alert data-testid="profile-prices-hint">
                  <AlertDescription>{t('profile:sections.pricesHint')}</AlertDescription>
                </Alert>
              ) : null}
              <Controller
                control={c}
                name="show_occupancy"
                render={({ field }) => (
                  <Field orientation="horizontal">
                    <Switch
                      id="profile-show-occupancy"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                    <FieldLabel htmlFor="profile-show-occupancy">
                      {t('profile:sections.occupancy')}
                    </FieldLabel>
                  </Field>
                )}
              />
              {showOccupancy ? (
                <Controller
                  control={c}
                  name="occupancy_categories"
                  render={({ field, fieldState }) => (
                    <FieldSet data-invalid={fieldState.invalid}>
                      <FieldLegend>{t('profile:sections.categories')}</FieldLegend>
                      <FieldDescription>{t('profile:sections.categoriesHint')}</FieldDescription>
                      <div className="grid gap-3 sm:grid-cols-3">
                        {categories.map((cat) => (
                          <Field key={cat.code} orientation="horizontal">
                            <Checkbox
                              id={`profile-cat-${cat.code}`}
                              checked={field.value.includes(cat.code)}
                              onCheckedChange={(on) =>
                                field.onChange(toggle(field.value, cat.code, on === true))
                              }
                            />
                            <FieldLabel htmlFor={`profile-cat-${cat.code}`}>
                              {localName(cat.name, i18n.language) || cat.code}
                            </FieldLabel>
                          </Field>
                        ))}
                      </div>
                      {fieldState.error?.message ? (
                        <FieldError>{t(fieldState.error.message)}</FieldError>
                      ) : null}
                    </FieldSet>
                  )}
                />
              ) : null}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
      <ProblemAlert error={save.error} />
      {Object.keys(form.formState.errors).length > 0 ? (
        <Alert variant="destructive" data-testid="profile-form-invalid">
          <AlertDescription>{t('profile:invalid')}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <Button type="submit" disabled={save.isPending} data-testid="profile-save">
          {t('profile:save')}
        </Button>
      </div>
    </form>
  );
}
