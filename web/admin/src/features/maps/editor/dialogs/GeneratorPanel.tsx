import { CrosshairIcon, EyeIcon, PlusIcon, XIcon } from 'lucide-react';

import type { BerthGroup, GenerateResult } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import type { Pt } from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { LengthInput, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { formatDd, parseDegreesToDd } from '../model/calibration';
import type { EditorDocument } from '../model/document';
import {
  GENERATOR_FIELDS,
  type FieldSpec,
  type GeneratorType,
  type GeneratorValues,
  type Numbering,
} from './generators';

export type PickRequest = { field: string; mode: 'point' | 'polygon' };

export type GeneratorPanelProps = {
  type: GeneratorType;
  values: GeneratorValues;
  onChange: (values: GeneratorValues) => void;
  invalid: readonly string[];
  doc: EditorDocument;
  groups: readonly BerthGroup[];
  picking: PickRequest | null;
  onPick: (req: PickRequest | null) => void;
  preview: GenerateResult | null;
  pending: boolean;
  error: unknown;
  onPreview: () => void;
  onInsert: () => void;
  onDiscard: () => void;
};

/**
 * One form per generator (spec 8.3). "Anteprima" asks the server for the elements on the current document and
 * overlays them; "Inserisci" adds them as one undo step. Points and polygons are picked on the canvas, so this
 * is a side panel rather than a modal dialog.
 */
export function GeneratorPanel(p: GeneratorPanelProps) {
  const { t } = useT();
  const set = (name: string, v: unknown) => p.onChange({ ...p.values, [name]: v });
  const errors = p.preview?.report?.errors.length ?? 0;
  return (
    <section className="flex flex-col gap-3" aria-label={t(`editor:generators.${p.type}.title`)}>
      <h2 className="text-base font-semibold">{t(`editor:generators.${p.type}.title`)}</h2>
      <p className="text-muted-foreground text-sm">{t(`editor:generators.${p.type}.hint`)}</p>
      {GENERATOR_FIELDS[p.type].map((f) => (
        <GeneratorField
          key={f.name}
          spec={f}
          value={p.values[f.name]}
          invalid={p.invalid.includes(f.name)}
          onChange={(v) => set(f.name, v)}
          doc={p.doc}
          groups={p.groups}
          picking={p.picking?.field === f.name}
          onPick={() =>
            p.onPick(
              p.picking?.field === f.name
                ? null
                : { field: f.name, mode: f.kind === 'polygon' ? 'polygon' : 'point' },
            )
          }
        />
      ))}
      {p.error ? <ProblemAlert error={p.error} /> : null}
      {p.invalid.length > 0 ? (
        <p className="text-destructive text-sm">{t('editor:generators.invalid')}</p>
      ) : null}
      {p.preview ? (
        <div
          className="flex flex-col gap-2 rounded-md border p-3 text-sm"
          data-testid="generator-preview"
        >
          <p>
            {t('editor:generators.previewCounts', {
              berths: p.preview.counts.berths,
              structures: p.preview.counts.structures,
              others:
                p.preview.counts.dynamic_areas +
                p.preview.counts.land_resources +
                p.preview.counts.pedestals,
            })}
          </p>
          {p.preview.report ? (
            <p>
              <Badge variant={errors > 0 ? 'destructive' : 'secondary'}>
                {t('editor:validation.errors', { count: errors })}
              </Badge>{' '}
              <Badge variant="outline">
                {t('editor:validation.warnings', { count: p.preview.report.warnings.length })}
              </Badge>
            </p>
          ) : null}
        </div>
      ) : null}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={p.onPreview} disabled={p.pending}>
          <EyeIcon />
          {t('editor:generators.preview')}
        </Button>
        <Button onClick={p.onInsert} disabled={!p.preview || p.pending}>
          <PlusIcon />
          {t('editor:generators.insert')}
        </Button>
        {p.preview ? (
          <Button variant="ghost" onClick={p.onDiscard}>
            <XIcon />
            {t('editor:generators.discard')}
          </Button>
        ) : null}
      </div>
    </section>
  );
}

function GeneratorField({
  spec,
  value,
  invalid,
  onChange,
  doc,
  groups,
  picking,
  onPick,
}: {
  spec: FieldSpec;
  value: unknown;
  invalid: boolean;
  onChange: (v: unknown) => void;
  doc: EditorDocument;
  groups: readonly BerthGroup[];
  picking: boolean;
  onPick: () => void;
}) {
  const { t, i18n } = useT();
  const id = `gen-${spec.name}`;
  const label = t(`editor:genFields.${spec.name}`);
  const m = t('maps:units.m');
  const wrap = (control: React.ReactNode) => (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      {control}
    </div>
  );
  switch (spec.kind) {
    case 'cm':
      return wrap(
        <LengthInput
          id={id}
          value={(value as number | null) ?? null}
          onChange={onChange}
          unit={m}
          locale={i18n.language}
          aria-invalid={invalid}
        />,
      );
    case 'int':
      return wrap(
        <Input
          id={id}
          inputMode="numeric"
          value={value === null || value === undefined ? '' : String(value)}
          aria-invalid={invalid}
          onChange={(e) =>
            onChange(
              e.target.value === ''
                ? null
                : /^\d+$/.test(e.target.value)
                  ? Number(e.target.value)
                  : value,
            )
          }
        />,
      );
    case 'angle':
      return wrap(
        <Input
          key={String(value)}
          id={id}
          inputMode="decimal"
          defaultValue={typeof value === 'number' ? formatDd(value) : ''}
          aria-invalid={invalid}
          onBlur={(e) => onChange(parseDegreesToDd(e.target.value))}
        />,
      );
    case 'text':
      return wrap(
        <Input
          id={id}
          value={(value as string) ?? ''}
          maxLength={32}
          aria-invalid={invalid}
          onChange={(e) => onChange(e.target.value)}
        />,
      );
    case 'bool':
      return (
        <div className="flex items-center gap-2">
          <Checkbox id={id} checked={!!value} onCheckedChange={(c) => onChange(c === true)} />
          <Label htmlFor={id}>{label}</Label>
        </div>
      );
    case 'sides': {
      const sides = new Set((value as string[]) ?? []);
      return (
        <fieldset className="flex gap-4">
          <legend className="mb-1 text-sm font-medium">{label}</legend>
          {(['left', 'right'] as const).map((s) => (
            <div key={s} className="flex items-center gap-2">
              <Checkbox
                id={`${id}-${s}`}
                checked={sides.has(s)}
                onCheckedChange={(c) => {
                  const next = new Set(sides);
                  if (c === true) next.add(s);
                  else next.delete(s);
                  onChange((['left', 'right'] as const).filter((x) => next.has(x)));
                }}
              />
              <Label htmlFor={`${id}-${s}`}>{t(`editor:sides.${s}`)}</Label>
            </div>
          ))}
        </fieldset>
      );
    }
    case 'numbering': {
      const n = (value as Numbering) ?? { scheme: 'continuous', prefix: '', start: 1, pad: 0 };
      return (
        <fieldset className="grid grid-cols-2 gap-2">
          <legend className="mb-1 text-sm font-medium">{label}</legend>
          <div className="col-span-2 flex flex-col gap-1">
            <Label htmlFor={`${id}-scheme`}>{t('editor:renumber.scheme')}</Label>
            <Select value={n.scheme} onValueChange={(v) => onChange({ ...n, scheme: v })}>
              <SelectTrigger id={`${id}-scheme`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(['continuous', 'odd_left_even_right', 'per_side'] as const).map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(`editor:renumber.schemes.${s}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-prefix`}>{t('editor:renumber.prefix')}</Label>
            <Input
              id={`${id}-prefix`}
              value={n.prefix}
              maxLength={16}
              onChange={(e) => onChange({ ...n, prefix: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-start`}>{t('editor:renumber.start')}</Label>
            <Input
              id={`${id}-start`}
              inputMode="numeric"
              value={String(n.start)}
              onChange={(e) =>
                /^\d*$/.test(e.target.value) &&
                onChange({ ...n, start: Number(e.target.value || 0) })
              }
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-pad`}>{t('editor:renumber.pad')}</Label>
            <Select value={String(n.pad)} onValueChange={(v) => onChange({ ...n, pad: Number(v) })}>
              <SelectTrigger id={`${id}-pad`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {['0', '1', '2', '3'].map((v) => (
                  <SelectItem key={v} value={v}>
                    {v}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </fieldset>
      );
    }
    case 'select':
      return wrap(
        <Select value={String(value ?? '')} onValueChange={onChange}>
          <SelectTrigger id={id} className="w-full" aria-invalid={invalid}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {(spec.options ?? []).map((o) => (
              <SelectItem key={o} value={o}>
                {t(`editor:options.${o}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>,
      );
    case 'structure': {
      const options = [...doc.elements.entries()].filter(
        ([, e]) => e.kind === 'structure' && (spec.structureKinds ?? []).includes(e.el.kind),
      );
      return wrap(
        <Select
          value={(value as string) || 'none'}
          onValueChange={(v) => onChange(v === 'none' ? '' : v)}
        >
          <SelectTrigger id={id} className="w-full" aria-invalid={invalid}>
            <SelectValue placeholder={t('editor:fields.none')} />
          </SelectTrigger>
          <SelectContent>
            {!spec.required ? (
              <SelectItem value="none">{t('editor:fields.none')}</SelectItem>
            ) : null}
            {options.map(([ref, e]) => (
              <SelectItem key={ref} value={ref}>
                {e.kind === 'structure'
                  ? `${t(`editor:structureKinds.${e.el.kind}`)} ${e.el.code}`
                  : ref}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>,
      );
    }
    case 'group':
      return wrap(
        <Select
          value={(value as string) || 'none'}
          onValueChange={(v) => onChange(v === 'none' ? '' : v)}
        >
          <SelectTrigger id={id} className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">{t('editor:fields.none')}</SelectItem>
            {groups.map((g) => (
              <SelectItem key={g.id} value={g.id}>
                {g.code}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>,
      );
    case 'point': {
      const pt = value as Pt | null;
      return wrap(
        <div className="flex items-center gap-2">
          <span id={id} className="text-sm" data-testid={id} aria-invalid={invalid}>
            {pt
              ? `${formatCm(pt.x, i18n.language)}, ${formatCm(pt.y, i18n.language)}`
              : t('editor:generators.noPoint')}
          </span>
          <Button
            variant={picking ? 'secondary' : 'outline'}
            size="sm"
            onClick={onPick}
            aria-pressed={picking}
          >
            <CrosshairIcon />
            {picking ? t('editor:generators.picking') : t('editor:generators.pick')}
          </Button>
        </div>,
      );
    }
    case 'polygon': {
      const pts = (value as Pt[]) ?? [];
      return wrap(
        <div className="flex items-center gap-2">
          <span id={id} className="text-sm" aria-invalid={invalid}>
            {t('editor:generators.vertices', { count: pts.length })}
          </span>
          <Button
            variant={picking ? 'secondary' : 'outline'}
            size="sm"
            onClick={onPick}
            aria-pressed={picking}
          >
            <CrosshairIcon />
            {picking ? t('editor:generators.pickingPolygon') : t('editor:generators.pickPolygon')}
          </Button>
        </div>,
      );
    }
  }
}
