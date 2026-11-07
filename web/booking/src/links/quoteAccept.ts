// Quote acceptance by the customer (spec 13.3, m4-plan §3.2): which data the marina still needs, which documents to
// upload, and the accept body. Pure; the server validates again.
import type { PublicQuote, PublicQuoteAccept, RequiredField, Schemas } from '@porto/shared/api';

export type MissingData = {
  address: Schemas['Address'];
  fiscalCode: string;
  birthDate: string;
  registration: string;
  insuranceExpiresOn: string;
};

export const EMPTY_DATA: MissingData = {
  address: { line1: '', postal_code: '', city: '', province: '', country: 'IT' },
  fiscalCode: '',
  birthDate: '',
  registration: '',
  insuranceExpiresOn: '',
};

const CUSTOMER_FIELDS: readonly RequiredField[] = ['address', 'fiscal_code', 'birth_date'];
const VESSEL_FIELDS: readonly RequiredField[] = ['vessel_registration', 'insurance_expiry'];
/** Missing fields satisfied by an upload instead of a form field. */
export const UPLOAD_FIELDS: Partial<Record<RequiredField, string>> = {
  insurance_document: 'insurance_policy',
  identity_document: 'identity_card',
};

export function needsCustomer(q: Pick<PublicQuote, 'missing_fields'>): boolean {
  return q.missing_fields.some((f) => CUSTOMER_FIELDS.includes(f));
}

export function needsVessel(q: Pick<PublicQuote, 'missing_fields'>): boolean {
  return q.missing_fields.some((f) => VESSEL_FIELDS.includes(f));
}

export function uploadsNeeded(q: Pick<PublicQuote, 'missing_fields'>): RequiredField[] {
  return q.missing_fields.filter((f) => f in UPLOAD_FIELDS);
}

const FISCAL_CODE = /^[A-Z0-9]{11,16}$/i;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

export type AcceptResult =
  | { ok: true; body: PublicQuoteAccept }
  | {
      ok: false;
      error:
        | 'option'
        | 'address'
        | 'fiscal_code'
        | 'birth_date'
        | 'registration'
        | 'insurance'
        | 'uploads';
    };

/**
 * Body of the acceptance: option, plan and only the blocks the quote is missing. The customer block needs the
 * whole address, the fiscal code and the birth date; the vessel block the registration and the insurance expiry.
 */
export function acceptBody(
  q: Pick<PublicQuote, 'missing_fields'>,
  optionId: string,
  plan: Schemas['PlanChoice'],
  d: MissingData,
  uploaded: ReadonlySet<RequiredField>,
): AcceptResult {
  if (!optionId) return { ok: false, error: 'option' };
  const body: PublicQuoteAccept = { option_id: optionId, ...(plan !== 'standard' ? { plan } : {}) };
  if (needsCustomer(q)) {
    const a = d.address;
    if (!a.line1?.trim() || !a.postal_code?.trim() || !a.city?.trim())
      return { ok: false, error: 'address' };
    if (!FISCAL_CODE.test(d.fiscalCode.trim())) return { ok: false, error: 'fiscal_code' };
    if (!DATE.test(d.birthDate)) return { ok: false, error: 'birth_date' };
    body.customer = {
      address: Object.fromEntries(
        Object.entries(a)
          .filter(([, v]) => typeof v === 'string' && v.trim() !== '')
          .map(([k, v]) => [k, (v as string).trim()]),
      ) as Schemas['Address'],
      fiscal_code: d.fiscalCode.trim().toUpperCase(),
      birth_date: d.birthDate,
    };
  }
  if (needsVessel(q)) {
    if (!d.registration.trim()) return { ok: false, error: 'registration' };
    if (!DATE.test(d.insuranceExpiresOn)) return { ok: false, error: 'insurance' };
    body.vessel = {
      registration: d.registration.trim(),
      insurance_expires_on: d.insuranceExpiresOn,
    };
  }
  if (uploadsNeeded(q).some((f) => !uploaded.has(f))) return { ok: false, error: 'uploads' };
  return { ok: true, body };
}

/** Path of a signature link inside the booking SPA (the server returns the full URL). */
export function linkPath(url: string): string {
  try {
    const u = new URL(url, 'http://local');
    return u.pathname + u.search;
  } catch {
    return url;
  }
}
