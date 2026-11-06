import { z } from 'zod';

import type { Customer, CustomerInput, CustomerPatch } from '@porto/shared/api';

import { CUSTOMER_CATEGORIES, CUSTOMER_KINDS } from '../lib/enums';
import { isValidFiscalCode, isValidIban, isValidVatNumber, normalizeCode } from '../lib/fiscal';

const contact = z.object({ label: z.string().trim().max(50), value: z.string().trim().max(200) });

/** Customer form (create and edit). zod messages are i18n keys. */
export const customerSchema = z
  .object({
    kind: z.enum(CUSTOMER_KINDS),
    first_name: z.string().trim().max(100),
    last_name: z.string().trim().max(100),
    company_name: z.string().trim().max(200),
    fiscal_code: z
      .string()
      .trim()
      .max(16 + 8),
    vat_number: z.string().trim().max(20),
    sdi_code: z.string().trim().max(7),
    pec: z.string().trim().max(200),
    line1: z.string().trim().max(200),
    line2: z.string().trim().max(200),
    postal_code: z.string().trim().max(20),
    city: z.string().trim().max(100),
    province: z.string().trim().max(50),
    country: z.string().trim().max(2),
    nationality: z.string().trim().max(2),
    locale: z.enum(['it', 'en']),
    emails: z.array(contact),
    phones: z.array(contact),
    category: z.union([z.enum(CUSTOMER_CATEGORIES), z.literal('')]),
    tags: z.string().max(500),
    payment_days: z.string().trim(),
    end_of_month: z.boolean(),
    iban: z.string().trim().max(50),
    notes: z.string().max(4000),
  })
  .superRefine((v, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: 'custom', path: [path], message });
    if (v.kind === 'person') {
      if (!v.first_name) issue('first_name', 'common:validation.required');
      if (!v.last_name) issue('last_name', 'common:validation.required');
    } else if (!v.company_name) {
      issue('company_name', 'common:validation.required');
    }
    if (v.fiscal_code && !isValidFiscalCode(v.fiscal_code, v.kind)) {
      issue('fiscal_code', 'customers:validation.fiscalCode');
    }
    if (v.vat_number && !isValidVatNumber(v.vat_number)) {
      issue('vat_number', 'customers:validation.vat');
    }
    if (v.iban && !isValidIban(v.iban)) issue('iban', 'customers:validation.iban');
    if (v.payment_days && !/^\d{1,3}$/.test(v.payment_days)) {
      issue('payment_days', 'customers:validation.paymentDays');
    }
    if (v.country && !/^[A-Za-z]{2}$/.test(v.country))
      issue('country', 'customers:validation.country');
    if (v.nationality && !/^[A-Za-z]{2}$/.test(v.nationality)) {
      issue('nationality', 'customers:validation.country');
    }
    v.emails.forEach((e, i) => {
      if (e.value && !z.email().safeParse(e.value).success) {
        ctx.addIssue({
          code: 'custom',
          path: ['emails', i, 'value'],
          message: 'common:validation.email',
        });
      }
    });
  });

export type CustomerFormValues = z.infer<typeof customerSchema>;

export function emptyCustomerValues(): CustomerFormValues {
  return {
    kind: 'person',
    first_name: '',
    last_name: '',
    company_name: '',
    fiscal_code: '',
    vat_number: '',
    sdi_code: '',
    pec: '',
    line1: '',
    line2: '',
    postal_code: '',
    city: '',
    province: '',
    country: 'IT',
    nationality: '',
    locale: 'it',
    emails: [{ label: '', value: '' }],
    phones: [{ label: '', value: '' }],
    category: '',
    tags: '',
    payment_days: '',
    end_of_month: false,
    iban: '',
    notes: '',
  };
}

