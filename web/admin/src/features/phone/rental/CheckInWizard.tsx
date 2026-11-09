import { ArrowLeftIcon, MinusIcon, PlusIcon } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';

import {
  useCheckInRental,
  useCreateRentalDamage,
  usePreviewRentalCheckIn,
  useRentalBooking,
  useRentalExtras,
  useRentalHandover,
  useUploadHandoverPhoto,
  type RentalCheckInInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, parseEuroToCents } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Progress } from '@porto/shared/ui/progress';
import { Textarea } from '@porto/shared/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { QuickCashDialog } from '../../rental-handover/components/QuickCashDialog';
import {
  CHECK_IN_STEPS,
  canCommit,
  retainOf,
  type CheckInStep,
} from '../../rental-handover/lib/wizard';
import { reencodePhoto } from '../lib/photo';
import { BoatStep } from './BoatStep';
import { useUnitCategory } from './useUnitCategory';

type Decision = RentalCheckInInput['deposit_decision'];

/** Local "YYYY-MM-DDTHH:MM" of an instant for a datetime-local input. */
function localInput(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

/**
 * Check-in wizard of the rental phone (m11-plan §8): return time (now, editable), boat (offline queue), damages,
 * extras used, summary with the stateless preview (late fee, fuel, extras, damage excess, deposit proposal), the
 * deposit decision and "Rientro"; then quick cash of the return charge.
 */
export function CheckInWizard({ bookingId, onDone }: { bookingId: string; onDone: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const booking = useRentalBooking(marinaId, bookingId);
  const handover = useRentalHandover(marinaId, bookingId, 'checkin');
  const category = useUnitCategory(booking.data?.unit_id ?? '');
  const [step, setStep] = useState<CheckInStep>('return');
  const [returned, setReturned] = useState(() => localInput(new Date()));
  const [extras, setExtras] = useState<Record<string, number>>({});
  const b = booking.data;
  const label = b ? `${b.number} ${b.unit_code}` : '';
  const index = CHECK_IN_STEPS.indexOf(step);
  const go = (s: CheckInStep) => setStep(s);
  return (
    <div className="flex flex-col gap-4" data-testid="checkin-wizard">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onDone}>
        <ArrowLeftIcon />
        {t('rentalHandover:back')}
      </Button>
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">{t('rentalHandover:checkin.title', { label })}</h1>
        <Progress
          value={((index + 1) / CHECK_IN_STEPS.length) * 100}
          aria-label={t('rentalHandover:progress')}
        />
        <div className="flex flex-wrap gap-1">
          {CHECK_IN_STEPS.map((s) => (
            <Badge
              key={s}
              variant={s === step ? 'default' : 'outline'}
              className="cursor-pointer"
              onClick={() => go(s)}
            >
              {t(`rentalHandover:steps.${s}`)}
            </Badge>
          ))}
        </div>
      </div>
      {!b ? null : step === 'return' ? (
        <div className="flex flex-col gap-3" data-testid="return-step">
          <Field>
            <FieldLabel htmlFor="returned-at">{t('rentalHandover:checkin.returnedAt')}</FieldLabel>
            <Input
              id="returned-at"
              type="datetime-local"
              value={returned}
              onChange={(e) => setReturned(e.target.value)}
            />
          </Field>
          <Button className="min-h-11" data-testid="return-next" onClick={() => go('boat')}>
            {t('rentalHandover:next')}
          </Button>
        </div>
      ) : step === 'boat' ? (
        <BoatStep
          bookingId={bookingId}
          label={label}
          phase="checkin"
          category={category}
          handover={handover.data}
          onSaved={() => go('damages')}
        />
      ) : step === 'damages' ? (
        <DamagesStep bookingId={bookingId} label={label} onDone={() => go('extras')} />
      ) : step === 'extras' ? (
        <ExtrasStep value={extras} onChange={setExtras} onDone={() => go('summary')} />
      ) : (
        <ReturnSummary
          bookingId={bookingId}
          version={b.version}
          returnedAt={new Date(returned).toISOString()}
          extras={extras}
          missing={handover.data?.missing ?? ['checklist']}
          onDone={onDone}
        />
      )}
    </div>
  );
}

function DamagesStep({
  bookingId,
  label,
  onDone,
}: {
  bookingId: string;
  label: string;
  onDone: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateRentalDamage(marinaId);
  const upload = useUploadHandoverPhoto(marinaId);
  const [adding, setAdding] = useState(false);
  const [description, setDescription] = useState('');
  const [estimate, setEstimate] = useState('');
  const [photo, setPhoto] = useState<Blob | null>(null);
  const cents = parseEuroToCents(estimate);
  if (!adding)
    return (
      <div className="flex flex-col gap-3" data-testid="damages-step">
        <Button className="min-h-11" data-testid="damages-none" onClick={onDone}>
          {t('rentalHandover:damages.none')}
        </Button>
        <Button
          className="min-h-11"
          variant="outline"
          data-testid="damages-add"
          onClick={() => setAdding(true)}
        >
          {t('rentalHandover:damages.add')}
        </Button>
      </div>
    );
  return (
    <div className="flex flex-col gap-3" data-testid="damage-form">
      <label className="hover:bg-muted flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md border">
        {t('rentalHandover:damages.photo')}
        <input
          data-testid="damage-photo"
          type="file"
          accept="image/*"
          capture="environment"
          className="sr-only"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            e.target.value = '';
            if (file) setPhoto(await reencodePhoto(file));
          }}
        />
      </label>
      <Field>
        <FieldLabel htmlFor="damage-description">
          {t('rentalHandover:damages.description')}
        </FieldLabel>
        <Textarea
          id="damage-description"
          maxLength={500}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="damage-estimate">{t('rentalHandover:damages.estimate')}</FieldLabel>
        <Input
          id="damage-estimate"
          inputMode="decimal"
          value={estimate}
          onChange={(e) => setEstimate(e.target.value)}
        />
      </Field>
      {create.error ? <DeskProblemAlert error={create.error} /> : null}
      <Button
        className="min-h-11"
        data-testid="damage-save"
        disabled={!description.trim() || cents == null || create.isPending}
        onClick={() =>
          create.mutate(
            { bookingId, body: { description: description.trim(), estimate_cents: cents ?? 0 } },
            {
              onSuccess: async (d) => {
                if (photo)
                  await upload.mutateAsync({
                    bookingId,
                    phase: 'checkin',
                    kind: 'damage',
                    damageId: d.id,
                    file: photo,
                    label,
                  });
                setAdding(false);
                onDone();
              },
            },
          )
        }
      >
        {t('rentalHandover:damages.save')}
      </Button>
    </div>
  );
}

