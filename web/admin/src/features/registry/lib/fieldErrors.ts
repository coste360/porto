import type { FieldValues, Path, UseFormSetError } from 'react-hook-form';

import { isProblem } from '@porto/shared/api';

/** Server field codes with their own message; anything else shows the generic "invalid value". */
const CODE_KEYS: Record<string, string> = {
  required: 'common:validation.required',
  fiscal_code_invalid: 'customers:validation.fiscalCode',
  vat_invalid: 'customers:validation.vat',
  iban_invalid: 'customers:validation.iban',
  email_invalid: 'common:validation.email',
  owner_shares_exceed: 'vessels:parties.validation.sharesExceed',
  owner_share_required: 'vessels:parties.validation.shareRequired',
};

export function fieldErrorKey(code: string): string {
  return CODE_KEYS[code] ?? 'customers:validation.invalid';
}

/**
 * Puts the `errors[]` of a validation_failed problem on the matching form fields. Returns true when at least
 * one field matched (the caller can then skip the generic alert).
 */
export function applyFieldErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  known: readonly string[],
): boolean {
  if (!isProblem(error, 'validation_failed')) return false;
  let matched = false;
  for (const e of error.errors) {
    const field = e.field.replace(/^billing_address\./, '');
    if (!known.includes(field)) continue;
    setError(field as Path<T>, { type: 'server', message: fieldErrorKey(e.code) });
    matched = true;
  }
  return matched;
}
