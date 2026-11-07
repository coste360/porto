import { useState } from 'react';

import {
  PUBLIC_UPLOAD_MAX_BYTES,
  PUBLIC_UPLOAD_TYPES,
  useAcceptPublicQuote,
  useUploadPublicDocument,
  type PublicAcceptResult,
  type PublicQuote,
  type RequiredField,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldLabel, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';

import { DepositCard, Done, Money, PdfViewer, type LinkRef } from './common';
import {
  EMPTY_DATA,
  UPLOAD_FIELDS,
  acceptBody,
  linkPath,
  needsCustomer,
  needsVessel,
  uploadsNeeded,
  type MissingData,
} from './quoteAccept';

/** Quote acceptance (13.3): options, missing data, documents, plan, accept → signing link and deposit. */
export function QuoteLinkPage({ linkRef, quote }: { linkRef: LinkRef; quote: PublicQuote }) {
  const { t } = useT();
  const accept = useAcceptPublicQuote(linkRef);
  const [optionId, setOptionId] = useState(quote.options.length === 1 ? quote.options[0]!.id : '');
  const [plan, setPlan] = useState<Schemas['PlanChoice']>('standard');
  const [data, setData] = useState<MissingData>(EMPTY_DATA);
  const [uploaded, setUploaded] = useState<Set<RequiredField>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PublicAcceptResult | null>(null);
  const option = quote.options.find((o) => o.id === optionId);
  const open = quote.status === 'sent' || quote.status === 'draft';

  if (result) {
    return (
      <div className="flex flex-col gap-4">
        <Done title={t('customerLinks:quoteAccept.done', { code: result.contract_code })} />
        {result.signature_link ? (
          <Button asChild size="lg">
            <a href={linkPath(result.signature_link)}>{t('customerLinks:quoteAccept.sign')}</a>
          </Button>
        ) : null}
        {result.deposit ? <DepositCard deposit={result.deposit} /> : null}
      </div>
    );
  }
  if (!open) {
    return (
      <Done
        title={t(`customerLinks:quoteAccept.closed.${quote.status}`, {
          defaultValue: t('customerLinks:quoteAccept.closedOther'),
        })}
      />
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">
        {t('customerLinks:quoteAccept.intro', {
          vessel: quote.vessel_name,
          date: formatIsoDate(quote.valid_until),
        })}
      </p>
      <PdfViewer
        linkRef={linkRef}
        title={t('customerLinks:quoteAccept.pdf', { code: quote.code })}
      />
      <FieldSet>
        <FieldLegend>{t('customerLinks:quoteAccept.options')}</FieldLegend>
        <RadioGroup
          value={optionId}
          onValueChange={setOptionId}
          aria-label={t('customerLinks:quoteAccept.options')}
        >
          {quote.options.map((o) => (
            <Card key={o.id}>
              <CardContent className="flex items-start gap-3 pt-4">
                <RadioGroupItem id={`opt-${o.id}`} value={o.id} className="mt-1" />
                <label htmlFor={`opt-${o.id}`} className="flex flex-1 flex-col gap-1 text-sm">
                  <span className="font-medium">
                    {o.label || t('customerLinks:quoteAccept.option', { n: o.seq })}
                  </span>
                  <span>{`${formatIsoDate(o.starts_on)} – ${formatIsoDate(o.ends_on)}`}</span>
                  {o.resource_codes?.length ? <span>{o.resource_codes.join(', ')}</span> : null}
                  <span className="text-lg font-semibold">
                    <Money cents={o.total_cents} />
                  </span>
                  {o.deposit_cents > 0 ? (
                    <span className="text-muted-foreground">
                      {t('customerLinks:quoteAccept.deposit')} <Money cents={o.deposit_cents} />
                    </span>
                  ) : null}
                </label>
              </CardContent>
            </Card>
          ))}
        </RadioGroup>
      </FieldSet>
      {option?.early_payment_total_cents !== undefined ? (
        <FieldSet>
          <FieldLegend>{t('customerLinks:plan.title')}</FieldLegend>
          <RadioGroup
            value={plan}
            onValueChange={(v) => setPlan(v as Schemas['PlanChoice'])}
            aria-label={t('customerLinks:plan.title')}
          >
            <Field orientation="horizontal">
              <RadioGroupItem id="plan-standard" value="standard" />
              <FieldLabel htmlFor="plan-standard">
                {t('customerLinks:plan.standard')} <Money cents={option.total_cents} />
              </FieldLabel>
            </Field>
            <Field orientation="horizontal">
              <RadioGroupItem id="plan-early" value="early_payment" />
              <FieldLabel htmlFor="plan-early">
                {t('customerLinks:plan.early')} <Money cents={option.early_payment_total_cents} />
              </FieldLabel>
            </Field>
          </RadioGroup>
        </FieldSet>
      ) : null}
      {needsCustomer(quote) ? <CustomerData data={data} onChange={setData} /> : null}
      {needsVessel(quote) ? <VesselData data={data} onChange={setData} /> : null}
      {uploadsNeeded(quote).map((f) => (
        <UploadField
          key={f}
          linkRef={linkRef}
          field={f}
          done={uploaded.has(f)}
          expiresOn={f === 'insurance_document' ? data.insuranceExpiresOn : ''}
          onDone={() => setUploaded(new Set([...uploaded, f]))}
        />
      ))}
      {error ? <FieldError>{t(`customerLinks:quoteAccept.errors.${error}`)}</FieldError> : null}
      <ProblemAlert error={accept.error} />
      <Button
        size="lg"
        disabled={accept.isPending}
        onClick={() => {
          const r = acceptBody(quote, optionId, plan, data, uploaded);
          if (!r.ok) return setError(r.error);
          setError(null);
          accept.mutate(r.body, { onSuccess: setResult });
        }}
      >
        {t('customerLinks:quoteAccept.accept')}
      </Button>
    </div>
  );
}

function CustomerData({
  data,
  onChange,
}: {
  data: MissingData;
  onChange: (d: MissingData) => void;
}) {
  const { t } = useT();
  const a = data.address;
  const set = (k: keyof Schemas['Address'], v: string) =>
    onChange({ ...data, address: { ...a, [k]: v } });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('customerLinks:data.customer')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {(['line1', 'postal_code', 'city', 'province', 'country'] as const).map((k) => (
          <Field key={k}>
            <FieldLabel htmlFor={`addr-${k}`}>{t(`customerLinks:data.address.${k}`)}</FieldLabel>
            <Input
              id={`addr-${k}`}
              autoComplete={
                k === 'line1'
                  ? 'address-line1'
                  : k === 'postal_code'
                    ? 'postal-code'
                    : k === 'city'
                      ? 'address-level2'
                      : 'off'
              }
              value={a[k] ?? ''}
              onChange={(e) => set(k, e.target.value)}
            />
          </Field>
        ))}
        <Field>
          <FieldLabel htmlFor="data-fiscal">{t('customerLinks:data.fiscalCode')}</FieldLabel>
          <Input
            id="data-fiscal"
            autoCapitalize="characters"
            value={data.fiscalCode}
            onChange={(e) => onChange({ ...data, fiscalCode: e.target.value })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="data-birth">{t('customerLinks:data.birthDate')}</FieldLabel>
          <Input
            id="data-birth"
            type="date"
            value={data.birthDate}
            onChange={(e) => onChange({ ...data, birthDate: e.target.value })}
          />
        </Field>
      </CardContent>
    </Card>
  );
}

function VesselData({ data, onChange }: { data: MissingData; onChange: (d: MissingData) => void }) {
  const { t } = useT();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('customerLinks:data.vessel')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Field>
          <FieldLabel htmlFor="data-registration">
            {t('customerLinks:data.registration')}
          </FieldLabel>
          <Input
            id="data-registration"
            value={data.registration}
            onChange={(e) => onChange({ ...data, registration: e.target.value })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="data-insurance">
            {t('customerLinks:data.insuranceExpiry')}
          </FieldLabel>
          <Input
            id="data-insurance"
            type="date"
            value={data.insuranceExpiresOn}
            onChange={(e) => onChange({ ...data, insuranceExpiresOn: e.target.value })}
          />
        </Field>
      </CardContent>
    </Card>
  );
}

function UploadField({
  linkRef,
  field,
  done,
  expiresOn,
  onDone,
}: {
  linkRef: LinkRef;
  field: RequiredField;
  done: boolean;
  expiresOn: string;
  onDone: () => void;
}) {
  const { t } = useT();
  const upload = useUploadPublicDocument(linkRef);
  const [fileError, setFileError] = useState<'type' | 'size' | null>(null);
  return (
    <Field data-invalid={fileError ? true : undefined}>
      <FieldLabel htmlFor={`upload-${field}`}>{t(`docsEnums:requiredField.${field}`)}</FieldLabel>
      <Input
        id={`upload-${field}`}
        type="file"
        accept={PUBLIC_UPLOAD_TYPES.join(',')}
        capture="environment"
        onChange={(e) => {
          const f = e.target.files?.[0];
          setFileError(null);
          if (!f) return;
          if (!(PUBLIC_UPLOAD_TYPES as readonly string[]).includes(f.type))
            return setFileError('type');
          if (f.size > PUBLIC_UPLOAD_MAX_BYTES) return setFileError('size');
          upload.mutate(
            { file: f, type: UPLOAD_FIELDS[field] ?? 'other', ...(expiresOn ? { expiresOn } : {}) },
            { onSuccess: onDone },
          );
        }}
      />
      {done ? (
        <p className="text-sm" role="status">
          {t('customerLinks:upload.done')}
        </p>
      ) : null}
      {fileError ? <FieldError>{t(`customerLinks:upload.errors.${fileError}`)}</FieldError> : null}
      <ProblemAlert error={upload.error} />
    </Field>
  );
}
