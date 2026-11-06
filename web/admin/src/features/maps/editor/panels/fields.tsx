import { useState, type ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { LengthInput } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { formatDd, parseDegreesToDd } from '../model/calibration';
import { MIXED, type Common } from './common';

function Row({ id, label, children }: { id: string; label: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  );
}

/** Length in metres, committed on blur or Enter (one command per edit). */
export function CmField({
  id,
  label,
  value,
  onCommit,
  min = 1,
}: {
  id: string;
  label: ReactNode;
  value: Common<number | undefined>;
  onCommit: (cm: number | undefined) => void;
  min?: number;
}) {
  const { t, i18n } = useT();
  const shown = value === MIXED ? null : (value ?? null);
  const [draft, setDraft] = useState<number | null | 'unset'>('unset');
  const current = draft === 'unset' ? shown : draft;
  const commit = () => {
    if (draft === 'unset') return;
    if (draft === null) onCommit(undefined);
    else if (draft >= min) onCommit(draft);
    setDraft('unset');
  };
  return (
    <Row id={id} label={label}>
      <LengthInput
        key={`${String(shown)}`}
        id={id}
        value={current}
        unit={t('maps:units.m')}
        locale={i18n.language}
        placeholder={value === MIXED ? t('editor:properties.mixed') : undefined}
        onChange={(cm) => setDraft(cm)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit();
        }}
      />
    </Row>
  );
}

/** Angle in degrees with one decimal (rotation_dd). */
export function AngleField({
  id,
  label,
  value,
  onCommit,
}: {
  id: string;
  label: ReactNode;
  value: Common<number>;
  onCommit: (dd: number) => void;
}) {
  const { t } = useT();
  const shown = value === MIXED || value === undefined ? '' : formatDd(value);
  return (
    <Row id={id} label={label}>
      <Input
        key={shown}
        id={id}
        defaultValue={shown}
        inputMode="decimal"
        placeholder={value === MIXED ? t('editor:properties.mixed') : undefined}
        onBlur={(e) => {
          const dd = parseDegreesToDd(e.target.value);
          if (dd !== null && e.target.value !== shown) onCommit(dd);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
        }}
      />
    </Row>
  );
}

export function TextField({
  id,
  label,
  value,
  onCommit,
  maxLength = 32,
  required = false,
}: {
  id: string;
  label: ReactNode;
  value: Common<string | undefined>;
  onCommit: (v: string) => void;
  maxLength?: number;
  required?: boolean;
}) {
  const { t } = useT();
  const shown = value === MIXED ? '' : (value ?? '');
  return (
    <Row id={id} label={label}>
      <Input
        key={shown}
        id={id}
        defaultValue={shown}
        maxLength={maxLength}
        placeholder={value === MIXED ? t('editor:properties.mixed') : undefined}
        onBlur={(e) => {
          const v = e.target.value.trim();
          if (v === shown || (required && v === '')) return;
          onCommit(v);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
        }}
      />
    </Row>
  );
}

export function IntField({
  id,
  label,
  value,
  onCommit,
  min = 0,
  max = Number.MAX_SAFE_INTEGER,
}: {
  id: string;
  label: ReactNode;
  value: Common<number | undefined>;
  onCommit: (v: number) => void;
  min?: number;
  max?: number;
}) {
  const { t } = useT();
  const shown = value === MIXED || value === undefined ? '' : String(value);
  return (
    <Row id={id} label={label}>
      <Input
        key={shown}
        id={id}
        defaultValue={shown}
        inputMode="numeric"
        placeholder={value === MIXED ? t('editor:properties.mixed') : undefined}
        onBlur={(e) => {
          const n = Number(e.target.value);
          if (e.target.value === shown || !Number.isInteger(n) || n < min || n > max) return;
          onCommit(n);
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
        }}
      />
    </Row>
  );
}

export function SelectField<T extends string>({
  id,
  label,
  value,
  options,
  optionLabel,
  onCommit,
}: {
  id: string;
  label: ReactNode;
  value: Common<T | undefined>;
  options: readonly T[];
  optionLabel: (v: T) => string;
  onCommit: (v: T) => void;
}) {
  const { t } = useT();
  return (
    <Row id={id} label={label}>
      <Select
        value={value === MIXED || value === undefined ? '' : value}
        onValueChange={(v) => onCommit(v as T)}
      >
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={value === MIXED ? t('editor:properties.mixed') : ''} />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o} value={o}>
              {optionLabel(o)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Row>
  );
}

export function BoolField({
  id,
  label,
  value,
  onCommit,
}: {
  id: string;
  label: ReactNode;
  value: Common<boolean | undefined>;
  onCommit: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Checkbox
        id={id}
        checked={value === MIXED ? 'indeterminate' : !!value}
        onCheckedChange={(c) => onCommit(c === true)}
      />
      <Label htmlFor={id}>{label}</Label>
    </div>
  );
}
