import { ArrowLeftIcon, CameraIcon, GaugeIcon } from 'lucide-react';
import { useDeferredValue, useMemo, useState } from 'react';
import { toast } from 'sonner';

import { useRecordMeterReading, useUtilityMeters, type UtilityMeter } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate, formatQtyMilli, parseQtyToMilli } from '@porto/shared/lib/format';
import { useQueue } from '@porto/shared/offline';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { EmptyState } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { expectedRange } from './range';

/** Dock tab "Letture" (m9-plan §12.2): meter search by code, berth or socket; previous value and expected range;
 * numeric input; optional photo; offline queue (kind utility.reading). */
export function ReadingsTab({ today }: { today: string }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [q, setQ] = useState('');
  const deferred = useDeferredValue(q.trim().toLowerCase());
  const meters = useUtilityMeters(marinaId, { route: 'dock' });
  const queue = useQueue(marinaId);
  const [selected, setSelected] = useState<UtilityMeter | null>(null);
  const list = useMemo(
    () =>
      (meters.data?.items ?? []).filter(
        (m) =>
          !deferred ||
          m.code.toLowerCase().includes(deferred) ||
          (m.socket_code ?? '').toLowerCase().includes(deferred) ||
          (m.serial ?? '').toLowerCase().includes(deferred),
      ),
    [meters.data, deferred],
  );
  if (selected)
    return <ReadingForm meter={selected} today={today} onDone={() => setSelected(null)} />;
  return (
    <div className="flex flex-col gap-4 p-4" data-testid="readings-tab">
      <h2 className="text-lg font-semibold">{t('utilities:dock.readings')}</h2>
      {queue.pending > 0 ? (
        <p role="status" className="text-muted-foreground text-sm">
          {t('utilities:dock.queued', { count: queue.pending })}
        </p>
      ) : null}
      <Field>
        <FieldLabel htmlFor="dock-meter-q">{t('utilities:dock.search')}</FieldLabel>
        <Input
          id="dock-meter-q"
          value={q}
          autoComplete="off"
          onChange={(e) => setQ(e.target.value)}
        />
      </Field>
      {list.length === 0 ? (
        <EmptyState icon={<GaugeIcon />} title={t('utilities:dock.noMeters')} />
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((m) => (
            <li key={m.id}>
              <Button
                variant="outline"
                className="min-h-11 w-full justify-between"
                data-testid="dock-meter"
                onClick={() => setSelected(m)}
              >
                <span className="font-medium">{m.code}</span>
                <span className="text-muted-foreground text-sm">
                  {t(`utilities:enums.kind.${m.kind}`)}
                </span>
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ReadingForm({
  meter,
  today,
  onDone,
}: {
  meter: UtilityMeter;
  today: string;
  onDone: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const record = useRecordMeterReading(marinaId);
  const [value, setValue] = useState('');
  const [note, setNote] = useState('');
  const [photo, setPhoto] = useState<File | null>(null);
  const milli = parseQtyToMilli(value, i18n.language);
  const range = expectedRange(meter, today);
  const unit = t(`utilities:enums.unit.${meter.unit}`);
  const outOfRange =
    milli !== null &&
    range !== null &&
    (milli < range.min || (range.max !== null && milli > range.max));
  const submit = () =>
    record.mutate(
      {
        meterId: meter.id,
        meterCode: meter.code,
        value,
        ...(note.trim() ? { note: note.trim() } : {}),
        ...(photo ? { photo } : {}),
      },
      {
        onSuccess: () => {
          toast.success(t('utilities:dock.saved', { code: meter.code }));
          onDone();
        },
      },
    );
  return (
    <div className="flex flex-col gap-4 p-4" data-testid="reading-form">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onDone}>
        <ArrowLeftIcon />
        {t('utilities:dock.back')}
      </Button>
      <h2 className="text-lg font-semibold">{meter.code}</h2>
      {meter.last_reading ? (
        <p className="text-sm" data-testid="previous-reading">
          {t('utilities:dock.previous', {
            value: formatQtyMilli(meter.last_reading.value_milli, i18n.language),
            unit,
            date: formatIsoDate(meter.last_reading.read_on),
          })}
        </p>
      ) : (
        <p className="text-muted-foreground text-sm">{t('utilities:dock.first')}</p>
      )}
      <Field>
        <FieldLabel htmlFor="dock-value">{t('utilities:dock.value', { unit })}</FieldLabel>
        <Input
          id="dock-value"
          inputMode="decimal"
          autoComplete="off"
          className="h-14 text-2xl tabular-nums"
          value={value}
          aria-invalid={value !== '' && milli === null}
          onChange={(e) => setValue(e.target.value.replace(/[^\d.,]/g, ''))}
        />
        {range ? (
          <FieldDescription data-testid="expected-range">
            {range.max !== null
              ? t('utilities:dock.range', {
                  min: formatQtyMilli(range.min, i18n.language),
                  max: formatQtyMilli(range.max, i18n.language),
                  unit,
                })
              : t('utilities:dock.rangeMin', {
                  min: formatQtyMilli(range.min, i18n.language),
                  unit,
                })}
          </FieldDescription>
        ) : null}
      </Field>
      {outOfRange ? (
        <p role="alert" className="text-destructive text-sm" data-testid="out-of-range">
          {t('utilities:dock.outOfRange')}
        </p>
      ) : null}
      <Field>
        <FieldLabel htmlFor="dock-photo">
          <CameraIcon className="size-4" /> {t('utilities:dock.photo')}
        </FieldLabel>
        <Input
          id="dock-photo"
          type="file"
          accept="image/jpeg,image/png"
          capture="environment"
          onChange={(e) => setPhoto(e.target.files?.[0] ?? null)}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="dock-note">{t('utilities:dock.note')}</FieldLabel>
        <Textarea
          id="dock-note"
          value={note}
          maxLength={500}
          onChange={(e) => setNote(e.target.value)}
        />
      </Field>
      <DeskProblemAlert error={record.error} />
      <Button className="min-h-11" disabled={milli === null || record.isPending} onClick={submit}>
        {t('utilities:dock.save')}
      </Button>
    </div>
  );
}
