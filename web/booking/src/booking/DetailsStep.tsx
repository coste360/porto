import { ArrowRightIcon, CheckCircle2Icon, FileUpIcon } from 'lucide-react';
import { useRef, useState, type ReactNode } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { z } from 'zod';

import {
  BOOKING_DOCUMENT_MAX_BYTES,
  BOOKING_DOCUMENT_TYPES,
  isProblem,
  useSubmitBookingDetails,
  useUploadBookingDocument,
  type Booking,
  type BookingConfig,
  type BookingDocumentType,
  type BookingRef,
  type HullType,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { FormField } from '@porto/shared/ui/patterns/FormField';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { ScrollArea } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { zodFormResolver } from '../common/formResolver';
import { localized } from '../common/i18nText';
import { HULL_TYPES } from './hullTypes';

const req = 'common:validation.required';

function makeSchema(config: BookingConfig) {
  const rf = config.required_fields;
  return z.object({
    first_name: z.string().trim().min(1, req).max(100),
    last_name: z.string().trim().min(1, req).max(100),
    email: z.string().trim().email('common:validation.email').max(254),
    phone: rf.phone
      ? z.string().trim().min(5, 'checkout:validation.phone').max(40)
      : z.string().trim().max(40),
    country: z
      .string()
      .trim()
      .regex(/^[A-Za-z]{2}$/, 'checkout:validation.country'),
    vessel_name: z.string().trim().min(1, req).max(120),
    registration: z.string().trim().max(40),
    hull_type: z.enum(HULL_TYPES as [HullType, ...HullType[]], { message: req }),
    people_on_board: rf.people_on_board
      ? z.number(req).int().min(1, req).max(500)
      : z.number().int().min(1).max(500).optional(),
    expected_arrival_time: z
      .string()
      .regex(/^$|^([01]\d|2[0-3]):[0-5]\d$/, 'checkout:validation.time'),
    notes: z.string().max(2000),
    terms: z.literal(true, { message: 'checkout:validation.consent' }),
    privacy: z.literal(true, { message: 'checkout:validation.consent' }),
    marketing: z.boolean(),
  });
}

type FormIn = z.input<ReturnType<typeof makeSchema>>;
type FormOut = z.output<ReturnType<typeof makeSchema>>;

function TextDialog({
  title,
  text,
  open,
  onOpenChange,
}: {
  title: string;
  text: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className="sr-only">{title}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-96">
          <p className="text-sm whitespace-pre-line">{text}</p>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

function ConsentField({
  id,
  checked,
  onChange,
  label,
  error,
  onRead,
  readLabel,
}: {
  id: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  label: ReactNode;
  error?: string | undefined;
  onRead?: () => void;
  readLabel?: string;
}) {
  return (
    <Field orientation="horizontal" data-invalid={error ? true : undefined}>
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(v) => onChange(v === true)}
        aria-invalid={Boolean(error)}
      />
      <div className="flex flex-col gap-1">
        <FieldLabel htmlFor={id} className="font-normal">
          {label}
        </FieldLabel>
        {onRead ? (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto w-fit p-0"
            onClick={onRead}
          >
            {readLabel}
          </Button>
        ) : null}
        {error ? <FieldError>{error}</FieldError> : null}
      </div>
    </Field>
  );
}

/** Upload of one document type: camera or file, PDF/PNG/JPEG up to 10 MiB, sent as the raw body. */
function DocumentUpload({
  bookingRef,
  type,
  uploaded,
  required,
}: {
  bookingRef: BookingRef;
  type: BookingDocumentType;
  uploaded: boolean;
  required: boolean;
}) {
  const { t } = useT();
  const upload = useUploadBookingDocument(bookingRef);
  const [localError, setLocalError] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const id = `doc-${type}`;
  const onFile = (file: File | undefined) => {
    setLocalError(null);
    if (!file) return;
    if (!(BOOKING_DOCUMENT_TYPES as readonly string[]).includes(file.type)) {
      setLocalError(t('checkout:documents.badType'));
      return;
    }
    if (file.size > BOOKING_DOCUMENT_MAX_BYTES) {
      setLocalError(t('checkout:documents.tooLarge'));
      return;
    }
    upload.mutate({ file, type });
  };
  const done = uploaded || upload.isSuccess;
  return (
    <div
      className="flex flex-col gap-2 rounded-lg border p-3"
      data-testid="booking-document"
      data-type={type}
    >
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={id} className="text-sm font-medium">
          {t(`checkout:documentTypes.${type}`)}
          {required ? null : (
            <span className="text-muted-foreground font-normal">
              {' '}
              {t('checkout:search.optional')}
            </span>
          )}
        </label>
        {done ? (
          <span className="text-primary flex items-center gap-1 text-sm">
            <CheckCircle2Icon className="size-4" aria-hidden />
            {t('checkout:documents.uploaded')}
          </span>
        ) : null}
      </div>
      <input
        ref={input}
        id={id}
        type="file"
        accept="image/*,application/pdf"
        capture="environment"
        className="sr-only"
        onChange={(e) => {
          onFile(e.target.files?.[0]);
          e.target.value = '';
        }}
      />
      <Button
        type="button"
        variant="outline"
        disabled={upload.isPending}
        onClick={() => input.current?.click()}
      >
        <FileUpIcon />
        {upload.isPending
          ? t('checkout:documents.uploading')
          : done
            ? t('checkout:documents.replace')
            : t('checkout:documents.upload')}
      </Button>
      {localError ? <p className="text-destructive text-sm">{localError}</p> : null}
      {upload.error ? <ProblemAlert error={upload.error} /> : null}
    </div>
  );
}

/** Step 4 "Dati": customer and vessel, people on board, arrival time, documents and consents. */
export default function DetailsStep({
  config,
  booking,
  bookingRef,
  hullType,
  onDone,
  onHoldExpired,
}: {
  config: BookingConfig;
  booking: Booking;
  bookingRef: BookingRef;
  hullType: HullType | undefined;
  onDone: () => void;
  onHoldExpired: () => void;
}) {
  const { t, i18n } = useT();
  const submit = useSubmitBookingDetails(bookingRef);
  const [reading, setReading] = useState<'terms' | 'privacy' | 'marketing' | null>(null);
  const textOf = (kind: 'terms' | 'privacy' | 'marketing') =>
    config.texts.find((x) => x.kind === kind);
  const form = useForm<FormIn, unknown, FormOut>({
    resolver: zodFormResolver(makeSchema(config)),
    defaultValues: {
      first_name: '',
      last_name: '',
      email: '',
      phone: '',
      country: i18n.language.startsWith('en') ? '' : 'IT',
      vessel_name: booking.vessel.name && booking.vessel.name !== '—' ? booking.vessel.name : '',
      registration: '',
      hull_type: (booking.vessel.hull_type ?? hullType) as HullType,
      people_on_board: config.required_fields.people_on_board
        ? (undefined as unknown as number)
        : undefined,
      expected_arrival_time: '',
      notes: '',
      terms: false as unknown as true,
      privacy: false as unknown as true,
      marketing: false,
    },
  });

  const requiredDocs = config.required_fields.documents;
  const uploadedTypes = new Set(booking.documents.map((d) => d.type));
  const missing = booking.missing_documents;
  const docsBlocking = missing.length > 0 && !config.required_fields.documents_later;
  const terms = textOf('terms');
  const privacy = textOf('privacy');
  const marketing = textOf('marketing');

  const onSubmit = form.handleSubmit((v) => {
    if (!terms || !privacy) return;
    submit.mutate(
      {
        customer: {
          first_name: v.first_name,
          last_name: v.last_name,
          email: v.email,
          ...(v.phone ? { phone: v.phone } : {}),
          country: v.country.toUpperCase(),
          locale: i18n.language.startsWith('en') ? 'en' : 'it',
        },
        vessel: {
          name: v.vessel_name,
          hull_type: v.hull_type,
          ...(v.registration ? { registration: v.registration } : {}),
        },
        ...(v.people_on_board ? { people_on_board: v.people_on_board } : {}),
        ...(v.expected_arrival_time ? { expected_arrival_time: v.expected_arrival_time } : {}),
        ...(v.notes ? { notes: v.notes } : {}),
        consents: {
          terms_version: terms.version,
          privacy_version: privacy.version,
          marketing: v.marketing,
          ...(v.marketing && marketing ? { marketing_version: marketing.version } : {}),
        },
      },
      {
        onError: (e) => (isProblem(e, 'hold_expired') ? onHoldExpired() : undefined),
        onSuccess: (b) =>
          b.missing_documents.length === 0 || config.required_fields.documents_later
            ? onDone()
            : undefined,
      },
    );
  });

  return (
    <form
      onSubmit={onSubmit}
      noValidate
      className="flex flex-col gap-4"
      data-testid="booking-details"
    >
      <Card>
        <CardHeader>
          <CardTitle>{t('checkout:details.customer')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="first_name"
              label={t('checkout:details.firstName')}
              render={(f) => <Input {...f} autoComplete="given-name" />}
            />
            <FormField
              control={form.control}
              name="last_name"
              label={t('checkout:details.lastName')}
              render={(f) => <Input {...f} autoComplete="family-name" />}
            />
            <FormField
              control={form.control}
              name="email"
              label={t('checkout:details.email')}
              render={(f) => <Input {...f} type="email" inputMode="email" autoComplete="email" />}
            />
            <FormField
              control={form.control}
              name="phone"
              label={t('checkout:details.phone')}
              render={(f) => <Input {...f} type="tel" inputMode="tel" autoComplete="tel" />}
            />
            <FormField
              control={form.control}
              name="country"
              label={t('checkout:details.country')}
              description={t('checkout:details.countryHint')}
              render={(f) => (
                <Input {...f} maxLength={2} autoComplete="country" className="uppercase" />
              )}
            />
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('checkout:details.vessel')}</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <FormField
              control={form.control}
              name="vessel_name"
              label={t('checkout:details.vesselName')}
              render={(f) => <Input {...f} />}
            />
            <FormField
              control={form.control}
              name="registration"
              label={t('checkout:details.registration')}
              description={t('checkout:search.optional')}
              render={(f) => <Input {...f} />}
            />
            <FormField
              control={form.control}
              name="hull_type"
              label={t('checkout:search.hullType')}
              render={(f) => (
                <Select value={f.value ?? ''} onValueChange={(v) => f.onChange(v as HullType)}>
                  <SelectTrigger id={f.id} aria-invalid={f['aria-invalid']} className="w-full">
                    <SelectValue placeholder={t('checkout:search.hullTypePlaceholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {HULL_TYPES.map((h) => (
                      <SelectItem key={h} value={h}>
                        {t(`checkout:hullTypes.${h}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            <FormField
              control={form.control}
              name="people_on_board"
              label={t('checkout:details.peopleOnBoard')}
              render={(f) => (
                <Input
                  id={f.id}
                  name={f.name}
                  aria-invalid={f['aria-invalid']}
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={500}
                  value={f.value ?? ''}
                  onBlur={f.onBlur}
                  onChange={(e) =>
                    f.onChange(e.target.value === '' ? undefined : Number(e.target.value))
                  }
                />
              )}
            />
            <FormField
              control={form.control}
              name="expected_arrival_time"
              label={t('checkout:details.arrivalTime')}
              description={t('checkout:details.arrivalWindow', {
                from: config.rules.arrival_from,
                to: config.rules.arrival_to,
              })}
              render={(f) => (
                <Input
                  {...f}
                  type="time"
                  min={config.rules.arrival_from}
                  max={config.rules.arrival_to}
                />
              )}
            />
            <FormField
              control={form.control}
              name="notes"
              label={t('checkout:details.notes')}
              description={t('checkout:search.optional')}
              render={(f) => <Textarea {...f} rows={3} className="sm:col-span-2" />}
            />
          </FieldGroup>
        </CardContent>
      </Card>

      {requiredDocs.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('checkout:documents.title')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-muted-foreground text-sm">
              {config.required_fields.documents_later
                ? t('checkout:documents.laterHint')
                : t('checkout:documents.hint')}
            </p>
            {requiredDocs.map((type) => (
              <DocumentUpload
                key={type}
                bookingRef={bookingRef}
                type={type}
                uploaded={uploadedTypes.has(type)}
                required={!config.required_fields.documents_later}
              />
            ))}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{t('checkout:consents.title')}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Controller
            control={form.control}
            name="terms"
            render={({ field, fieldState }) => (
              <ConsentField
                id="consent-terms"
                checked={field.value === true}
                onChange={field.onChange}
                label={t('checkout:consents.terms')}
                error={fieldState.error?.message ? t(fieldState.error.message) : undefined}
                {...(terms
                  ? { onRead: () => setReading('terms'), readLabel: t('checkout:consents.read') }
                  : {})}
              />
            )}
          />
          <Controller
            control={form.control}
            name="privacy"
            render={({ field, fieldState }) => (
              <ConsentField
                id="consent-privacy"
                checked={field.value === true}
                onChange={field.onChange}
                label={t('checkout:consents.privacy')}
                error={fieldState.error?.message ? t(fieldState.error.message) : undefined}
                {...(privacy
                  ? { onRead: () => setReading('privacy'), readLabel: t('checkout:consents.read') }
                  : {})}
              />
            )}
          />
          {marketing ? (
            <Controller
              control={form.control}
              name="marketing"
              render={({ field }) => (
                <ConsentField
                  id="consent-marketing"
                  checked={field.value}
                  onChange={field.onChange}
                  label={t('checkout:consents.marketing')}
                  onRead={() => setReading('marketing')}
                  readLabel={t('checkout:consents.read')}
                />
              )}
            />
          ) : null}
          {!terms || !privacy ? (
            <p className="text-destructive text-sm">{t('checkout:consents.missingTexts')}</p>
          ) : null}
        </CardContent>
      </Card>

      {reading ? (
        <TextDialog
          title={t(`checkout:consents.dialog.${reading}`)}
          text={localized(textOf(reading)?.text, i18n.language)}
          open
          onOpenChange={(o) => (o ? undefined : setReading(null))}
        />
      ) : null}

      {submit.error ? <ProblemAlert error={submit.error} /> : null}
      {docsBlocking && submit.isSuccess ? (
        <p className="text-destructive text-sm" role="status">
          {t('checkout:documents.missing')}
        </p>
      ) : null}
      <Button
        type="submit"
        size="lg"
        disabled={submit.isPending || !terms || !privacy}
        data-testid="booking-details-submit"
      >
        <ArrowRightIcon />
        {submit.isPending ? t('checkout:details.saving') : t('checkout:details.continue')}
      </Button>
    </form>
  );
}
