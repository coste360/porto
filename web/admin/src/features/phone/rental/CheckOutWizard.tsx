import { ArrowLeftIcon, CameraIcon, CheckIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  useCheckOutRental,
  usePutHandoverIdentity,
  useRentalBooking,
  useRentalDeposit,
  useRentalHandover,
  useSignRentalContract,
  useStartRentalDeposit,
  useUploadHandoverPhoto,
  type RentalDepositInput,
  type RentalFileKind,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Progress } from '@porto/shared/ui/progress';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { DeskProblemAlert } from '../../desk/components/DeskProblemAlert';
import { QuickCashDialog } from '../../rental-handover/components/QuickCashDialog';
import { RenterQr } from '../../rental-handover/components/RenterQr';
import { SignaturePad } from '../../rental-handover/components/SignaturePad';
import {
  canCommit,
  checkOutSteps,
  firstOpen,
  stepDone,
  type CheckOutStep,
} from '../../rental-handover/lib/wizard';
import { reencodePhoto } from '../lib/photo';
import { BoatStep } from './BoatStep';
import { useUnitCategory } from './useUnitCategory';

/**
 * Check-out wizard of the rental phone (m11-plan §7): identity, licence (when needed), contract signed on screen,
 * deposit, boat, summary and "Consegna". Every step is online except the boat step (offline queue). The step
 * header follows the server's missing list (the same as 422 handover_incomplete).
 */