export function customerToValues(c: Customer): CustomerFormValues {
  return {
    kind: c.kind,
    first_name: c.first_name,
    last_name: c.last_name,
    company_name: c.company_name,
    fiscal_code: c.fiscal_code ?? '',
    vat_number: c.vat_number ?? '',
    sdi_code: c.sdi_code ?? '',
    pec: c.pec ?? '',
    line1: c.billing_address.line1 ?? '',
    line2: c.billing_address.line2 ?? '',
    postal_code: c.billing_address.postal_code ?? '',
    city: c.billing_address.city ?? '',
    province: c.billing_address.province ?? '',
    country: c.billing_address.country ?? '',
    nationality: c.nationality ?? '',
    locale: c.locale,
    emails: c.emails.length ? c.emails.map((e) => ({ ...e })) : [{ label: '', value: '' }],
    phones: c.phones.length ? c.phones.map((p) => ({ ...p })) : [{ label: '', value: '' }],
    category: c.category ?? '',
    tags: c.tags.join(', '),
    payment_days: c.payment_terms ? String(c.payment_terms.days) : '',
    end_of_month: c.payment_terms?.end_of_month ?? false,
    iban: '',
    notes: c.notes,
  };
}

export function parseTags(text: string): string[] {
  const tags: string[] = [];
  for (const raw of text.split(',')) {
    const tag = raw.trim();
    if (tag && !tags.includes(tag)) tags.push(tag);
  }
  return tags;
}

const contacts = (list: CustomerFormValues['emails']) =>
  list.filter((c) => c.value !== '').map((c) => ({ label: c.label, value: c.value }));

function common(v: CustomerFormValues) {
  const isPerson = v.kind === 'person';
  return {
    kind: v.kind,
    first_name: isPerson ? v.first_name : '',
    last_name: isPerson ? v.last_name : '',
    company_name: isPerson ? '' : v.company_name,
    billing_address: {
      line1: v.line1,
      line2: v.line2,
      postal_code: v.postal_code,
      city: v.city,
      province: v.province,
      country: v.country.toUpperCase(),
    },
    locale: v.locale,
    emails: contacts(v.emails),
    phones: contacts(v.phones),
    tags: parseTags(v.tags),
    notes: v.notes,
  };
}

/** Create body: empty optional fields are omitted. */
export function valuesToInput(v: CustomerFormValues): CustomerInput {
  return {
    ...common(v),
    ...(v.fiscal_code ? { fiscal_code: normalizeCode(v.fiscal_code) } : {}),
    ...(v.vat_number ? { vat_number: normalizeCode(v.vat_number) } : {}),
    ...(v.sdi_code ? { sdi_code: v.sdi_code.toUpperCase() } : {}),
    ...(v.pec ? { pec: v.pec } : {}),
    ...(v.nationality ? { nationality: v.nationality.toUpperCase() } : {}),
    ...(v.category ? { category: v.category } : {}),
    ...(v.payment_days
      ? { payment_terms: { days: Number(v.payment_days), end_of_month: v.end_of_month } }
      : {}),
    ...(v.iban ? { iban: normalizeCode(v.iban) } : {}),
  };
}

/**
 * Patch body: every editable field is sent, empty nullable fields as null. The IBAN is write-only: it is sent
 * only when typed, or as "" when the user asked to remove it.
 */
export function valuesToPatch(v: CustomerFormValues, clearIban = false): CustomerPatch {
  return {
    ...common(v),
    fiscal_code: v.fiscal_code ? normalizeCode(v.fiscal_code) : null,
    vat_number: v.vat_number ? normalizeCode(v.vat_number) : null,
    sdi_code: v.sdi_code ? v.sdi_code.toUpperCase() : null,
    pec: v.pec || null,
    nationality: v.nationality ? v.nationality.toUpperCase() : null,
    category: v.category || null,
    payment_terms: v.payment_days
      ? { days: Number(v.payment_days), end_of_month: v.end_of_month }
      : null,
    ...(v.iban ? { iban: normalizeCode(v.iban) } : clearIban ? { iban: '' } : {}),
  };
}
