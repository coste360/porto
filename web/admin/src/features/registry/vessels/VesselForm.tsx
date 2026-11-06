import { zodResolver } from '@hookform/resolvers/zod';
import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useEffect, type ReactNode } from 'react';
import { useFieldArray, useForm, type Control } from 'react-hook-form';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldGroup, FieldLegend, FieldSet } from '@porto/shared/ui/field';

import { LengthField, SelectField, TextAreaField, TextField } from '../components/fields';
import { FUELS, HULL_TYPES, POWER_RATINGS } from '../lib/enums';
import { applyFieldErrors } from '../lib/fieldErrors';
import { emptyVesselValues, vesselSchema, type VesselFormValues } from './vesselSchema';

export type VesselFormProps = {
  defaultValues?: VesselFormValues;
  /** Edit mode shows the dimensions source choice (declared or measured). */
  edit?: boolean;
  hasMeasurement?: boolean;
  submitLabel: string;
  pending: boolean;
  disabled?: boolean;
  error?: unknown;
  onSubmit: (values: VesselFormValues) => void;
  onCancel?: () => void;
  /** Extra content above the buttons (create: the parties editor). */
  children?: ReactNode;
};

export function VesselForm({
  defaultValues,
  edit = false,
  hasMeasurement = false,
  submitLabel,
  pending,
  disabled = false,
  error,
  onSubmit,
  onCancel,
  children,
}: VesselFormProps) {
  const { t } = useT();
  const form = useForm<VesselFormValues>({
    resolver: zodResolver(vesselSchema),
    defaultValues: defaultValues ?? emptyVesselValues(),
  });
  useEffect(() => {
    if (error) applyFieldErrors(error, form.setError, Object.keys(emptyVesselValues()));
  }, [error, form.setError]);
  const c = form.control;

  return (
    <form noValidate className="flex flex-col gap-6" onSubmit={form.handleSubmit(onSubmit)}>
      <fieldset disabled={disabled || pending} className="flex flex-col gap-6">
        <FieldSet>
          <FieldLegend>{t('vessels:form.sectionIdentity')}</FieldLegend>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <TextField control={c} name="name" label={t('vessels:fields.name')} />
            <SelectField
              control={c}
              name="hull_type"
              label={t('vessels:fields.hullType')}
              options={HULL_TYPES.map((v) => ({ value: v, label: t(`vessels:hullTypes.${v}`) }))}
            />
            <TextField control={c} name="registration" label={t('vessels:fields.registration')} />
            <TextField
              control={c}
              name="registry_office"
              label={t('vessels:fields.registryOffice')}
            />
            <TextField control={c} name="flag" label={t('vessels:fields.flag')} />
            <TextField control={c} name="home_port" label={t('vessels:fields.homePort')} />
            <TextField control={c} name="model" label={t('vessels:fields.model')} />
            <TextField control={c} name="builder" label={t('vessels:fields.builder')} />
            <TextField control={c} name="year_built" label={t('vessels:fields.yearBuilt')} />
            <TextField control={c} name="hull_color" label={t('vessels:fields.hullColor')} />
            <TextField control={c} name="hull_material" label={t('vessels:fields.hullMaterial')} />
          </FieldGroup>
        </FieldSet>

        <FieldSet>
          <FieldLegend>{t('vessels:form.sectionDimensions')}</FieldLegend>
          <p className="text-muted-foreground text-sm">{t('vessels:form.declaredHint')}</p>
          <FieldGroup className="grid gap-4 sm:grid-cols-3">
            <LengthField control={c} name="loa_cm" label={t('vessels:fields.loa')} />
            <LengthField control={c} name="beam_cm" label={t('vessels:fields.beam')} />
            <LengthField control={c} name="draft_cm" label={t('vessels:fields.draft')} />
            <LengthField control={c} name="lwl_cm" label={t('vessels:fields.lwl')} />
            <LengthField control={c} name="air_draft_cm" label={t('vessels:fields.airDraft')} />
            <TextField
              control={c}
              name="displacement_kg"
              label={t('vessels:fields.displacementKg')}
            />
            {edit ? (
              <SelectField
                control={c}
                name="dimensions_source"
                label={t('vessels:fields.dimensionsSource')}
                options={[
                  { value: 'declared', label: t('vessels:dimensionsSource.declared') },
                  ...(hasMeasurement
                    ? [{ value: 'measured', label: t('vessels:dimensionsSource.measured') }]
                    : []),
                ]}
              />
            ) : null}
          </FieldGroup>
        </FieldSet>

        <EnginesEditor control={c} />

        <FieldSet>
          <FieldLegend>{t('vessels:form.sectionOther')}</FieldLegend>
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <SelectField
              control={c}
              name="fuel"
              label={t('vessels:fields.fuel')}
              noneLabel={t('vessels:fields.noFuel')}
              options={FUELS.map((v) => ({ value: v, label: t(`vessels:fuels.${v}`) }))}
            />
            <SelectField
              control={c}
              name="power_required_a"
              label={t('vessels:fields.powerRequired')}
              noneLabel={t('vessels:fields.noPower')}
              options={POWER_RATINGS.map((a) => ({
                value: String(a),
                label: t('vessels:fields.amperes', { value: a }),
              }))}
            />
          </FieldGroup>
          <TextAreaField control={c} name="notes" label={t('vessels:fields.notes')} />
        </FieldSet>
      </fieldset>
      {children}
      <div className="flex justify-end gap-2">
        {onCancel ? (
          <Button type="button" variant="outline" onClick={onCancel}>
            {t('common:actions.cancel')}
          </Button>
        ) : null}
        <Button type="submit" disabled={disabled || pending}>
          {pending ? t('common:actions.saving') : submitLabel}
        </Button>
      </div>
    </form>
  );
}

function EnginesEditor({ control }: { control: Control<VesselFormValues> }) {
  const { t } = useT();
  const { fields, append, remove } = useFieldArray({ control, name: 'engines' });
  return (
    <FieldSet>
      <FieldLegend>{t('vessels:engines.title')}</FieldLegend>
      {fields.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('vessels:engines.empty')}</p>
      ) : null}
      {fields.map((f, i) => (
        <div key={f.id} className="flex flex-col gap-2 rounded-md border p-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">{t('vessels:engines.item', { n: i + 1 })}</span>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('vessels:engines.remove', { n: i + 1 })}
              onClick={() => remove(i)}
            >
              <Trash2Icon />
            </Button>
          </div>
          <FieldGroup className="grid gap-3 sm:grid-cols-3">
            <TextField
              control={control}
              name={`engines.${i}.make`}
              label={t('vessels:engines.make')}
            />
            <TextField
              control={control}
              name={`engines.${i}.model`}
              label={t('vessels:engines.model')}
            />
            <TextField
              control={control}
              name={`engines.${i}.serial`}
              label={t('vessels:engines.serial')}
            />
            <TextField
              control={control}
              name={`engines.${i}.power_kw`}
              label={t('vessels:engines.powerKw')}
            />
            <TextField
              control={control}
              name={`engines.${i}.power_hp`}
              label={t('vessels:engines.powerHp')}
            />
            <TextField
              control={control}
              name={`engines.${i}.hours`}
              label={t('vessels:engines.hours')}
            />
          </FieldGroup>
        </div>
      ))}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={fields.length >= 8}
          onClick={() =>
            append({ make: '', model: '', power_kw: '', power_hp: '', serial: '', hours: '' })
          }
        >
          <PlusIcon />
          {t('vessels:engines.add')}
        </Button>
      </div>
    </FieldSet>
  );
}
