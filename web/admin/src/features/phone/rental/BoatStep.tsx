import { CameraIcon, CheckCheckIcon } from 'lucide-react';
import { useMemo, useState } from 'react';

import {
  useRentalSettings,
  useSaveHandoverChecklist,
  useUploadHandoverPhoto,
  type RentalHandover,
  type RentalPhase,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { isQueuedOffline } from '@porto/shared/offline';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Slider } from '@porto/shared/ui/slider';
import { Toggle } from '@porto/shared/ui/toggle';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { parseHours } from '../../rental-handover/lib/wizard';
import { reencodePhoto } from '../lib/photo';

/**
 * The boat step of both wizards (m11-plan §7 step 4, §8 step 2): checklist ("Tutto ok" in one tap), fuel in eighths,
 * engine hours on the numeric keypad and boat photos. The save and the photos go through the offline queue (kind
 * rental.checklist): they never change status, charges or deposits (INV-71).
 */
export function BoatStep({
  bookingId,
  label,
  phase,
  category,
  handover,
  onSaved,
}: {
  bookingId: string;
  label: string;
  phase: RentalPhase;
  category: string;
  handover: RentalHandover | undefined;
  onSaved: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const settings = useRentalSettings(marinaId);
  const save = useSaveHandoverChecklist(marinaId);
  const upload = useUploadHandoverPhoto(marinaId);
  const items = useMemo(
    () =>
      (settings.data?.checklist ?? []).filter(
        (it) => !it.categories?.length || it.categories.includes(category as never),
      ),
    [settings.data, category],
  );
  const initial = Object.fromEntries((handover?.checklist ?? []).map((c) => [c.key, c.ok]));
  const [ok, setOk] = useState<Record<string, boolean>>(initial);
  const [fuel, setFuel] = useState<number>(handover?.fuel_eighths ?? 8);
  const [hours, setHours] = useState(
    handover?.engine_hours_milli != null
      ? String(handover.engine_hours_milli / 1000).replace('.', ',')
      : '',
  );
  const photos = (handover?.files ?? []).filter((f) => f.kind === 'boat').length;
  const [queued, setQueued] = useState(0);
  const lang = i18n.language === 'en' ? 'en' : 'it';
  const hoursMilli = parseHours(hours);
  const submit = () =>
    save.mutate(
      {
        bookingId,
        phase,
        label,
        body: {
          items: items.map((it) => ({ key: it.key, ok: ok[it.key] ?? false })),
          fuel_eighths: fuel,
          ...(hoursMilli != null ? { engine_hours_milli: hoursMilli } : {}),
          device_at: new Date().toISOString(),
        },
      },
      {
        onSuccess: onSaved,
        onError: (e) => {
          if (isQueuedOffline(e)) onSaved();
        },
      },
    );
  return (
    <div className="flex flex-col gap-4" data-testid={`boat-step-${phase}`}>
      <Button
        variant="secondary"
        className="min-h-11"
        data-testid="checklist-all-ok"
        onClick={() => setOk(Object.fromEntries(items.map((it) => [it.key, true])))}
      >
        <CheckCheckIcon />
        {t('rentalHandover:boat.allOk')}
      </Button>
      <div className="grid grid-cols-2 gap-2">
        {items.map((it) => (
          <Toggle
            key={it.key}
            variant="outline"
            className="min-h-11 justify-start"
            pressed={ok[it.key] ?? false}
            onPressedChange={(v) => setOk((o) => ({ ...o, [it.key]: v }))}
          >
            {it.label[lang] ?? it.label.it ?? it.key}
          </Toggle>
        ))}
      </div>
      <Field>
        <FieldLabel htmlFor={`fuel-${phase}`}>
          {t('rentalHandover:boat.fuel', { value: fuel })}
        </FieldLabel>
        <Slider
          id={`fuel-${phase}`}
          min={0}
          max={8}
          step={1}
          value={[fuel]}
          onValueChange={(v) => setFuel(v[0] ?? 0)}
          aria-label={t('rentalHandover:boat.fuelLabel')}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor={`hours-${phase}`}>{t('rentalHandover:boat.engineHours')}</FieldLabel>
        <Input
          id={`hours-${phase}`}
          inputMode="decimal"
          value={hours}
          onChange={(e) => setHours(e.target.value.replace(/[^\d,.]/g, ''))}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor={`photo-${phase}`}>
          {t('rentalHandover:boat.photos', { count: photos + queued })}
        </FieldLabel>
        <label className="hover:bg-muted flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md border">
          <CameraIcon className="size-4" />
          {t('rentalHandover:boat.addPhoto')}
          <input
            id={`photo-${phase}`}
            data-testid={`boat-photo-${phase}`}
            type="file"
            accept="image/*"
            capture="environment"
            className="sr-only"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              e.target.value = '';
              if (!file) return;
              const blob = await reencodePhoto(file);
              upload.mutate(
                { bookingId, phase, kind: 'boat', file: blob, label },
                {
                  onError: (err) => {
                    if (isQueuedOffline(err)) setQueued((n) => n + 1);
                  },
                },
              );
            }}
          />
        </label>
      </Field>
      {save.error && !isQueuedOffline(save.error) ? <DeskProblemAlert error={save.error} /> : null}
      <Button
        className="min-h-11"
        data-testid="boat-save"
        disabled={save.isPending}
        onClick={submit}
      >
        {t('rentalHandover:boat.save')}
      </Button>
    </div>
  );
}
