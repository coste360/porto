import { describe, expect, it } from 'vitest';

import {
  customerSchema,
  emptyCustomerValues,
  parseTags,
  valuesToInput,
  valuesToPatch,
  type CustomerFormValues,
} from './customerSchema';

const person = (over: Partial<CustomerFormValues> = {}): CustomerFormValues => ({
  ...emptyCustomerValues(),
  first_name: 'Mario',
  last_name: 'Rossi',
  ...over,
});

const issues = (v: CustomerFormValues) =>
  customerSchema.safeParse(v).error?.issues.map((i) => `${i.path.join('.')}:${i.message}`) ?? [];

describe('customerSchema', () => {
  it('needs names for a person and the company name for a company', () => {
    expect(issues(person({ first_name: '' }))).toEqual(['first_name:common:validation.required']);
    expect(issues({ ...emptyCustomerValues(), kind: 'company' })).toEqual([
      'company_name:common:validation.required',
    ]);
  });

  it('checks fiscal code, VAT, IBAN and emails', () => {
    expect(
      issues(
        person({
          fiscal_code: 'RSSMRA85T10A562T',
          vat_number: '12345678904',
          iban: 'IT61X0542811101000000123456',
          emails: [{ label: '', value: 'not-an-email' }],
        }),
      ),
    ).toEqual([
      'fiscal_code:customers:validation.fiscalCode',
      'vat_number:customers:validation.vat',
      'iban:customers:validation.iban',
      'emails.0.value:common:validation.email',
    ]);
    expect(issues(person({ fiscal_code: 'rssmra85t10a562s' }))).toEqual([]);
  });
});

describe('form → API', () => {
  it('omits empty optional fields on create and normalises codes', () => {
    const input = valuesToInput(
      person({
        fiscal_code: 'rss mra 85t10 a562s',
        tags: 'vip, socio, vip',
        emails: [
          { label: 'casa', value: 'mario@example.invalid' },
          { label: '', value: '' },
        ],
        phones: [{ label: '', value: '' }],
        payment_days: '30',
        end_of_month: true,
      }),
    );
    expect(input).toEqual({
      kind: 'person',
      first_name: 'Mario',
      last_name: 'Rossi',
      company_name: '',
      billing_address: {
        line1: '',
        line2: '',
        postal_code: '',
        city: '',
        province: '',
        country: 'IT',
      },
      locale: 'it',
      emails: [{ label: 'casa', value: 'mario@example.invalid' }],
      phones: [],
      tags: ['vip', 'socio'],
      notes: '',
      fiscal_code: 'RSSMRA85T10A562S',
      payment_terms: { days: 30, end_of_month: true },
    });
  });

  it('sends nulls on patch and the IBAN only when typed or cleared', () => {
    const patch = valuesToPatch(person());
    expect(patch.fiscal_code).toBeNull();
    expect(patch.category).toBeNull();
    expect(patch.payment_terms).toBeNull();
    expect('iban' in patch).toBe(false);
    expect(valuesToPatch(person(), true).iban).toBe('');
    expect(valuesToPatch(person({ iban: 'it60 x054 2811 1010 0000 0123 456' })).iban).toBe(
      'IT60X0542811101000000123456',
    );
  });

  it('parses tags', () => {
    expect(parseTags(' a, ,b,a ')).toEqual(['a', 'b']);
  });
});
