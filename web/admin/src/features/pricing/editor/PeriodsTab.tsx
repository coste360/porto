import { PlusIcon, Trash2Icon } from 'lucide-react';
import { useState } from 'react';

import type { RatePeriod } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { cn } from '@porto/shared/lib/utils';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';

import { PERIOD_BG, PERIOD_COLORS, PERIOD_KINDS } from '../lib/enums';
import {
  newKey,
  periodColor,
  refOf,
  renamePeriod,
  validityYears,
  yearTimeline,
  type EditorDoc,
} from './model';

export type PeriodsTabProps = {
  doc: EditorDoc;
  onChange: (doc: EditorDoc) => void;
  readOnly: boolean;
};

/** Tab Periodi: seasons and events with their date ranges, and the year timeline. */
export function PeriodsTab({ doc, onChange, readOnly }: PeriodsTabProps) {
  const { t } = useT();
  const setPeriod = (i: number, p: RatePeriod) =>
    onChange({ ...doc, periods: doc.periods.map((x, j) => (j === i ? p : x)) });
  const add = () => {
    const n = doc.periods.length + 1;
    const color = PERIOD_COLORS[(n - 1) % PERIOD_COLORS.length]!;
    onChange({
      ...doc,
      periods: [
        ...doc.periods,
        {
          key: newKey('p'),
          code: `P${n}`,
          name: { it: t('pricing:periods.defaultName', { n }) },
          kind: 'season',
          color,
          ranges: [{ from: doc.valid_from, to: doc.valid_to }],
        },
      ],
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <PeriodTimeline doc={doc} />
      <div className="flex flex-col gap-4">
        {doc.periods.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('pricing:periods.empty')}</p>
        ) : null}
        {doc.periods.map((p, i) => (
          <PeriodCard
            key={refOf(p) || i}
            index={i}
            period={p}
            doc={doc}
            readOnly={readOnly}
            onChange={(np) => setPeriod(i, np)}
            onRename={(to) => onChange(renamePeriod(doc, i, to))}
            onRemove={() => onChange({ ...doc, periods: doc.periods.filter((_, j) => j !== i) })}
          />
        ))}
        {readOnly ? null : (
          <div>
            <Button type="button" variant="outline" onClick={add}>
              <PlusIcon />
              {t('pricing:periods.add')}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function PeriodCard({
  index,
  period: p,
  doc,
  readOnly,
  onChange,
  onRename,
  onRemove,
}: {
  index: number;
  period: RatePeriod;
  doc: EditorDoc;
  readOnly: boolean;
  onChange: (p: RatePeriod) => void;
  onRename: (to: string) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const id = `pl-period-${refOf(p) || index}`;
  return (
    <Card id={id} tabIndex={-1} data-testid="period-card">
      <CardContent className="flex flex-col gap-4 pt-6">
        <fieldset disabled={readOnly} className="grid gap-4 sm:grid-cols-4">
          <Field>
            <FieldLabel htmlFor={`${id}-code`}>{t('pricing:periods.fields.code')}</FieldLabel>
            <Input
              id={`${id}-code`}
              value={p.code}
              maxLength={32}
              autoComplete="off"
              // Renaming also updates the rule scopes naming the period.
              onChange={(e) => onRename(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-name`}>{t('pricing:periods.fields.name')}</FieldLabel>
            <Input
              id={`${id}-name`}
              value={p.name.it}
              maxLength={200}
              onChange={(e) => onChange({ ...p, name: { ...p.name, it: e.target.value } })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-kind`}>{t('pricing:periods.fields.kind')}</FieldLabel>
            <Select
              value={p.kind}
              disabled={readOnly}
              onValueChange={(v) => onChange({ ...p, kind: v as RatePeriod['kind'] })}
            >
              <SelectTrigger id={`${id}-kind`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PERIOD_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`pricingEnums:periodKinds.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor={`${id}-color`}>{t('pricing:periods.fields.color')}</FieldLabel>
            <Select
              value={periodColor(doc.periods, p)}
              disabled={readOnly}
              onValueChange={(v) => onChange({ ...p, color: v as RatePeriod['color'] })}
            >
              <SelectTrigger id={`${id}-color`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PERIOD_COLORS.map((c, ci) => (
                  <SelectItem key={c} value={c}>
                    <span className={cn('size-3 rounded-sm', PERIOD_BG[c])} aria-hidden />
                    {t('pricing:periods.colorN', { n: ci + 1 })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </fieldset>
        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">{t('pricing:periods.fields.ranges')}</span>
          {p.ranges.map((r, ri) => (
            <div key={ri} className="grid grid-cols-7 items-end gap-2">
              <Field className="col-span-3">
                <FieldLabel htmlFor={`${id}-r${ri}-from`}>
                  {t('pricing:periods.fields.from')}
                </FieldLabel>
                <DatePicker
                  id={`${id}-r${ri}-from`}
                  value={r.from}
                  disabled={readOnly}
                  onChange={(d) =>
                    d &&
                    onChange({
                      ...p,
                      ranges: p.ranges.map((x, j) => (j === ri ? { ...x, from: d } : x)),
                    })
                  }
                />
              </Field>
              <Field className="col-span-3">
                <FieldLabel htmlFor={`${id}-r${ri}-to`}>
                  {t('pricing:periods.fields.to')}
                </FieldLabel>
                <DatePicker
                  id={`${id}-r${ri}-to`}
                  value={r.to}
                  disabled={readOnly}
                  onChange={(d) =>
                    d &&
                    onChange({
                      ...p,
                      ranges: p.ranges.map((x, j) => (j === ri ? { ...x, to: d } : x)),
                    })
                  }
                />
              </Field>
              {readOnly ? null : (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  disabled={p.ranges.length <= 1}
                  aria-label={t('pricing:periods.removeRange', { n: ri + 1 })}
                  onClick={() => onChange({ ...p, ranges: p.ranges.filter((_, j) => j !== ri) })}
                >
                  <Trash2Icon />
                </Button>
              )}
            </div>
          ))}
          {readOnly ? null : (
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() =>
                  onChange({
                    ...p,
                    ranges: [...p.ranges, { from: doc.valid_from, to: doc.valid_from }],
                  })
                }
              >
                <PlusIcon />
                {t('pricing:periods.addRange')}
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={onRemove}>
                <Trash2Icon />
                {t('pricing:periods.remove', { code: p.code })}
              </Button>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

const MONTH_KEYS = [
  'jan',
  'feb',
  'mar',
  'apr',
  'may',
  'jun',
  'jul',
  'aug',
  'sep',
  'oct',
  'nov',
  'dec',
] as const;

/** Year timeline: one row per month, one cell per day coloured with the period's `chart-*` token. */
export function PeriodTimeline({ doc }: { doc: EditorDoc }) {
  const { t } = useT();
  const years = validityYears(doc.valid_from, doc.valid_to);
  const [picked, setPicked] = useState<number | null>(null);
  const year = picked !== null && years.includes(picked) ? picked : (years[0] ?? 2000);
  const months = yearTimeline(doc.periods, year, { from: doc.valid_from, to: doc.valid_to });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('pricing:periods.timeline')}</CardTitle>
        <CardDescription>{t('pricing:periods.timelineHint')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {years.length > 1 ? (
          <div className="flex flex-wrap gap-2" role="group" aria-label={t('pricing:periods.year')}>
            {years.map((y) => (
              <Button
                key={y}
                type="button"
                size="sm"
                variant={y === year ? 'default' : 'outline'}
                aria-pressed={y === year}
                onClick={() => setPicked(y)}
              >
                {y}
              </Button>
            ))}
          </div>
        ) : null}
        <div className="flex flex-col gap-1 overflow-x-auto" data-testid="period-timeline">
          {months.map((m) => (
            <div key={m.month} className="flex items-center gap-2">
              <span className="text-muted-foreground w-10 shrink-0 text-xs">
                {t(`pricing:months.${MONTH_KEYS[m.month - 1]}`)}
              </span>
              <div className="grid min-w-96 flex-1 grid-cols-31 gap-px">
                {m.days.map((d) => (
                  <div
                    key={d.date}
                    title={d.period ? `${d.date} · ${d.period}` : d.date}
                    data-date={d.date}
                    data-period={d.period ?? ''}
                    className={cn(
                      'h-4 rounded-sm',
                      d.color ? PERIOD_BG[d.color] : d.inValidity ? 'bg-muted' : 'bg-transparent',
                      !d.inValidity && d.color ? 'opacity-40' : '',
                    )}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
        <ul className="flex flex-wrap gap-3" aria-label={t('pricing:periods.legend')}>
          {doc.periods.map((p, i) => (
            <li key={refOf(p) || i} className="flex items-center gap-2 text-sm">
              <span
                className={cn('size-3 rounded-sm', PERIOD_BG[periodColor(doc.periods, p)])}
                aria-hidden
              />
              <span className="font-mono">{p.code}</span>
              <span>{p.name.it}</span>
              <Badge variant="outline">{t(`pricingEnums:periodKinds.${p.kind}`)}</Badge>
            </li>
          ))}
          <li className="flex items-center gap-2 text-sm">
            <span className="bg-muted size-3 rounded-sm" aria-hidden />
            {t('pricing:periods.noPeriod')}
          </li>
        </ul>
      </CardContent>
    </Card>
  );
}