function ExtrasStep({
  value,
  onChange,
  onDone,
}: {
  value: Record<string, number>;
  onChange: (v: Record<string, number>) => void;
  onDone: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const extras = useRentalExtras(marinaId);
  const list = (extras.data?.items ?? []).filter((x) => x.active);
  return (
    <div className="flex flex-col gap-3" data-testid="extras-step">
      {list.map((x) => (
        <div key={x.id} className="flex min-h-11 items-center justify-between gap-2">
          <span>{x.item_code}</span>
          <span className="flex items-center gap-2">
            <Button
              size="icon"
              variant="outline"
              aria-label={t('rentalHandover:extras.less')}
              onClick={() => onChange({ ...value, [x.id]: Math.max(0, (value[x.id] ?? 0) - 1) })}
            >
              <MinusIcon />
            </Button>
            <span className="w-6 text-center">{value[x.id] ?? 0}</span>
            <Button
              size="icon"
              variant="outline"
              aria-label={t('rentalHandover:extras.more')}
              onClick={() => onChange({ ...value, [x.id]: (value[x.id] ?? 0) + 1 })}
            >
              <PlusIcon />
            </Button>
          </span>
        </div>
      ))}
      <Button className="min-h-11" data-testid="extras-next" onClick={onDone}>
        {t('rentalHandover:next')}
      </Button>
    </div>
  );
}

function ReturnSummary({
  bookingId,
  version,
  returnedAt,
  extras,
  missing,
  onDone,
}: {
  bookingId: string;
  version: number;
  returnedAt: string;
  extras: Record<string, number>;
  missing: readonly string[];
  onDone: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const preview = usePreviewRentalCheckIn(marinaId);
  const checkIn = useCheckInRental(marinaId);
  const [decision, setDecision] = useState<Decision>('release');
  const [typed, setTyped] = useState('');
  const [cash, setCash] = useState(false);
  const [done, setDone] = useState(false);
  const extraList = Object.entries(extras)
    .filter(([, n]) => n > 0)
    .map(([extra_id, n]) => ({ extra_id, qty_milli: n * 1000 }));
  const p = preview.data;
  const retain = retainOf(decision, p?.deposit_cents ?? 0, parseEuroToCents(typed) ?? 0);
  const body: RentalCheckInInput = {
    returned_at: returnedAt,
    deposit_decision: decision,
    ...(decision === 'retain' ? { retain_cents: retain } : {}),
    ...(extraList.length ? { extras: extraList } : {}),
  };
  const key = JSON.stringify(body);
  useEffect(() => {
    preview.mutate({ bookingId, body });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- recomputed when the inputs change
  }, [bookingId, key]);
  const fmt = (c: number) => formatCents(c, 'EUR', i18n.language);
  return (
    <div className="flex flex-col gap-3" data-testid="checkin-summary">
      {p ? (
        <dl className="grid grid-cols-2 gap-1 text-sm" data-testid="checkin-preview">
          <dt>{t('rentalHandover:checkin.late', { minutes: p.late_minutes })}</dt>
          <dd className="text-right">{fmt(p.late_fee_cents)}</dd>
          <dt>{t('rentalHandover:checkin.fuel')}</dt>
          <dd className="text-right">{fmt(p.fuel_cents)}</dd>
          <dt>{t('rentalHandover:checkin.extras')}</dt>
          <dd className="text-right">{fmt(p.extras_cents)}</dd>
          <dt>{t('rentalHandover:checkin.damageExcess')}</dt>
          <dd className="text-right">{fmt(p.damage_excess_cents)}</dd>
          <dt className="font-semibold">{t('rentalHandover:checkin.total')}</dt>
          <dd className="text-right font-semibold" data-testid="return-total">
            {fmt(p.return_total_cents)}
          </dd>
          <dt>{t('rentalHandover:checkin.proposal')}</dt>
          <dd className="text-right">{fmt(p.deposit_proposal_cents)}</dd>
        </dl>
      ) : null}
      <ToggleGroup
        type="single"
        variant="outline"
        value={decision}
        onValueChange={(v) => {
          if (!v) return;
          setDecision(v as Decision);
          if (v === 'retain' && !typed && p && p.deposit_proposal_cents > 0)
            setTyped(String(p.deposit_proposal_cents / 100).replace('.', ','));
        }}
        className="grid w-full grid-cols-3"
      >
        {(['release', 'retain_all', 'retain'] as const).map((d) => (
          <ToggleGroupItem key={d} value={d} className="min-h-11" data-testid={`decision-${d}`}>
            {t(`rentalHandover:decision.${d}`)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      {decision === 'retain' ? (
        <Field>
          <FieldLabel htmlFor="retain-amount">{t('rentalHandover:decision.amount')}</FieldLabel>
          <Input
            id="retain-amount"
            inputMode="decimal"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
        </Field>
      ) : null}
      {missing.length > 0 ? (
        <ul className="text-destructive text-sm">
          {missing.map((m) => (
            <li key={m}>{t(`rentalHandover:missing.${m}`)}</li>
          ))}
        </ul>
      ) : null}
      {preview.error ? <DeskProblemAlert error={preview.error} /> : null}
      {checkIn.error ? <DeskProblemAlert error={checkIn.error} /> : null}
      {!done ? (
        <Button
          className="min-h-11"
          data-testid="checkin-commit"
          disabled={!canCommit(missing as never) || checkIn.isPending}
          onClick={() =>
            checkIn.mutate(
              { bookingId, version, body },
              {
                onSuccess: () => {
                  toast.success(t('rentalHandover:checkin.done'));
                  setDone(true);
                  if (!p || p.return_total_cents === 0) onDone();
                },
              },
            )
          }
        >
          {t('rentalHandover:checkin.commit')}
        </Button>
      ) : (
        <>
          <Button
            className="min-h-11"
            variant="secondary"
            data-testid="checkin-cash"
            onClick={() => setCash(true)}
          >
            {t('rentalHandover:cash.collect')}
          </Button>
          <Button className="min-h-11" variant="outline" onClick={onDone}>
            {t('rentalHandover:finish')}
          </Button>
        </>
      )}
      <QuickCashDialog
        marinaId={marinaId}
        bookingId={bookingId}
        target="return"
        open={cash}
        onOpenChange={setCash}
        onDone={() => onDone()}
      />
    </div>
  );
}
