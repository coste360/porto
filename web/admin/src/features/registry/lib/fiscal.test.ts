import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';

import {
  isValidFiscalCode,
  isValidIban,
  isValidPersonFiscalCode,
  isValidVatNumber,
  normalizeCode,
} from './fiscal';

describe('fiscal checks', () => {
  it.each([
    ['RSSMRA85T10A562S', true],
    ['rssmra85t10a562s', true],
    ['RSS MRA 85T10 A562S', true],
    ['RSSMRA85T10A56NH', true], // omocodia: last digit replaced by N
    ['BNCGNN70A41H501V', true],
    ['RSSMRA85T10A562T', false], // wrong check character
    ['RSSMRA85Z10A562S', false], // Z is not a month letter
    ['RSSMRA85T32A562S', false], // day 32
    ['RSSMRA85T10A562', false],
    ['', false],
  ])('person fiscal code %s → %s', (code, valid) => {
    expect(isValidPersonFiscalCode(code)).toBe(valid);
  });

  it('company fiscal codes are 11 digits with the VAT check or 16 characters', () => {
    expect(isValidFiscalCode('00743110157', 'company')).toBe(true);
    expect(isValidFiscalCode('00743110158', 'company')).toBe(false);
    expect(isValidFiscalCode('RSSMRA85T10A562S', 'company')).toBe(true);
    expect(isValidFiscalCode('00743110157', 'person')).toBe(false);
  });

  it.each([
    ['12345678903', true],
    ['IT12345678903', true],
    ['it 00743110157', true],
    ['12345678904', false],
    ['IT1234567890', false],
    ['DE123456789', true], // other countries: format only
    ['FRXX999999999', true],
    ['D1', false],
  ])('VAT %s → %s', (vat, valid) => {
    expect(isValidVatNumber(vat)).toBe(valid);
  });

  it.each([
    ['IT60X0542811101000000123456', true],
    ['IT60 X054 2811 1010 0000 0123 456', true],
    ['GB82WEST12345698765432', true],
    ['IT61X0542811101000000123456', false],
    ['IT60X05428', false],
  ])('IBAN %s → %s', (iban, valid) => {
    expect(isValidIban(iban)).toBe(valid);
  });

  it('normalises to upper case without spaces, dots and dashes', () => {
    expect(normalizeCode(' it 60.x-1 ')).toBe('IT60X1');
  });

  // Shared vectors (m1-plan §5.6), owned by BACKEND-REGISTRY and read by the Go tests too.
  const vectorsPath = resolve(__dirname, '../../../../../../testdata/registry/fiscal-codes.json');
  it.runIf(existsSync(vectorsPath))('agrees with the shared server vectors', () => {
    type Set = { valid: string[]; invalid: string[] };
    const v = JSON.parse(readFileSync(vectorsPath, 'utf8')) as Record<string, Set>;
    const check = (name: string, fn: (s: string) => boolean) => {
      expect(v[name], name).toBeDefined();
      for (const s of v[name]!.valid) expect(fn(s), `${name} valid ${s}`).toBe(true);
      for (const s of v[name]!.invalid) expect(fn(s), `${name} invalid ${s}`).toBe(false);
    };
    check('person_fiscal_code', (s) => isValidFiscalCode(s, 'person'));
    check('company_fiscal_code', (s) => isValidFiscalCode(s, 'company'));
    check('vat_number', isValidVatNumber);
    check('iban', isValidIban);
  });
});