export function CheckOutWizard({ bookingId, onDone }: { bookingId: string; onDone: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const booking = useRentalBooking(marinaId, bookingId);
  const handover = useRentalHandover(marinaId, bookingId, 'checkout');
  const category = useUnitCategory(booking.data?.unit_id ?? '');
  const steps = checkOutSteps(booking.data?.licence_required ?? false);
  const missing = handover.data?.missing ?? [];
  const [step, setStep] = useState<CheckOutStep | null>(null);
  const [form, setForm] = useState<IdentityForm>({
    number: '',
    expires: '',
    licNumber: '',
    licExpires: '',
  });
  const current = step ?? firstOpen(steps, missing);
  const next = () =>
    setStep(
      firstOpen(
        steps,
        missing.filter((m) => !coveredBy(current).includes(m)),
      ),
    );
  const index = steps.indexOf(current);
  const b = booking.data;
  const label = b ? `${b.number} ${b.unit_code}` : '';
  return (
    <div className="flex flex-col gap-4" data-testid="checkout-wizard">
      <Button variant="ghost" className="min-h-11 self-start" onClick={onDone}>
        <ArrowLeftIcon />
        {t('rentalHandover:back')}
      </Button>
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold">{t('rentalHandover:checkout.title', { label })}</h1>
        <Progress
          value={((index + 1) / steps.length) * 100}
          aria-label={t('rentalHandover:progress')}
        />
        <div className="flex flex-wrap gap-1">
          {steps.map((s) => (
            <Badge
              key={s}
              variant={s === current ? 'default' : stepDone(s, missing) ? 'secondary' : 'outline'}
              className="cursor-pointer"
              onClick={() => setStep(s)}
            >
              {t(`rentalHandover:steps.${s}`)}
            </Badge>
          ))}
        </div>
      </div>
      {!b ? null : current === 'identity' || current === 'licence' ? (
        <IdentityStep
          bookingId={bookingId}
          licence={current === 'licence'}
          form={form}
          setForm={setForm}
          onSaved={next}
          label={label}
        />
      ) : current === 'contract' ? (
        <ContractStep
          bookingId={bookingId}
          summary={`${b.unit_code} · ${b.formula_code} · ${formatCents(b.total_cents, 'EUR', i18n.language)}`}
          onSigned={next}
        />
      ) : current === 'deposit' ? (
        <DepositStep bookingId={bookingId} onDone={next} />
      ) : current === 'boat' ? (
        <BoatStep
          bookingId={bookingId}
          label={label}
          phase="checkout"
          category={category}
          handover={handover.data}
          onSaved={next}
        />
      ) : (
        <SummaryStep bookingId={bookingId} version={b.version} missing={missing} onDone={onDone} />
      )}
    </div>
  );
}

function coveredBy(step: CheckOutStep) {
  return step === 'boat' ? ['checklist', 'fuel', 'engine_hours', 'photos'] : [step];
}

function PhotoInput({
  id,
  label,
  onFile,
}: {
  id: string;
  label: string;
  onFile: (f: Blob) => void;
}) {
  return (
    <label className="hover:bg-muted flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-md border">
      <CameraIcon className="size-4" />
      {label}
      <input
        id={id}
        data-testid={id}
        type="file"
        accept="image/*"
        capture="environment"
        className="sr-only"
        onChange={async (e) => {
          const file = e.target.files?.[0];
          e.target.value = '';
          if (file) onFile(await reencodePhoto(file));
        }}
      />
    </label>
  );
}

type IdentityForm = { number: string; expires: string; licNumber: string; licExpires: string };

function IdentityStep({
  bookingId,
  licence,
  label,
  form,
  setForm,
  onSaved,
}: {
  bookingId: string;
  licence: boolean;
  label: string;
  form: IdentityForm;
  setForm: (f: IdentityForm) => void;
  onSaved: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const put = usePutHandoverIdentity(marinaId);
  const upload = useUploadHandoverPhoto(marinaId);
  const { number, expires, licNumber, licExpires } = form;
  const set = (k: keyof IdentityForm) => (v: string) => setForm({ ...form, [k]: v });
  const setNumber = set('number');
  const setExpires = set('expires');
  const setLicNumber = set('licNumber');
  const setLicExpires = set('licExpires');
  const [photo, setPhoto] = useState<Blob | null>(null);
  const [licPhoto, setLicPhoto] = useState<Blob | null>(null);
  const kind: RentalFileKind = licence ? 'licence' : 'id_document';
  const ready = number.trim() && expires && (!licence || (licNumber.trim() && licExpires));
  const save = () =>
    put.mutate(
      {
        bookingId,
        body: {
          document_type: 'identity_card',
          document_number: number.trim(),
          document_expires_on: expires,
          ...(licNumber.trim()
            ? { licence_number: licNumber.trim(), licence_expires_on: licExpires }
            : {}),
        },
      },
      {
        onSuccess: async () => {
          const p = licence ? licPhoto : photo;
          if (p) await upload.mutateAsync({ bookingId, phase: 'checkout', kind, file: p, label });
          onSaved();
        },
      },
    );
  return (
    <div className="flex flex-col gap-3" data-testid={licence ? 'licence-step' : 'identity-step'}>
      {!licence ? (
        <>
          <Field>
            <FieldLabel htmlFor="doc-number">{t('rentalHandover:identity.number')}</FieldLabel>
            <Input
              id="doc-number"
              autoComplete="off"
              value={number}
              onChange={(e) => setNumber(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="doc-expires">{t('rentalHandover:identity.expires')}</FieldLabel>
            <Input
              id="doc-expires"
              type="date"
              value={expires}
              onChange={(e) => setExpires(e.target.value)}
            />
          </Field>
        </>
      ) : null}
      {!licence ? (
        <PhotoInput
          id="photo-id-document"
          label={t('rentalHandover:identity.photo')}
          onFile={setPhoto}
        />
      ) : (
        <>
          <Field>
            <FieldLabel htmlFor="lic-number">{t('rentalHandover:licence.number')}</FieldLabel>
            <Input
              id="lic-number"
              autoComplete="off"
              value={licNumber}
              onChange={(e) => setLicNumber(e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="lic-expires">{t('rentalHandover:licence.expires')}</FieldLabel>
            <Input
              id="lic-expires"
              type="date"
              value={licExpires}
              onChange={(e) => setLicExpires(e.target.value)}
            />
          </Field>
          <PhotoInput
            id="photo-licence"
            label={t('rentalHandover:licence.photo')}
            onFile={setLicPhoto}
          />
        </>
      )}
      {put.error ? <DeskProblemAlert error={put.error} /> : null}
      {upload.error ? <DeskProblemAlert error={upload.error} /> : null}
      <Button
        className="min-h-11"
        data-testid="identity-save"
        disabled={!ready || put.isPending}
        onClick={save}
      >
        {t('rentalHandover:next')}
      </Button>
    </div>
  );
}

function ContractStep({
  bookingId,
  summary,
  onSigned,
}: {
  bookingId: string;
  summary: string;
  onSigned: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const sign = useSignRentalContract(marinaId);
  const [png, setPng] = useState<string | null>(null);
  return (
    <div className="flex flex-col gap-3" data-testid="contract-step">
      <p className="text-sm">{summary}</p>
      <p className="text-muted-foreground text-sm">{t('rentalHandover:sign.hint')}</p>
      <SignaturePad label={t('rentalHandover:sign.pad')} onChange={setPng} />
      {sign.error ? <DeskProblemAlert error={sign.error} /> : null}
      <Button
        className="min-h-11"
        data-testid="contract-sign"
        disabled={!png || sign.isPending}
        onClick={() =>
          sign.mutate(
            {
              bookingId,
              body: {
                driver: 'screen',
                signature_png: png!,
                device_label: navigator.userAgent.slice(0, 80),
              },
            },
            { onSuccess: onSigned },
          )
        }
      >
        {t('rentalHandover:sign.submit')}
      </Button>
      <div className="flex gap-2">
        <Button
          variant="ghost"
          className="min-h-11"
          onClick={() => sign.mutate({ bookingId, body: { driver: 'paper' } })}
        >
          {t('rentalHandover:sign.paper')}
        </Button>
        <Button
          variant="ghost"
          className="min-h-11"
          onClick={() => sign.mutate({ bookingId, body: { driver: 'otp' } })}
        >
          {t('rentalHandover:sign.otp')}
        </Button>
      </div>
    </div>
  );
}

function DepositStep({ bookingId, onDone }: { bookingId: string; onDone: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const deposit = useRentalDeposit(marinaId, bookingId);
  const start = useStartRentalDeposit(marinaId);
  const [mode, setMode] = useState<RentalDepositInput['mode']>('cash');
  const [reason, setReason] = useState('');
  const d = deposit.data;
  const taken = d && ['held', 'authorized', 'waived'].includes(d.status);
  return (
    <div className="flex flex-col gap-3" data-testid="deposit-step">
      {d ? (
        <p className="text-sm">
          {t('rentalHandover:deposit.amount', {
            amount: formatCents(d.amount_cents, 'EUR', i18n.language),
          })}{' '}
          <Badge variant="outline">{t(`rentalHandover:deposit.status.${d.status}`)}</Badge>
        </p>
      ) : null}
      {taken ? (
        <Button className="min-h-11" onClick={onDone}>
          <CheckIcon />
          {t('rentalHandover:next')}
        </Button>
      ) : (
        <>
          <ToggleGroup
            type="single"
            variant="outline"
            value={mode}
            onValueChange={(v) => v && setMode(v as RentalDepositInput['mode'])}
            className="grid w-full grid-cols-2"
          >
            {(['cash', 'card_pos', 'preauth', 'waived'] as const).map((m) => (
              <ToggleGroupItem
                key={m}
                value={m}
                className="min-h-11"
                data-testid={`deposit-mode-${m}`}
              >
                {t(`rentalHandover:deposit.mode.${m}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          {mode === 'waived' ? (
            <Field>
              <FieldLabel htmlFor="deposit-reason">{t('rentalHandover:deposit.reason')}</FieldLabel>
              <Input
                id="deposit-reason"
                value={reason}
                maxLength={200}
                onChange={(e) => setReason(e.target.value)}
              />
            </Field>
          ) : null}
          {d?.checkout_qr_svg ? (
            <RenterQr
              svg={d.checkout_qr_svg}
              alt={t('rentalHandover:deposit.qrAlt')}
              hint={t('rentalHandover:deposit.qrHint')}
              testId="deposit-qr"
            />
          ) : null}
          {d?.checkout_url ? (
            <a
              className="text-primary break-all underline"
              href={d.checkout_url}
              target="_blank"
              rel="noreferrer"
              data-testid="deposit-link"
            >
              {t('rentalHandover:deposit.openPage')}
            </a>
          ) : null}
          {start.error ? <DeskProblemAlert error={start.error} /> : null}
          <Button
            className="min-h-11"
            data-testid="deposit-take"
            disabled={start.isPending || (mode === 'waived' && !reason.trim())}
            onClick={() =>
              start.mutate(
                {
                  bookingId,
                  body: { mode, ...(mode === 'waived' ? { reason: reason.trim() } : {}) },
                },
                {
                  onSuccess: (r) => {
                    if (r.status !== 'pending') onDone();
                  },
                },
              )
            }
          >
            {t('rentalHandover:deposit.take')}
          </Button>
        </>
      )}
    </div>
  );
}

function SummaryStep({
  bookingId,
  version,
  missing,
  onDone,
}: {
  bookingId: string;
  version: number;
  missing: readonly string[];
  onDone: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const checkOut = useCheckOutRental(marinaId);
  const [cash, setCash] = useState(false);
  const [done, setDone] = useState(false);
  return (
    <div className="flex flex-col gap-3" data-testid="checkout-summary">
      {missing.length > 0 ? (
        <ul className="text-destructive text-sm">
          {missing.map((m) => (
            <li key={m}>{t(`rentalHandover:missing.${m}`)}</li>
          ))}
        </ul>
      ) : (
        <p className="text-sm">{t('rentalHandover:checkout.ready')}</p>
      )}
      {checkOut.error ? <DeskProblemAlert error={checkOut.error} /> : null}
      {!done ? (
        <Button
          className="min-h-11"
          data-testid="checkout-commit"
          disabled={!canCommit(missing as never) || checkOut.isPending}
          onClick={() =>
            checkOut.mutate(
              { bookingId, version },
              {
                onSuccess: () => {
                  toast.success(t('rentalHandover:checkout.done'));
                  setDone(true);
                },
              },
            )
          }
        >
          {t('rentalHandover:checkout.commit')}
        </Button>
      ) : (
        <>
          <Button
            className="min-h-11"
            variant="secondary"
            data-testid="checkout-cash"
            onClick={() => setCash(true)}
          >
            {t('rentalHandover:cash.collect')}
          </Button>
          <Button
            className="min-h-11"
            variant="outline"
            data-testid="checkout-finish"
            onClick={onDone}
          >
            {t('rentalHandover:finish')}
          </Button>
        </>
      )}
      <QuickCashDialog
        marinaId={marinaId}
        bookingId={bookingId}
        target="main"
        open={cash}
        onOpenChange={setCash}
        onDone={() => onDone()}
      />
    </div>
  );
}
